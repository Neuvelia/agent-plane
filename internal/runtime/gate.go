package runtime

import (
	"sync"

	"agentplane/internal/events"
)

// CheckCapture redacts before bounding retained bytes and drains the entire stream.
// It is a capability; configured secret values never enter executor input.
type CheckCapture struct {
	mu       sync.Mutex
	redactor *logRedactor
	stream   events.Stream
	data     []byte
	omitted  uint64
	closed   bool
}

func (c *CheckCapture) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.add(c.redactor.Write(c.stream, b))
	}
	return len(b), nil
}
func (c *CheckCapture) add(b []byte) {
	n := events.MaxGateStream - len(c.data)
	if n > len(b) {
		n = len(b)
	}
	c.data = append(c.data, b[:n]...)
	c.omitted += uint64(len(b) - n)
}
func (c *CheckCapture) Finish() ([]byte, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.add(c.redactor.Flush(c.stream))
		c.closed = true
	}
	return append([]byte(nil), c.data...), c.omitted
}
func newCheckCapture(r *logRedactor) *CheckCapture {
	return &CheckCapture{redactor: r.clone(), stream: events.Stdout}
}

// NewCheckCapture is the unconfigured capture for direct local Gate execution.
func NewCheckCapture() *CheckCapture { r, _ := newLogRedactor(nil); return newCheckCapture(r) }
