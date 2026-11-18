package api

import (
	"agentplane/internal/adapter/yamlgraph"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"agentplane/internal/adapter/yamlgraph/edit"
	"agentplane/internal/domain/graph"
)

// Graph document API (ADR-0021). A client addresses a graph by an opaque grant,
// never by path: the main process of the desktop registers the path the person
// chose, and only the grant reaches the renderer. Nothing here edits the file
// except save, which replaces it atomically.

const (
	// MaxDocumentBytes caps graph documents; every other body keeps its 64 KiB cap.
	MaxDocumentBytes = 1 << 20
	// MaxGrants bounds the grants one server process holds.
	MaxGrants = 256

	docBodyLimit = MaxDocumentBytes + 64<<10
)

type grant struct {
	id    string
	path  string // canonical: resolved directory + base name
	dir   string
	label string

	mu   sync.Mutex // serialises edit and save on this grant
	text []byte     // the text last opened, edited or saved: the base of the next edit
	hash string
}

type docState struct {
	mu     sync.Mutex
	grants map[string]*grant
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Server) grantByID(id string) *grant {
	s.docs.mu.Lock()
	defer s.docs.mu.Unlock()
	return s.docs.grants[id]
}

// readJSONLimit is readJSON with a caller-chosen body cap.
func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("request body is larger than %d bytes", limit))
			return false
		}
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}

// checkText enforces what a graph document may contain: bounded size, valid
// UTF-8, no NUL.
func checkText(w http.ResponseWriter, text []byte) bool {
	switch {
	case len(text) > MaxDocumentBytes:
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a graph document may be at most %d bytes", MaxDocumentBytes))
	case !utf8.Valid(text) || strings.IndexByte(string(text), 0) >= 0:
		writeError(w, http.StatusUnprocessableEntity, "not_text", "the document is not valid UTF-8 text without NUL bytes")
	default:
		return true
	}
	return false
}

var yamlExt = map[string]bool{".yaml": true, ".yml": true}

type grantBody struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // "open" (an existing file) or "create" (a file that does not exist yet)
}

// grants registers a canonical path. The desktop's main process calls it; the
// renderer cannot reach it (ADR-0021 allowlist).
func (s *Server) grants(w http.ResponseWriter, r *http.Request) {
	var body grantBody
	if !readJSON(w, r, &body) {
		return
	}
	if body.Mode != "open" && body.Mode != "create" {
		writeError(w, http.StatusBadRequest, "bad_request", `mode must be "open" or "create"`)
		return
	}
	if body.Path == "" || !filepath.IsAbs(body.Path) || strings.IndexByte(body.Path, 0) >= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "path must be an absolute path")
		return
	}
	clean := filepath.Clean(body.Path)
	if !yamlExt[strings.ToLower(filepath.Ext(clean))] {
		writeError(w, http.StatusUnprocessableEntity, "not_yaml", "graph files must end in .yaml or .yml")
		return
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "bad_directory", "the directory does not exist or cannot be resolved")
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		writeError(w, http.StatusUnprocessableEntity, "bad_directory", "the parent is not a directory")
		return
	}
	final := filepath.Join(dir, filepath.Base(clean))
	info, statErr := os.Lstat(final) // Lstat: a symlink final component is never followed
	switch body.Mode {
	case "open":
		if statErr != nil {
			writeError(w, http.StatusNotFound, "not_found", "the file does not exist")
			return
		}
		if !info.Mode().IsRegular() {
			writeError(w, http.StatusUnprocessableEntity, "not_regular", "the path is not a regular file (symbolic links are not followed)")
			return
		}
		if info.Size() > MaxDocumentBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a graph document may be at most %d bytes", MaxDocumentBytes))
			return
		}
	case "create":
		if statErr == nil {
			writeError(w, http.StatusConflict, "exists", "the file already exists")
			return
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			writeError(w, http.StatusUnprocessableEntity, "bad_directory", "the path cannot be inspected")
			return
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "cannot create a grant")
		return
	}
	g := &grant{id: hex.EncodeToString(raw[:]), path: final, dir: dir, label: filepath.Base(final)}
	s.docs.mu.Lock()
	if s.docs.grants == nil {
		s.docs.grants = map[string]*grant{}
	}
	if len(s.docs.grants) >= MaxGrants {
		s.docs.mu.Unlock()
		writeError(w, http.StatusConflict, "too_many_grants", fmt.Sprintf("at most %d graphs can be open at once", MaxGrants))
		return
	}
	s.docs.grants[g.id] = g
	s.docs.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"grant": g.id, "label": g.label, "exists": body.Mode == "open"})
}

