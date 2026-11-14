package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/store"
)

// StartError carries a client-facing reason for a run that could not start.
type StartError struct {
	Status      int
	Message     string
	Diagnostics []graph.Diagnostic
}

func (e *StartError) Error() string { return e.Message }

// StorageError wraps a failure to persist a run's events. Only this kind of
// error marks a started run as "could not be stored"; any other failure after a
// run began is an ordinary internal error.
type StorageError struct{ Err error }

func (e *StorageError) Error() string { return e.Err.Error() }
func (e *StorageError) Unwrap() error { return e.Err }

// StartFunc runs one graph to completion, calling persist for every retained
// event and decisions for human nodes. It returns an error only if the run did
// not start (a *StartError) or failed to persist.
type StartFunc func(ctx context.Context, graphPath string, inputs []string, persist func(events.Event) error, decisions runtime.DecisionProvider) error

// ValidateFunc validates a graph file for the preflight screen.
type ValidateFunc func(path string) (status string, diagnostics []graph.Diagnostic, err error)

// Server is the loopback API. Create with New, serve with Serve.
type Server struct {
	Token    string
	Store    store.Store
	Hub      *Hub
	Start    StartFunc
	Validate ValidateFunc
	// ValidateDefinition is the full validation of a parsed graph: graph rules
	// plus each node's static configuration. Nil means graph rules only.
	ValidateDefinition func(*graph.Definition) []graph.Diagnostic

	docs     docState // graph document grants (ADR-0021)
	baseCtx  context.Context
	addr     string
	active   sync.WaitGroup // runs started through the API and not yet returned
	mu       sync.Mutex
	draining bool // set once shutdown begins; no new run may be admitted
}

// ExpectHost pins the Host header that guard accepts. Serve sets it from the
// listener; tests use it to exercise the check.
func (s *Server) ExpectHost(addr string) { s.addr = addr }

// NewToken returns 32 random bytes, hex encoded.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Listen binds an ephemeral port on 127.0.0.1 only. There is deliberately no
// way to choose another address (ADR-0019).
func Listen() (net.Listener, error) { return net.Listen("tcp4", "127.0.0.1:0") }

// Serve blocks until ctx ends, then shuts down and returns. Runs started
// through the API are cancelled with ctx.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	s.baseCtx = ctx
	s.ExpectHost(l.Addr().String())
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		_ = srv.Close()
		s.mu.Lock()
		s.draining = true
		s.mu.Unlock()
		// Runs were cancelled with ctx; give them a bounded moment to record
		// their terminal event so a stopped server leaves no half-written run.
		drained := make(chan struct{})
		go func() { s.active.Wait(); close(drained) }()
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
		}
		return nil
	case err := <-errc:
		return err
	}
}

