package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"unicode/utf8"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

// valueRecorder is coordinator-owned state for opt-in value recording
// (ADR-0023). It is nil when recording is off, so no value_recorded fact and
// no extra work exist for such runs.
type valueRecorder struct {
	redactor *logRedactor
	budget   int
}

func newValueRecorder(enabled bool, redactor *logRedactor) *valueRecorder {
	if !enabled {
		return nil
	}
	return &valueRecorder{redactor: redactor, budget: events.MaxRunValueBytes}
}

// preflightValueRecording mirrors the observed-executor rule: JSON must not
// rewrite redacted text, so secrets and the marker must be valid UTF-8.
func preflightValueRecording(enabled bool, redactions []string, redactor *logRedactor) error {
	if !enabled {
		return nil
	}
	for _, value := range redactions {
		if !utf8.ValidString(value) {
			return errors.New("invalid value-recording redactions: values must be valid UTF-8")
		}
	}
	if !utf8.Valid(redactor.marker) {
		return errors.New("invalid value-recording redactions: no UTF-8-safe secret-absent marker is available")
	}
	return nil
}

// valueText is the documented text form of each value type; an artifact
// reference contributes only its reference string.
func valueText(v graph.Value) string {
	switch v.Type() {
	case graph.ValueTypeString:
		s, _ := v.AsString()
		return s
	case graph.ValueTypeNumber:
		n, _ := v.AsNumber()
		return string(n)
	case graph.ValueTypeBoolean:
		if b, _ := v.AsBoolean(); b {
			return "true"
		}
		return "false"
	case graph.ValueTypeJSON:
		j, _ := v.AsJSON()
		return string(j)
	case graph.ValueTypeArtifactRef:
		r, _ := v.AsArtifactRef()
		return r
	}
	return ""
}

// payload redacts the whole text before cutting it, so a secret spanning the
// cut is replaced, and charges carried bytes to the run budget.
func (r *valueRecorder) payload(port string, v graph.Value, nodeRunID events.NodeRunID, attemptID events.AttemptID) events.ValueRecordedPayload {
	raw := valueText(v)
	c := r.redactor.clone()
	redactedBytes := append([]byte{}, c.Write(events.Stdout, []byte(raw))...)
	redactedBytes = append(redactedBytes, c.Flush(events.Stdout)...)
	text := string(redactedBytes)
	sum := sha256.Sum256(redactedBytes)
	p := events.ValueRecordedPayload{
		NodeRunID: nodeRunID, AttemptID: attemptID, Port: port, Type: string(v.Type()),
		SizeBytes: int64(len(text)), SHA256: hex.EncodeToString(sum[:]), Redacted: text != raw,
	}
	if !utf8.ValidString(text) {
		p.Truncated = true
		return p
	}
	limit := events.MaxValueBytes
	if r.budget < limit {
		limit = r.budget
	}
	carried, cut := truncateUTF8(text, limit)
	if cut && carried == "" {
		p.Truncated = true
		return p
	}
	r.budget -= len(carried)
	p.Truncated = cut
	p.Value = &carried
	return p
}

func (m *runMachine) recordGraphInputs(inputs map[string]graph.Value) {
	if m.values == nil {
		return
	}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m.appendEvent(events.Event{Type: events.ValueRecorded, Payload: m.values.payload(events.GraphInputPortPrefix+name, inputs[name], "", "")})
	}
}

// recordAcceptedOutputs runs on the coordinator immediately before a node's
// succeeded transition, using the outputs the run has accepted.
func (m *runMachine) recordAcceptedOutputs(id string, attemptID events.AttemptID) {
	if m.values == nil {
		return
	}
	node := m.nodes[id]
	for _, port := range node.Outputs {
		m.appendEvent(events.Event{Type: events.ValueRecorded, NodeID: id, NodeRunID: node.NodeRunID, AttemptID: attemptID, Payload: m.values.payload(port.Name, port.Value, node.NodeRunID, attemptID)})
	}
}