// revokeGrant forgets a grant when its document is closed. Like grants, it is a
// main-process route the renderer cannot reach.
func (s *Server) revokeGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.docs.mu.Lock()
	_, ok := s.docs.grants[id]
	delete(s.docs.grants, id)
	s.docs.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_grant", "unknown grant")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readGranted reads the granted file's current bytes, refusing anything that is
// no longer a regular file.
func readGranted(g *grant) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(g.path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, info, errNotRegular
	}
	if info.Size() > MaxDocumentBytes {
		return nil, info, errTooLarge
	}
	data, err := os.ReadFile(g.path)
	return data, info, err
}

var (
	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("document too large")
)

func readError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "not_found", "the file no longer exists")
	case errors.Is(err, errNotRegular):
		writeError(w, http.StatusConflict, "not_regular", "the path is no longer a regular file")
	case errors.Is(err, errTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a graph document may be at most %d bytes", MaxDocumentBytes))
	default:
		writeError(w, http.StatusInternalServerError, "unreadable", "the file cannot be read")
	}
}

type docResponse struct {
	Text            string          `json:"text"`
	Hash            string          `json:"hash"`
	Version         string          `json:"version,omitempty"`
	Definition      *definitionDTO  `json:"definition,omitempty"`
	Diagnostics     []diagnosticDTO `json:"diagnostics"`
	ParseError      string          `json:"parse_error,omitempty"`
	ParseLine       int             `json:"parse_line,omitempty"`
	ParseColumn     int             `json:"parse_column,omitempty"`
	ReadonlyCanvas  bool            `json:"readonly_canvas"`
	ReadonlyReasons []string        `json:"readonly_reasons,omitempty"`
	LineEnding      string          `json:"line_ending"`
	Label           string          `json:"label"`
	Results         []edit.OpResult `json:"results,omitempty"`
	// NodeTypes holds the field descriptors of the node types in the graph, or
	// of every known type on request (F2.2).
	NodeTypes map[string]nodeTypeDTO `json:"node_types"`
	// Nodes holds per-node inspector data; absent when the text does not parse.
	Nodes map[string]nodeInfoDTO `json:"node_info,omitempty"`
}

// diagnostics runs the full validation: graph rules and, when the composition
// root provides it, every node's static configuration.
func (s *Server) diagnostics(def *graph.Definition) []graph.Diagnostic {
	var diags []graph.Diagnostic
	if s.ValidateDefinition != nil {
		diags = s.ValidateDefinition(def)
	} else {
		diags = graph.Validate(def).Diagnostics
	}
	if diags == nil {
		diags = []graph.Diagnostic{}
	}
	return diags
}

// parseErrorMessage is the text shown to the user. The YAML library cuts long
// scalars at a byte count, which can split a multi-byte character; the position
// lookups keep using the raw message because they match that cut.
func parseErrorMessage(err error) string {
	return strings.ToValidUTF8(err.Error(), "�")
}

func (s *Server) document(g *grant, text []byte, allTypes bool) docResponse {
	res := docResponse{Text: string(text), Hash: hashOf(text), Diagnostics: []diagnosticDTO{}, Label: g.label, LineEnding: "lf"}
	res.NodeTypes = s.nodeTypesFor(nil, allTypes)
	if strings.Contains(res.Text, "\r\n") {
		res.LineEnding = "crlf"
	}
	if reasons := edit.Inspect(text); len(reasons) > 0 {
		res.ReadonlyCanvas, res.ReadonlyReasons = true, reasons
	}
	def, err := yamlgraph.Parse(text)
	if err != nil {
		res.ParseError = parseErrorMessage(err)
		res.ParseLine = edit.ParseErrorLine(err.Error())
		res.ParseColumn = edit.ParseErrorColumn(text, err.Error())
		return res
	}
	res.Definition = toDefinitionDTO(def)
	res.NodeTypes = s.nodeTypesFor(def, allTypes)
	res.Nodes = s.nodeInfo(text, def)
	if v, err := yamlgraph.SemanticVersion(def); err == nil {
		res.Version = v
	}
	res.Diagnostics = withLines(text, s.diagnostics(def))
	return res
}

