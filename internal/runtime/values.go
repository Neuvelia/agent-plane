package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
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
	return preflightUTF8Redactions("value-recording", redactions, redactor)
}

// preflightUTF8Redactions is the shared rule of every feature that rewrites
// redacted text: secrets and the marker must be valid UTF-8.
func preflightUTF8Redactions(feature string, redactions []string, redactor *logRedactor) error {
	for _, value := range redactions {
		if !utf8.ValidString(value) {
			return fmt.Errorf("invalid %s redactions: values must be valid UTF-8", feature)
		}
	}
	if !utf8.Valid(redactor.marker) {
		return fmt.Errorf("invalid %s redactions: no UTF-8-safe secret-absent marker is available", feature)
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
	if v.Type() == graph.ValueTypeJSON && jsonStringsContainSecret([]byte(raw), r.redactor.secrets) {
		// A JSON string may spell a secret with any mix of escapes (\", \\,
		// A, é, surrogate pairs), so byte redaction cannot be complete.
		// Decoding every key and string and omitting the text when one holds a
		// secret is: the text is never carried, only the size and digest.
		p.Truncated, p.Redacted = true, true
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

// jsonStringsContainSecret reports whether any decoded object key or string in
// raw contains a secret. Numbers, literals and structure are raw text and are
// already covered by byte redaction. Undecodable input counts as containing one.
func jsonStringsContainSecret(raw []byte, secrets [][]byte) bool {
	if len(secrets) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		s, ok := token.(string)
		if !ok {
			continue
		}
		for _, secret := range secrets {
			if strings.Contains(s, string(secret)) {
				return true
			}
		}
	}
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