// Handler returns the authenticated router. Exposed for tests.
func (s *Server) Handler() http.Handler {
	if s.baseCtx == nil {
		s.baseCtx = context.Background()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /runs", s.listRuns)
	mux.HandleFunc("POST /runs", s.startRun)
	mux.HandleFunc("GET /runs/{id}", s.getRun)
	mux.HandleFunc("GET /runs/{id}/events", s.streamEvents)
	mux.HandleFunc("POST /runs/{id}/decisions/{nodeRun}", s.decide)
	mux.HandleFunc("GET /approvals", s.approvals)
	mux.HandleFunc("POST /graphs/validate", s.validate)
	mux.HandleFunc("POST /graphs/grants", s.grants)
	mux.HandleFunc("DELETE /graphs/grants/{id}", s.revokeGrant)
	mux.HandleFunc("POST /graphs/open", s.openDoc)
	mux.HandleFunc("POST /graphs/stat", s.statDoc)
	mux.HandleFunc("POST /graphs/edit", s.editDoc)
	mux.HandleFunc("PUT /graphs/save", s.saveDoc)
	return s.guard(mux)
}

// guard enforces the Host header (DNS-rebinding defence) and the bearer token
// (CSRF defence). No CORS headers are ever written, so browsers cannot read
// responses cross-origin.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.addr != "" && r.Host != s.addr {
			writeError(w, http.StatusMisdirectedRequest, "host_mismatch", "request host is not the loopback address")
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 || s.Token == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Diagnostics []graph.Diagnostic `json:"diagnostics,omitempty"`
	// Detail carries the structured editing error; CurrentHash the hash a stale
	// client should reconcile with.
	Detail      any    `json:"detail,omitempty"`
	CurrentHash string `json:"current_hash,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var b errorBody
	b.Error.Code, b.Error.Message = code, message
	writeJSON(w, status, b)
}

// readJSON decodes a bounded request body, rejecting unknown fields.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.Store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store", "could not read the run store")
		return
	}
	for i := range runs {
		if live, ok := s.Hub.Live(runs[i].RunID); ok {
			if msg := live.Err(); msg != "" {
				runs[i].Error = "run stopped: could not be stored (" + msg + ")"
			} else if !live.Finished() {
				runs[i].Status = "running"
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.Store.Read(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "unknown run")
		return
	}
	if live, ok := s.Hub.Live(id); ok {
		if msg := live.Err(); msg != "" {
			rec.Summary.Error = "run stopped: could not be stored (" + msg + ")"
		} else if !live.Finished() {
			rec.Summary.Status = "running"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": rec.Summary, "graph": string(rec.Graph), "graph_summary": summarizeGraph(rec.Graph)})
}

// GraphSummary is the part of a graph a map needs: nodes and data-dependency
// edges. It is derived server-side so no client parses YAML.
type GraphSummary struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}
type GraphNode struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
}
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func summarizeGraph(source []byte) GraphSummary {
	summary := GraphSummary{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	def, err := graph.Parse(source)
	if err != nil {
		return summary
	}
	ids := make([]string, 0, len(def.Nodes))
	for id := range def.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := def.Nodes[id]
		node := GraphNode{ID: id, Type: n.Type}
		if n.Loop != nil {
			node.MaxAttempts = n.Loop.Attempts()
		}
		summary.Nodes = append(summary.Nodes, node)
	}
	seen := map[GraphEdge]bool{}
	for _, e := range def.Edges {
		from, _, _ := strings.Cut(string(e.From), ".")
		to, _, _ := strings.Cut(string(e.To), ".")
		edge := GraphEdge{From: from, To: to}
		if from != "" && to != "" && !seen[edge] {
			seen[edge] = true
			summary.Edges = append(summary.Edges, edge)
		}
	}
	sort.Slice(summary.Edges, func(i, j int) bool {
		if summary.Edges[i].From != summary.Edges[j].From {
			return summary.Edges[i].From < summary.Edges[j].From
		}
		return summary.Edges[i].To < summary.Edges[j].To
	})
	return summary
}

type startBody struct {
	GraphPath  string   `json:"graph_path"`
	GraphGrant string   `json:"graph_grant"`
	Inputs     []string `json:"inputs"`
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	var body startBody
	if !readJSON(w, r, &body) {
		return
	}
	if (body.GraphPath == "") == (body.GraphGrant == "") {
		writeError(w, http.StatusBadRequest, "bad_request", "exactly one of graph_path and graph_grant is required")
		return
	}
	graphPath := body.GraphPath
	if body.GraphGrant != "" {
		// A granted graph runs from its saved bytes, never from unsaved editor text.
		g := s.lookup(w, body.GraphGrant)
		if g == nil {
			return
		}
		if info, err := os.Lstat(g.path); err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusConflict, "not_regular", "the graph file is missing or no longer a regular file")
			return
		}
		graphPath = g.path
	}
	// The hub sink runs after StartFunc's own store write, so anything a client
	// can see is already durable.
	slot := s.Hub.NewSlot(nil)
	failed := make(chan error, 1)
	s.mu.Lock()
	if s.draining {
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "shutting_down", "the server is shutting down")
		return
	}
	s.active.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.active.Done()
		err := s.Start(s.baseCtx, graphPath, body.Inputs, slot.Sink, s.Hub)
		var storage *StorageError
		if errors.As(err, &storage) {
			slot.Fail(err) // a started run that could not be stored is stopped and flagged
		}
		failed <- err
	}()
	select {
	case <-slot.Started:
		writeJSON(w, http.StatusAccepted, map[string]string{"run_id": slot.RunID()})
	case err := <-failed:
		select {
		case <-slot.Started: // the run began and already finished; it is listed normally
			writeJSON(w, http.StatusAccepted, map[string]string{"run_id": slot.RunID()})
			return
		default:
		}
		var se *StartError
		if errors.As(err, &se) {
			var b errorBody
			b.Error.Code, b.Error.Message = "cannot_start", se.Message
			b.Diagnostics = se.Diagnostics
			writeJSON(w, se.Status, b)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "run failed to start")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "run ended without events")
	case <-r.Context().Done():
	}
}

type decisionBody struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment"`
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var body decisionBody
	if !readJSON(w, r, &body) {
		return
	}
	if body.Decision != events.ApprovalApproved && body.Decision != events.ApprovalRejected {
		writeError(w, http.StatusBadRequest, "bad_request", `decision must be "approved" or "rejected"`)
		return
	}
	rec, already, err := s.Hub.Submit(r.PathValue("id"), events.NodeRunID(r.PathValue("nodeRun")), body.Decision == events.ApprovalApproved, body.Comment)
	switch {
	case errors.Is(err, ErrInvalidComment):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, ErrNotPending):
		writeError(w, http.StatusNotFound, "not_pending", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not record decision")
	case already:
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "already_decided", "message": "this node run was already decided"}, "decision": rec})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"decision": rec})
	}
}

