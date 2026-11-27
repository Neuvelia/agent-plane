package shell

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

// DefaultCaptureLimit is the most bytes of stdout, and separately of stderr,
// that Result retains (F0.5 requirement 10). Output beyond it is still drained
// and streamed to the log observer but is not kept; Result reports how much was
// omitted and the retained text ends with an explicit marker. The complete log
// as an artifact is deferred to F3.5.
const DefaultCaptureLimit = 1 << 20

func (e Executor) limit() int {
	if e.captureLimit > 0 {
		return e.captureLimit
	}
	return DefaultCaptureLimit
}

const maxLogChunk = 16 * 1024

// logWriter retains at most limit leading bytes and counts the rest, while
// still passing every byte to the observer in bounded chunks.
type logWriter struct {
	limit   int
	buffer  bytes.Buffer
	omitted uint64
	stream  string
	observe func(string, []byte)
}

func (w *logWriter) Write(data []byte) (int, error) {
	room := w.limit - w.buffer.Len()
	if room > len(data) {
		room = len(data)
	}
	if room > 0 {
		w.buffer.Write(data[:room])
	}
	w.omitted += uint64(len(data) - max(room, 0))
	if w.observe != nil {
		for start := 0; start < len(data); start += maxLogChunk {
			w.observe(w.stream, data[start:min(start+maxLogChunk, len(data))])
		}
	}
	return len(data), nil
}

// retained returns the kept text and the omitted byte count. Omitted output is
// marked in the text itself so a consumer cannot mistake it for the whole log.
func (w *logWriter) retained() (string, uint64) {
	kept := w.buffer.Bytes()
	if w.omitted == 0 {
		return string(kept), 0
	}
	// Dropping a rune split by the limit keeps the text valid UTF-8; count it.
	omitted := w.omitted
	for i := len(kept) - 1; i >= 0 && i >= len(kept)-utf8.UTFMax; i-- {
		if utf8.RuneStart(kept[i]) {
			if !utf8.FullRune(kept[i:]) {
				omitted += uint64(len(kept) - i)
				kept = kept[:i]
			}
			break
		}
	}
	marker := fmt.Sprintf("\n[agentplane: %d bytes of %s omitted; capture limit %d bytes]\n", omitted, w.stream, w.limit)
	return string(kept) + marker, omitted
}
