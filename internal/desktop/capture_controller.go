package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"agentplane/internal/capture"
	"agentplane/internal/events"
	"agentplane/internal/gitstate"
	"agentplane/internal/graph"
	"agentplane/internal/history"
	"agentplane/internal/runtime"
)

const maxActiveEvidenceDescriptors = 4096

type evidenceWriter interface {
	CaptureEvidence(context.Context, history.EvidenceCapture) error
}

type captureController struct {
	mu            sync.Mutex
	writer        evidenceWriter
	resolve       capture.Resolver
	redactor      *runtime.CaptureRedactor
	forbidden     []string
	budget        capture.Budget
	descriptors   []history.EvidenceDescriptor
	uncommitted   map[string]bool
	generation    uint64
	failed        bool
	complete      bool
	onUnavailable func()
}

func newCaptureController(writer evidenceWriter, resolver capture.Resolver, redactions []string, onUnavailable func()) (*captureController, error) {
	redactor, err := runtime.NewCaptureRedactor(redactions)
	if err != nil || writer == nil || resolver == nil {
		return nil, errors.New("attempt capture is unavailable")
	}
	return &captureController{writer: writer, resolve: resolver, redactor: redactor, forbidden: append([]string(nil), redactions...), uncommitted: make(map[string]bool), complete: true, onUnavailable: onUnavailable}, nil
}

func newCaptureID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func (c *captureController) markFailed() {
	c.mu.Lock()
	c.failed = true
	c.complete = false
	c.mu.Unlock()
	if c.onUnavailable != nil {
		c.onUnavailable()
	}
}

func (c *captureController) failedWrite() bool   { c.mu.Lock(); defer c.mu.Unlock(); return c.failed }
func (c *captureController) completeState() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.complete }

// appendActive keeps a reserved sentinel slot for overflow. The active index
// is the sole source for history.evidence.list while a run is executing.
func (c *captureController) appendActive(item history.EvidenceDescriptor) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.descriptors) >= maxActiveEvidenceDescriptors {
		if !c.failed {
			c.failed = true
			c.complete = false
			c.descriptors = append(c.descriptors, history.EvidenceDescriptor{EvidenceID: "capture-overflow", RunID: item.RunID, AttemptID: item.AttemptID, NodeRunID: item.NodeRunID, Kind: "diff", Availability: "unavailable", OmissionReason: "capture_failed", CreatedAt: time.Now().UTC()})
			c.uncommitted["capture-overflow"] = true
			c.generation++
		}
		return false
	}
	c.descriptors = append(c.descriptors, item)
	c.generation++
	if item.Availability != "available" || item.Redacted {
		c.complete = false
	}
	return true
}