func (s *Server) approvals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"approvals": s.Hub.Pending()})
}

// validateBody names exactly one graph to validate: a file path (the original
// contract), unsaved text, or a grant (the granted file as it is on disk).
type validateBody struct {
	Path  string  `json:"path"`
	Text  *string `json:"text"`
	Grant string  `json:"grant"`
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	var body validateBody
	if !readJSONLimit(w, r, &body, docBodyLimit) {
		return
	}
	given := 0
	for _, set := range []bool{body.Path != "", body.Text != nil, body.Grant != ""} {
		if set {
			given++
		}
	}
	if given != 1 {
		writeError(w, http.StatusBadRequest, "bad_request", "exactly one of path, text and grant is required")
		return
	}
	if body.Path != "" {
		if s.Validate == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "path is required")
			return
		}
		status, diagnostics, err := s.Validate(body.Path)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "unreadable", err.Error())
			return
		}
		if diagnostics == nil {
			diagnostics = []graph.Diagnostic{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": status, "diagnostics": diagnostics})
		return
	}
	var text []byte
	if body.Text != nil {
		text = []byte(*body.Text)
	} else {
		g := s.lookup(w, body.Grant)
		if g == nil {
			return
		}
		data, _, err := readGranted(g)
		if err != nil {
			readError(w, err)
			return
		}
		text = data
	}
	if !checkText(w, text) {
		return
	}
	out := map[string]any{"diagnostics": []graph.Diagnostic{}}
	def, err := graph.Parse(text)
	if err != nil {
		out["status"], out["parse_error"] = "invalid", err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	diags := s.diagnostics(def)
	out["diagnostics"] = diags
	out["status"] = "valid"
	for _, d := range diags {
		if d.Severity == graph.SeverityError {
			out["status"] = "invalid"
			break
		}
	}
	if v, err := graph.SemanticVersion(def); err == nil {
		out["version"] = v
	}
	writeJSON(w, http.StatusOK, out)
}

// streamEvents replays a run from sequence zero and follows it to completion.
// Live runs are served from memory (no gaps, no duplicates: one index walks
// one slice); finished or foreign runs come from the store. Each SSE message
// carries one event as JSON with id = its sequence number.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	send := func(e events.Event) bool {
		data, err := json.Marshal(e)
		if err != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "id: %d\nevent: event\ndata: %s\n\n", e.Sequence, data)
		flusher.Flush()
		return err == nil
	}
	header := func() {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
	}
	if live, ok := s.Hub.Live(id); ok {
		header()
		next := 0
		for {
			batch, done, changed := live.From(next)
			for _, e := range batch {
				if !send(e) {
					return
				}
			}
			next += len(batch)
			if done && live.Err() != "" {
				body, _ := json.Marshal(map[string]string{"message": "the run was stopped because it could not be stored"})
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", body)
				flusher.Flush()
				return
			}
			if done {
				_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			select {
			case <-changed:
			case <-r.Context().Done():
				return
			}
		}
	}
	rec, err := s.Store.Read(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "unknown run")
		return
	}
	header()
	for _, e := range rec.Events {
		if !send(e) {
			return
		}
	}
	_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
	flusher.Flush()
}
