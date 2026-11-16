// Package system implements the engine's environment ports with the operating
// system: wall-clock time and random identifiers. Tests inject fakes instead.
package system

import (
	"crypto/rand"
	"fmt"
	"time"

	"agentplane/internal/domain/events"
)

// Clock reads the wall clock.
type Clock struct{}

// Now returns the current time.
func (Clock) Now() time.Time { return time.Now() }

// IDs generates random version-4 UUID identifiers.
type IDs struct{}

// NewRunID returns a new run identifier.
func (IDs) NewRunID() (events.RunID, error) {
	id, err := newUUID()
	return events.RunID(id), err
}

// NewNodeRunID returns a new node-run identifier.
func (IDs) NewNodeRunID() (events.NodeRunID, error) {
	id, err := newUUID()
	return events.NodeRunID(id), err
}

// NewAttemptID returns a new attempt identifier.
func (IDs) NewAttemptID() (events.AttemptID, error) {
	id, err := newUUID()
	return events.AttemptID(id), err
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
