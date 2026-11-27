package shell

import (
	"fmt"
	"sync"
)

// DefaultCaptureLimit is the number of leading bytes of each stream (stdout and
// stderr separately) a Result retains when Executor.CaptureLimit is zero
// (ADR-0039). Output beyond it is drained and streamed but not retained.
const DefaultCaptureLimit = 1 << 20

// CaptureDiagnostic is the fixed diagnostic code of a CaptureLimitError. It
// reaches node_finished.diagnostic_codes; the error text never does.
const CaptureDiagnostic = "E_SHELL_OUTPUT_TRUNCATED"

// CaptureLimitError reports that a declared output port reads a stream whose
// capture was truncated. Its value would be incomplete, so the node produces no
// outputs instead of passing a prefix downstream as if it were complete.
type CaptureLimitError struct {
	Output  string
	Stream  string
	Limit   int
	Omitted int64
}

func (e *CaptureLimitError) Error() string {
	return fmt.Sprintf("output %q needs the complete %s, but %d bytes beyond the %d-byte capture limit were omitted; write large output to a file instead", e.Output, e.Stream, e.Omitted, e.Limit)
}

// DiagnosticCode returns CaptureDiagnostic for the runtime's event failure codes.
func (*CaptureLimitError) DiagnosticCode() string { return CaptureDiagnostic }

// boundedCapture retains at most limit leading bytes and counts the rest. It
// always reports the whole write as consumed so the child never blocks on a
// full pipe. Writes come from one copy goroutine per stream; the mutex keeps
// the final read safe without relying on that.
type boundedCapture struct {
	mu      sync.Mutex
	limit   int
	data    []byte
	omitted int64
}

func newBoundedCapture(limit int) *boundedCapture {
	return &boundedCapture{limit: limit}
}

func (c *boundedCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := min(max(c.limit-len(c.data), 0), len(data))
	c.data = append(c.data, data[:room]...)
	c.omitted += int64(len(data) - room)
	return len(data), nil
}

// text returns the retained bytes and the omitted count. When bytes were
// omitted the text ends with an explicit marker, so evidence that was cut is
// never mistaken for a complete log.
func (c *boundedCapture) text(stream string) (string, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.omitted == 0 {
		return string(c.data), 0
	}
	return string(c.data) + omissionMarker(stream, c.omitted, c.limit), c.omitted
}

func omissionMarker(stream string, omitted int64, limit int) string {
	return fmt.Sprintf("\n[agentplane: %d bytes of %s omitted; capture limit %d bytes]\n", omitted, stream, limit)
}