type grantRef struct {
	Grant string `json:"grant"`
}

func (s *Server) lookup(w http.ResponseWriter, id string) *grant {
	g := s.grantByID(id)
	if g == nil {
		writeError(w, http.StatusNotFound, "unknown_grant", "unknown grant: open the graph again")
	}
	return g
}

// openBody is grantRef plus the request for every node type's descriptors.
type openBody struct {
	Grant        string `json:"grant"`
	AllNodeTypes bool   `json:"all_node_types"`
}

func (s *Server) openDoc(w http.ResponseWriter, r *http.Request) {
	var body openBody
	if !readJSON(w, r, &body) {
		return
	}
	g := s.lookup(w, body.Grant)
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	data, _, err := readGranted(g)
	if err != nil {
		readError(w, err)
		return
	}
	if !checkText(w, data) {
		return
	}
	g.text, g.hash = data, hashOf(data)
	writeJSON(w, http.StatusOK, s.document(g, data, body.AllNodeTypes))
}

func (s *Server) statDoc(w http.ResponseWriter, r *http.Request) {
	var body grantRef
	if !readJSON(w, r, &body) {
		return
	}
	g := s.lookup(w, body.Grant)
	if g == nil {
		return
	}
	data, info, err := readGranted(g)
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, http.StatusOK, map[string]any{"exists": false})
	case err != nil:
		readError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"exists": true, "hash": hashOf(data), "size": info.Size()})
	}
}

type editBody struct {
	Grant      string          `json:"grant"`
	BaseHash   string          `json:"base_hash"`
	Text       *string         `json:"text"`
	OpsVersion int             `json:"ops_version"`
	Ops        json.RawMessage `json:"ops"`
	// AllNodeTypes asks for every node type's descriptors, as on open.
	AllNodeTypes bool `json:"all_node_types"`
}

func editFailure(w http.ResponseWriter, err error) {
	var e *edit.Error
	if !errors.As(err, &e) {
		writeError(w, http.StatusInternalServerError, "internal", "the edit failed")
		return
	}
	var b errorBody
	b.Error.Code, b.Error.Message = e.Code, e.Message
	b.Detail = e
	writeJSON(w, http.StatusUnprocessableEntity, b)
}

// editDoc applies ADR-0020 operations. It never writes the file. The text the
// operations apply to is the client's, identified by base_hash: either the text
// the server holds for this grant, or text the client sends (after typing in the
// YAML view), which must match its hash.
func (s *Server) editDoc(w http.ResponseWriter, r *http.Request) {
	var body editBody
	if !readJSONLimit(w, r, &body, docBodyLimit) {
		return
	}
	g := s.lookup(w, body.Grant)
	if g == nil {
		return
	}
	if body.OpsVersion != edit.OpsVersion {
		writeError(w, http.StatusBadRequest, "unsupported_ops_version", fmt.Sprintf("ops_version must be %d", edit.OpsVersion))
		return
	}
	ops, err := edit.Decode(body.Ops)
	if err != nil {
		editFailure(w, err)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var base []byte
	if body.Text != nil {
		base = []byte(*body.Text)
		if !checkText(w, base) {
			return
		}
		if hashOf(base) != body.BaseHash {
			writeError(w, http.StatusBadRequest, "hash_mismatch", "base_hash is not the hash of the text sent")
			return
		}
	} else {
		if g.text == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "text is required for a graph that has not been opened")
			return
		}
		if body.BaseHash != g.hash {
			var b errorBody
			b.Error.Code, b.Error.Message = "stale_base", "the text changed since base_hash: open the graph again"
			b.CurrentHash = g.hash
			writeJSON(w, http.StatusConflict, b)
			return
		}
		base = g.text
	}
	res, err := edit.Apply(base, ops)
	if err != nil {
		editFailure(w, err)
		return
	}
	g.text, g.hash = res.Text, hashOf(res.Text)
	doc := s.document(g, res.Text, body.AllNodeTypes)
	doc.Results = res.Results
	writeJSON(w, http.StatusOK, doc)
}

