package runtime

import (
	"bytes"
	"errors"

	"agentplane/internal/events"
)

var ErrUnsafeProjectionIdentity = errors.New("required execution identity intersects a configured private value")

// ValidateProjectionIdentities checks deterministic identities before a
// desktop preflight token is issued. It never includes an identity or a
// configured value in the returned error.
func ValidateProjectionIdentities(graphID, graphVersion string, nodeIDs []string, redactions []string) error {
	template, err := newLogRedactor(redactions)
	if err != nil {
		return errors.New("invalid projection redactions")
	}
	return validateProjectionIdentities(template, graphID, graphVersion, nodeIDs)
}

func validateProjectionIdentities(template *logRedactor, graphID, graphVersion string, nodeIDs []string) error {
	if identityRedactedBy(template, graphID) || identityRedactedBy(template, graphVersion) {
		return ErrUnsafeProjectionIdentity
	}
	for _, id := range nodeIDs {
		if identityRedactedBy(template, id) {
			return ErrUnsafeProjectionIdentity
		}
	}
	return nil
}

func identityRedactedBy(template *logRedactor, value string) bool {
	if template == nil || value == "" {
		return false
	}
	r := template.clone()
	safe := append([]byte(nil), r.Write(events.Stdout, []byte(value))...)
	safe = append(safe, r.Flush(events.Stdout)...)
	return !bytes.Equal(safe, []byte(value))
}

func (m *runMachine) validateGeneratedProjectionIdentities(template *logRedactor) error {
	if identityRedactedBy(template, string(m.result.RunID)) {
		return ErrUnsafeProjectionIdentity
	}
	for _, id := range m.ids {
		if identityRedactedBy(template, string(m.nodes[id].NodeRunID)) {
			return ErrUnsafeProjectionIdentity
		}
	}
	return nil
}
