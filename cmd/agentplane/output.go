package main

import (
	"fmt"
	"io"
	"strings"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

func renderDiagnostics(w io.Writer, diagnostics []graph.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		location := diagnostic.Location.Path
		if location == "" {
			location = "graph"
		}
		if _, err := fmt.Fprintf(w, "%s %s at %s: %s\n", diagnostic.Severity, diagnostic.Code, location, diagnostic.Message); err != nil {
			return err
		}
		if diagnostic.Suggestion != "" {
			if _, err := fmt.Fprintf(w, "  hint: %s\n", diagnostic.Suggestion); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderFailure(w io.Writer, failure *runtime.Failure) error {
	if failure == nil {
		return nil
	}
	if failure.Err != nil {
		if _, err := fmt.Fprintf(w, "Failure in %s: %v\n", failure.NodeID, failure.Err); err != nil {
			return err
		}
	}
	return renderDiagnostics(w, failure.Diagnostics)
}

type logKey struct {
	node   string
	stream events.Stream
}
type eventRenderer struct {
	stdout  io.Writer
	stderr  io.Writer
	live    bool
	pending map[logKey]string
}

func newEventRenderer(stdout, stderr io.Writer, live bool) *eventRenderer {
	return &eventRenderer{stdout: stdout, stderr: stderr, live: live, pending: make(map[logKey]string)}
}

// Live delivery is a prefix of the retained stream. Replay restores the
// missing tail after Run returns without rendering delivered events twice.
func renderWatchEvents(renderer *eventRenderer, live <-chan events.Event, replay <-chan []events.Event, cancelRun func()) error {
	var lastSequence uint64
	var renderErr error
	for event := range live {
		if renderErr == nil {
			renderErr = renderer.Event(event)
			if renderErr != nil && cancelRun != nil {
				cancelRun()
			}
		}
		lastSequence = event.Sequence
	}
	for _, event := range <-replay {
		if event.Sequence > lastSequence && renderErr == nil {
			renderErr = renderer.Event(event)
		}
	}
	return renderErr
}

func (r *eventRenderer) Event(event events.Event) error {
	switch event.Type {
	case events.RunStarted:
		_, err := fmt.Fprintf(r.stderr, "Run started: %s\n", event.RunID)
		return err
	case events.AttemptStarted:
		p := event.Payload.(events.AttemptStartedPayload)
		_, err := fmt.Fprintf(r.stderr, "%s attempt %d/%d started: %s\n", event.NodeID, p.Number, p.MaxAttempts, event.AttemptID)
		return err
	case events.AttemptFinished:
		if err := r.flushNode(event.NodeID); err != nil {
			return err
		}
		p := event.Payload.(events.AttemptFinishedPayload)
		_, err := fmt.Fprintf(r.stderr, "%s attempt %s: %s\n", event.NodeID, event.AttemptID, p.Outcome)
		return err
	case events.NodeReady:
		_, err := fmt.Fprintf(r.stderr, "%s ready\n", event.NodeID)
		return err
	case events.NodeStarted:
		_, err := fmt.Fprintf(r.stderr, "%s running\n", event.NodeID)
		return err
	case events.LogChunk:
		payload := event.Payload.(events.LogChunkPayload)
		key := logKey{node: event.NodeID, stream: payload.Stream}
		r.pending[key] += string(payload.Data)
		for {
			line, rest, ok := strings.Cut(r.pending[key], "\n")
			if !ok {
				break
			}
			if err := r.logLine(key, line+"\n"); err != nil {
				return err
			}
			if rest == "" {
				delete(r.pending, key)
			} else {
				r.pending[key] = rest
			}
		}
		if r.live && r.pending[key] != "" {
			if err := r.logLine(key, r.pending[key]+"\n"); err != nil {
				return err
			}
			delete(r.pending, key)
		}
	case events.TextDelta:
		p := event.Payload.(events.TextDeltaPayload)
		_, err := fmt.Fprintf(r.stdout, "[%s text] %s\n", event.NodeID, p.Text)
		return err
	case events.ToolCallStarted, events.ToolCallFinished:
		p := event.Payload.(events.ToolCallPayload)
		_, err := fmt.Fprintf(r.stderr, "%s %s: %s %s\n", event.NodeID, event.Type, p.Name, p.Status)
		return err
	case events.CommandStarted, events.CommandFinished:
		p := event.Payload.(events.CommandPayload)
		_, err := fmt.Fprintf(r.stderr, "%s %s: %s %s\n", event.NodeID, event.Type, p.CommandID, p.Status)
		return err
	case events.FileChanged:
		p := event.Payload.(events.FileChangedPayload)
		_, err := fmt.Fprintf(r.stderr, "%s file_changed: %q %s (%s)\n", event.NodeID, p.Path, p.Action, p.Provenance)
		return err
	case events.Status:
		p := event.Payload.(events.StatusPayload)
		_, err := fmt.Fprintf(r.stderr, "%s status: %s\n", event.NodeID, p.Code)
		return err
	case events.ProgressOmitted:
		p := event.Payload.(events.ProgressOmittedPayload)
		_, err := fmt.Fprintf(r.stderr, "Progress omitted: retained %d events / %d bytes; live %d events / %d bytes (stopped=%t)\n", p.RetainedEvents, p.RetainedBytes, p.LiveEvents, p.LiveBytes, p.LiveStopped)
		return err
	case events.GateCheck:
		p := event.Payload.(events.GateCheckPayload)
		if _, err := fmt.Fprintf(r.stderr, "%s gate %s: %s (%s)\n", event.NodeID, p.CheckID, p.Verdict, p.Reason); err != nil {
			return err
		}
		for _, e := range p.Evidence {
			exit := "absent"
			if e.ExitCode != nil {
				exit = fmt.Sprint(*e.ExitCode)
			}
			if _, err := fmt.Fprintf(r.stderr, "  source=%s path=%q exit=%s stdout=%q stderr=%q omitted=%d/%d/%d\n", e.Source, e.Path, exit, e.Stdout, e.Stderr, e.StdoutOmitted, e.StderrOmitted, e.Omitted); err != nil {
				return err
			}
		}
	case events.NodeEvidence:
		p := event.Payload.(events.NodeEvidencePayload)
		label := event.NodeID
		if event.AttemptID != "" {
			label += " attempt " + string(event.AttemptID) + " producer"
		}
		if _, err := fmt.Fprintf(r.stderr, "%s evidence: %s %s (%d changed paths)\n", label, p.AdapterID, p.AdapterVersion, len(p.ChangedFiles)); err != nil {
			return err
		}
		for _, file := range p.ChangedFiles {
			if _, err := fmt.Fprintf(r.stderr, "  %q (%s)\n", file.Path, file.Provenance); err != nil {
				return err
			}
		}
	case events.NodeFinished:
		if err := r.flushNode(event.NodeID); err != nil {
			return err
		}
		payload := event.Payload.(events.NodeFinishedPayload)
		if payload.DurationMS != nil {
			_, err := fmt.Fprintf(r.stderr, "%s %s %dms\n", event.NodeID, payload.Status, *payload.DurationMS)
			return err
		} else {
			_, err := fmt.Fprintf(r.stderr, "%s %s\n", event.NodeID, payload.Status)
			return err
		}
	case events.NodeTerminated:
		if err := r.flushNode(event.NodeID); err != nil {
			return err
		}
		payload := event.Payload.(events.NodeTerminatedPayload)
		_, err := fmt.Fprintf(r.stderr, "%s %s\n", event.NodeID, payload.Status)
		return err
	case events.RunFinished:
		if err := r.flushAll(); err != nil {
			return err
		}
		payload := event.Payload.(events.RunFinishedPayload)
		_, err := fmt.Fprintf(r.stderr, "Run %s\n", payload.Status)
		return err
	}
	return nil
}

func (r *eventRenderer) logLine(key logKey, content string) error {
	w := r.stderr
	if key.stream == events.Stdout {
		w = r.stdout
	}
	_, err := fmt.Fprintf(w, "[%s %s] %s", key.node, key.stream, content)
	return err
}

func (r *eventRenderer) flushNode(node string) error {
	for _, stream := range []events.Stream{events.Stdout, events.Stderr} {
		key := logKey{node: node, stream: stream}
		if r.pending[key] != "" {
			if err := r.logLine(key, r.pending[key]+"\n"); err != nil {
				return err
			}
			delete(r.pending, key)
		}
	}
	return nil
}

func (r *eventRenderer) flushAll() error {
	for key, content := range r.pending {
		if err := r.logLine(key, content+"\n"); err != nil {
			return err
		}
	}
	clear(r.pending)
	return nil
}