type saveBody struct {
	Grant string `json:"grant"`
	// BaseHash is the hash of the file's bytes the client last saw; null creates
	// a file that must not exist yet.
	BaseHash *string `json:"base_hash"`
	Text     string  `json:"text"`
}

var errExists = errors.New("file exists")

// saveDoc replaces the granted file atomically: a temporary file in the same
// directory is written, synced and renamed over the target. Any failure leaves
// the previous file untouched.
func (s *Server) saveDoc(w http.ResponseWriter, r *http.Request) {
	var body saveBody
	if !readJSONLimit(w, r, &body, docBodyLimit) {
		return
	}
	g := s.lookup(w, body.Grant)
	if g == nil {
		return
	}
	data := []byte(body.Text)
	if !checkText(w, data) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// The directory must still be the one that was granted.
	dir, err := filepath.EvalSymlinks(filepath.Dir(g.path))
	if err != nil || dir != g.dir {
		writeError(w, http.StatusConflict, "path_changed", "the directory of the graph changed since it was opened")
		return
	}
	mode := os.FileMode(0o644) // graph files are meant to be committed
	info, statErr := os.Lstat(g.path)
	createOnly := false
	switch {
	case statErr == nil:
		if !info.Mode().IsRegular() {
			writeError(w, http.StatusConflict, "not_regular", "the path is no longer a regular file")
			return
		}
		if body.BaseHash == nil {
			writeError(w, http.StatusConflict, "exists", "the file already exists; send the hash of the version you last saw")
			return
		}
		current, err := os.ReadFile(g.path)
		if err != nil {
			readError(w, err)
			return
		}
		if hashOf(current) != *body.BaseHash {
			var b errorBody
			b.Error.Code, b.Error.Message = "conflict", "the file changed on disk since you last saw it"
			b.CurrentHash = hashOf(current)
			writeJSON(w, http.StatusConflict, b)
			return
		}
		mode = info.Mode().Perm()
	case errors.Is(statErr, os.ErrNotExist):
		if body.BaseHash != nil {
			writeError(w, http.StatusConflict, "gone", "the file no longer exists")
			return
		}
		createOnly = true
	default:
		writeError(w, http.StatusInternalServerError, "unreadable", "the file cannot be inspected")
		return
	}

	if err := atomicWrite(g.dir, g.path, data, mode, createOnly); err != nil {
		if errors.Is(err, errExists) {
			writeError(w, http.StatusConflict, "exists", "the file was created by someone else; open it again")
			return
		}
		writeError(w, http.StatusInternalServerError, "write_failed", "the file could not be written; the previous version is unchanged")
		return
	}
	g.text, g.hash = data, hashOf(data)
	writeJSON(w, http.StatusOK, map[string]any{"hash": g.hash, "size": len(data)})
}

// atomicWrite writes data to a temporary file next to final and renames it into
// place. With createOnly the final name must not exist (a hard link fails if it
// does, so two creators cannot both win).
func atomicWrite(dir, final string, data []byte, mode os.FileMode, createOnly bool) (err error) {
	tmp, err := os.CreateTemp(dir, ".agentplane-save-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if createOnly {
		if err = os.Link(name, final); err != nil {
			if errors.Is(err, os.ErrExist) {
				err = errExists
				return err
			}
			// A filesystem without hard links: check once more, then rename.
			if _, statErr := os.Lstat(final); statErr == nil {
				err = errExists
				return err
			}
			err = os.Rename(name, final)
		} else {
			err = os.Remove(name)
		}
	} else {
		err = os.Rename(name, final)
	}
	if err != nil {
		return err
	}
	if d, e := os.Open(dir); e == nil { // best effort: make the rename durable
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