func (c *captureController) record(ids runtime.ExecutionCorrelation, kind, sourcePath, group, reason string, content []byte) {
	if c == nil {
		return
	}
	if c.failedWrite() {
		return
	}
	if sourcePath != "" && !bytes.Equal(c.redactor.Redact([]byte(sourcePath)), []byte(sourcePath)) {
		sourcePath = ""
		reason = "unsafe_path"
		content = nil
	}
	id, err := newCaptureID()
	if err != nil {
		c.markFailed()
		return
	}
	created := time.Now().UTC()
	record := history.EvidenceCapture{EvidenceID: id, RunID: string(ids.RunID), AttemptID: string(ids.AttemptID), NodeRunID: string(ids.NodeRunID), Kind: kind, SourcePath: sourcePath, CaptureGroup: group, CreatedAt: created, OmissionReason: reason}
	item := history.EvidenceDescriptor{EvidenceID: id, RunID: record.RunID, AttemptID: record.AttemptID, NodeRunID: record.NodeRunID, Kind: kind, SourcePath: sourcePath, CaptureGroup: group, CreatedAt: created}
	if reason == "" {
		if len(content) > capture.MaxFileBytes {
			reason = "size_limit"
		} else {
			sanitized := c.redactor.Redact(content)
			if len(sanitized) > capture.MaxFileBytes || len(sanitized) > len(content) && !c.budget.Reserve(int64(len(sanitized)-len(content))) {
				reason = "size_limit"
			} else {
				value, parseErr := history.ParseSafeEvidence(sanitized, c.forbidden)
				if parseErr != nil {
					reason = "redaction_failed"
				} else {
					record.Content = &value
					record.Redacted = !bytes.Equal(sanitized, content)
					length := int64(len(sanitized))
					item.ByteLength = &length
					digest := sha256.Sum256(sanitized)
					item.Digest = hex.EncodeToString(digest[:])
					item.Redacted = record.Redacted
				}
			}
		}
	}
	if reason != "" {
		record.OmissionReason = reason
		record.Content = nil
		record.Redacted = false
		item.Availability = "unavailable"
		item.OmissionReason = reason
		item.ByteLength = nil
		item.Digest = ""
		item.Redacted = false
	} else {
		item.Availability = "available"
	}
	commitCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	err = c.writer.CaptureEvidence(commitCtx, record)
	stop()
	if err != nil {
		c.mu.Lock()
		c.uncommitted[id] = true
		c.mu.Unlock()
		item.Availability = "unavailable"
		item.OmissionReason = "capture_failed"
		item.ByteLength = nil
		item.Digest = ""
		item.Redacted = false
		c.appendActive(item)
		c.markFailed()
		return
	}
	if !c.appendActive(item) {
		c.markFailed()
	}
}

func (c *captureController) observe(node graph.NodeDefinition, observation runtime.ExecutionObservation, produce func(runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error)) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	scope := agentScope(node)
	ids := observation.Correlation
	var before capture.Snapshot
	var beforeErr error
	var gitBefore gitstate.Baseline
	var gitBeforeErr error
	var gitWorkspace capture.Workspace
	var beforeCalled, afterCalled bool
	previousBefore, previousAfter := observation.BeforeProduce, observation.AfterProduce
	observation.BeforeProduce = func(produceCtx context.Context) {
		beforeCalled = true
		gitWorkspace, gitBeforeErr = c.resolve()
		if gitBeforeErr == nil {
			var actual string
			actual, gitBeforeErr = capture.DirectoryIdentity(gitWorkspace.Path)
			if gitBeforeErr == nil && actual != gitWorkspace.Identity {
				gitBeforeErr = capture.ErrWorkspaceChanged
			}
		}
		if gitBeforeErr == nil {
			gitCtx, stop := context.WithTimeout(produceCtx, 10*time.Second)
			gitBefore, gitBeforeErr = gitstate.Capture(gitCtx, gitWorkspace.Path)
			stop()
		}
		if len(scope) != 0 {
			before, beforeErr = capture.Take(produceCtx, c.resolve, scope, &c.budget)
		}
		if previousBefore != nil {
			previousBefore(produceCtx)
		}
	}
	observation.AfterProduce = func(produceCtx context.Context) {
		// The producer may already have cancelled its context, but its files may
		// have changed. Capture before the embedded Gate or retry can run.
		afterCalled = true
		postCtx, stop := context.WithTimeout(context.WithoutCancel(produceCtx), 10*time.Second)
		var after capture.Snapshot
		var afterErr error
		if len(scope) != 0 {
			after, afterErr = capture.Take(postCtx, c.resolve, scope, &c.budget)
		}
		c.finishGitObservation(postCtx, ids, gitWorkspace, gitBefore, gitBeforeErr)
		stop()
		if len(scope) == 0 {
			c.record(ids, "diff", "", "", "diff_unavailable", nil)
		} else {
			c.finishCapture(ids, before, beforeErr, after, afterErr)
		}
		if previousAfter != nil {
			previousAfter(produceCtx)
		}
	}
	values, evidence, produceErr := produce(observation)
	if !beforeCalled || !afterCalled {
		c.record(ids, "diff", "", "", "capture_failed", nil)
	}
	return values, evidence, produceErr
}

