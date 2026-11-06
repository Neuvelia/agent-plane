package runtime

import (
	"agentplane/internal/events"
	"encoding/json"
	"sort"
	"unicode/utf8"
)

// progressRedactor admits a fixed number of logical public-text streams. It
// never evicts unresolved tails (which could disclose a split secret).
type progressRedactor struct {
	template *logRedactor
	streams  map[string]*textStream
	emit     func(events.Type, events.Payload, bool)
	omit     func()
	limited  bool
}

type textStream struct {
	redactor *logRedactor
	pending  []byte
}

const maxTextSources = 16

func newProgressRedactor(template *logRedactor, emit func(events.Type, events.Payload, bool), omit func()) *progressRedactor {
	return &progressRedactor{template: template, streams: make(map[string]*textStream), emit: emit, omit: omit}
}
func (p *progressRedactor) redact(s string) string {
	r := p.template.clone()
	b := append([]byte{}, r.Write(events.Stdout, []byte(s))...)
	return string(append(b, r.Flush(events.Stdout)...))
}
func (p *progressRedactor) text(id string, b []byte, final bool) {
	stream := p.streams[id]
	stream.pending = append(stream.pending, b...)
	safeID := p.redact(id)
	room := events.MaxProgressContent - len(safeID)
	if room < utf8.UTFMax {
		stream.pending = nil
		return
	}
	for len(stream.pending) > 0 {
		n := 0
		for n < len(stream.pending) {
			if !utf8.FullRune(stream.pending[n:]) && !final {
				break
			}
			_, size := utf8.DecodeRune(stream.pending[n:])
			if n+size > room {
				break
			}
			n += size
		}
		if n == 0 {
			break
		}
		p.emit(events.TextDelta, events.TextDeltaPayload{SourceID: safeID, Text: string(stream.pending[:n])}, safeID != id)
		stream.pending = append([]byte(nil), stream.pending[n:]...)
	}
}
func (p *progressRedactor) write(kind events.Type, payload events.Payload) {
	if !events.IsNormalized(kind) {
		return
	}
	if v, ok := payload.(events.TextDeltaPayload); ok && kind == events.TextDelta {
		if len(v.SourceID) > events.MaxProgressContent || !utf8.ValidString(v.SourceID) || !utf8.ValidString(v.Text) {
			return
		}
		if events.MaxProgressContent-len(p.redact(v.SourceID)) < utf8.UTFMax {
			p.omit()
			delete(p.streams, v.SourceID)
			return
		}
		r := p.streams[v.SourceID]
		if r == nil {
			if len(p.streams) >= maxTextSources {
				if !p.limited {
					p.limited = true
					p.emit(events.Status, events.StatusPayload{Code: "progress_limited"}, false)
				}
				return
			}
			r = &textStream{redactor: p.template.clone()}
			r.redactor.eager = true
			p.streams[v.SourceID] = r
		}
		// Bound each redactor write as well as retained event content.
		data := []byte(v.Text)
		for len(data) > 0 {
			n := len(data)
			if n > events.MaxProgressContent {
				n = events.MaxProgressContent
			}
			p.text(v.SourceID, r.redactor.Write(events.Stdout, data[:n]), false)
			data = data[n:]
		}
		if len(r.pending) == 0 && len(r.redactor.pending[events.Stdout]) == 0 {
			delete(p.streams, v.SourceID)
		}
		return
	}
	originalValid := (events.Event{Type: kind, Payload: payload}).Validate() == nil
	if !originalValid {
		return
	}
	identityRedacted := false
	switch v := payload.(type) {
	case events.ToolCallPayload:
		original := v
		v.CallID = p.redact(v.CallID)
		v.Name = p.redact(v.Name)
		v.Status = p.redact(v.Status)
		identityRedacted = v != original
		payload = v
	case events.CommandPayload:
		original := v
		v.CommandID = p.redact(v.CommandID)
		v.Status = p.redact(v.Status)
		identityRedacted = v.CommandID != original.CommandID || v.Status != original.Status
		payload = v
	case events.FileChangedPayload:
		original := v
		v.Path = p.redact(v.Path)
		v.Action = p.redact(v.Action)
		v.Provenance = p.redact(v.Provenance)
		identityRedacted = v != original
		payload = v
	case events.StatusPayload:
		original := v
		v.Code = p.redact(v.Code)
		identityRedacted = v != original
		payload = v
	default:
		return
	}
	b, _ := json.Marshal(payload)
	if len(b) > events.MaxProgressContent {
		p.omit()
		return
	}
	if (events.Event{Type: kind, Payload: payload}).Validate() != nil {
		p.omit()
		return
	}
	p.emit(kind, payload, identityRedacted)
}
func (p *progressRedactor) flush() {
	ids := make([]string, 0, len(p.streams))
	for id := range p.streams {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p.text(id, p.streams[id].redactor.Flush(events.Stdout), true)
	}
}
