package cliproc

import "bytes"

// LimitedBuffer is an io.Writer that keeps at most Limit bytes and records
// whether anything was dropped. Write always reports the full length so a
// copying producer is never stopped early; callers must check Overflow.
type LimitedBuffer struct {
	// Limit is the maximum number of bytes retained.
	Limit    int
	buffer   bytes.Buffer
	overflow bool
}

// NewLimitedBuffer returns an empty buffer that retains up to limit bytes.
func NewLimitedBuffer(limit int) *LimitedBuffer { return &LimitedBuffer{Limit: limit} }

// Write appends p up to Limit and sets Overflow when bytes were discarded.
func (b *LimitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	space := b.Limit - b.Len()
	if len(p) > space {
		b.overflow = true
		p = p[:space]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// Overflow reports whether any written byte was discarded.
func (b *LimitedBuffer) Overflow() bool { return b.overflow }

// Len returns the number of retained bytes.
func (b *LimitedBuffer) Len() int { return b.buffer.Len() }

// Bytes returns the retained bytes; the slice aliases the buffer.
func (b *LimitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

// String returns the retained bytes as a string.
func (b *LimitedBuffer) String() string { return b.buffer.String() }