func (c *captureController) finishGitObservation(ctx context.Context, ids runtime.ExecutionCorrelation, workspace capture.Workspace, before gitstate.Baseline, beforeErr error) {
	if beforeErr != nil {
		c.record(ids, "git_observation", "", "", "diff_unavailable", nil)
		return
	}
	selected, err := c.resolve()
	if err == nil && selected != workspace {
		err = capture.ErrWorkspaceChanged
	}
	if err == nil {
		var actual string
		actual, err = capture.DirectoryIdentity(selected.Path)
		if err == nil && actual != selected.Identity {
			err = capture.ErrWorkspaceChanged
		}
	}
	var changed []string
	if err == nil {
		changed, err = gitstate.Changed(ctx, before)
	}
	if err != nil {
		c.record(ids, "git_observation", "", "", "diff_unavailable", nil)
		return
	}
	content, err := json.Marshal(struct {
		Schema       int `json:"schema"`
		ChangedPaths int `json:"changed_paths"`
	}{Schema: 1, ChangedPaths: len(changed)})
	if err != nil {
		c.record(ids, "git_observation", "", "", "capture_failed", nil)
		return
	}
	if !c.budget.Reserve(int64(len(content))) {
		c.record(ids, "git_observation", "", "", "size_limit", nil)
		return
	}
	c.record(ids, "git_observation", "", "", "", content)
}

// ActiveEvidenceUnavailable returns no bytes and no database path. Only an
// explicitly failed in-memory insert is eligible for this fallback.
func (s *RunService) ActiveEvidenceUnavailable(evidenceID string) (history.EvidenceChunk, bool) {
	if s == nil || evidenceID == "" {
		return history.EvidenceChunk{}, false
	}
	s.mu.Lock()
	controllers := make([]*captureController, 0, len(s.byID))
	for _, record := range s.byID {
		if record.historyAvailability != HistoryArchived && record.capture != nil {
			controllers = append(controllers, record.capture)
		}
	}
	s.mu.Unlock()
	for _, controller := range controllers {
		controller.mu.Lock()
		found := controller.uncommitted[evidenceID]
		controller.mu.Unlock()
		if found {
			return history.EvidenceChunk{Availability: "unavailable", OmissionReason: "capture_failed", End: true}, true
		}
	}
	return history.EvidenceChunk{}, false
}

func (c *captureController) finishCapture(ids runtime.ExecutionCorrelation, before capture.Snapshot, beforeErr error, after capture.Snapshot, afterErr error) {
	if beforeErr != nil || afterErr != nil {
		reason := "capture_failed"
		if errors.Is(beforeErr, capture.ErrWorkspaceChanged) || errors.Is(afterErr, capture.ErrWorkspaceChanged) || errors.Is(beforeErr, capture.ErrUnsafePath) || errors.Is(afterErr, capture.ErrUnsafePath) {
			reason = "unsafe_path"
		}
		c.record(ids, "diff", "", "", reason, nil)
		return
	}
	changes, err := capture.Changes(before, after)
	if err != nil {
		c.record(ids, "diff", "", "", "unsafe_path", nil)
		return
	}
	if len(changes) == 0 {
		c.record(ids, "diff", "", "", "", []byte{})
		return
	}
	for _, change := range changes {
		group, err := newCaptureID()
		if err != nil {
			c.markFailed()
			break
		}
		left, right := change.Before, change.After
		if left.Unavailable != "" {
			c.record(ids, "preimage", change.Path, group, left.Unavailable, nil)
		} else if left.Present {
			c.record(ids, "preimage", change.Path, group, "", left.Content)
		} else {
			c.record(ids, "preimage", change.Path, group, "", []byte{})
		}
		if right.Unavailable != "" {
			c.record(ids, "postimage", change.Path, group, right.Unavailable, nil)
		} else if right.Present {
			c.record(ids, "postimage", change.Path, group, "", right.Content)
		} else {
			c.record(ids, "postimage", change.Path, group, "", []byte{})
		}
		if left.Unavailable != "" || right.Unavailable != "" {
			c.record(ids, "diff", change.Path, group, "diff_unavailable", nil)
			continue
		}
		diff, ok := capture.TextDiff(left.Content, right.Content)
		if !ok {
			c.record(ids, "diff", change.Path, group, "diff_unavailable", nil)
		} else if !c.budget.Reserve(int64(len(diff))) {
			c.record(ids, "diff", change.Path, group, "size_limit", nil)
		} else {
			c.record(ids, "diff", change.Path, group, "", diff)
		}
	}
}

func agentScope(node graph.NodeDefinition) []string {
	raw, present := node.Config["allowed_paths"]
	if !present {
		return nil
	}
	if strings, ok := raw.([]string); ok {
		return append([]string(nil), strings...)
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil
		}
		result = append(result, value)
	}
	return result
}

type activeEvidenceCursor struct {
	RunID, AttemptID string
	Generation       uint64
	After            string
}

const activeEvidenceTimeLayout = "2006-01-02T15:04:05.000000000Z"

func (c *captureController) page(runID, attemptID, cursor string, limit int) (history.EvidenceDescriptorPage, *ServiceError) {
	if c == nil || runID == "" || limit < 1 || limit > 256 || len(cursor) > 4096 {
		return history.EvidenceDescriptorPage{}, protocolError("invalid_request", "Invalid evidence descriptor page")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var after string
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		var parsed activeEvidenceCursor
		if err != nil || json.Unmarshal(data, &parsed) != nil || parsed.RunID != runID || parsed.AttemptID != attemptID || parsed.Generation != c.generation || parsed.After == "" {
			return history.EvidenceDescriptorPage{}, protocolError("stale_cursor", "Evidence descriptor page changed")
		}
		after = parsed.After
	}
	items := append([]history.EvidenceDescriptor(nil), c.descriptors...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].EvidenceID < items[j].EvidenceID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	page := history.EvidenceDescriptorPage{Descriptors: make([]history.EvidenceDescriptor, 0, limit), Generation: c.generation, Complete: c.complete}
	for _, item := range items {
		if attemptID != "" && item.AttemptID != attemptID {
			continue
		}
		key := item.CreatedAt.UTC().Format(activeEvidenceTimeLayout) + "/" + item.EvidenceID
		if key <= after {
			continue
		}
		if len(page.Descriptors) == limit {
			last := page.Descriptors[len(page.Descriptors)-1]
			encoded, _ := json.Marshal(activeEvidenceCursor{RunID: runID, AttemptID: attemptID, Generation: c.generation, After: last.CreatedAt.UTC().Format(activeEvidenceTimeLayout) + "/" + last.EvidenceID})
			page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
			break
		}
		page.Descriptors = append(page.Descriptors, item)
	}
	return page, nil
}

// ActiveEvidenceDescriptors serves the in-memory capture index while this
// service still owns an unarchived run. The bool tells handlers whether to
// fall back to the immutable SQLite history for completed/older runs.
func (s *RunService) ActiveEvidenceDescriptors(runID events.RunID, attemptID, cursor string, limit int) (history.EvidenceDescriptorPage, bool, *ServiceError) {
	if s == nil || runID == "" {
		return history.EvidenceDescriptorPage{}, false, protocolError("invalid_request", "Invalid run identity")
	}
	s.mu.Lock()
	record := s.byID[runID]
	var controller *captureController
	if record != nil && record.historyAvailability != HistoryArchived {
		controller = record.capture
	}
	s.mu.Unlock()
	if controller == nil {
		return history.EvidenceDescriptorPage{}, false, nil
	}
	page, err := controller.page(string(runID), attemptID, cursor, limit)
	return page, true, err
}
