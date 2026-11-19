package attemptdiff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	"agentplane/internal/domain/diff"
	"agentplane/internal/domain/events"
)

// Redactor masks configured secrets in a byte string. It must return valid
// UTF-8 for valid UTF-8 input and report whether it changed anything.
type Redactor interface {
	Redact(text []byte) (redacted []byte, changed bool)
}

// Attempt is the capture of one correction attempt: file events in path order
// and the summary the runtime emits last.
type Attempt struct {
	Files   []events.AttemptDiffPayload
	Summary events.AttemptDiffPayload
}

// side is one path at one snapshot: a real entry, an ignored signature or nothing.
type side struct {
	entry   *entry
	ignored string
	root    string
}

func (s side) present() bool { return s.entry != nil || s.ignored != "" }

func (s side) identity() string {
	switch {
	case s.entry != nil:
		e := s.entry
		return fmt.Sprintf("e|%d|%t|%s|%s", e.kind, e.exec, e.sha, e.id)
	case s.ignored != "":
		return "i|" + s.ignored
	}
	return ""
}

func sideOf(snap Snapshot, path string) side {
	if e, ok := snap.entries[path]; ok {
		return side{entry: &e, root: snap.root}
	}
	return side{ignored: snap.ignored[path], root: snap.root}
}

func changedPaths(before, after Snapshot) []string {
	union := map[string]struct{}{}
	for _, snap := range []Snapshot{before, after} {
		for p := range snap.entries {
			union[p] = struct{}{}
		}
		for p := range snap.ignored {
			union[p] = struct{}{}
		}
	}
	var changed []string
	for _, p := range slices.Sorted(maps.Keys(union)) {
		if sideOf(before, p).identity() != sideOf(after, p).identity() {
			changed = append(changed, p)
		}
	}
	return changed
}

// Diff compares two snapshots of the same workspace. Configured secrets are
// masked in both sides' text and in the path before anything is diffed, so a
// secret spanning lines or the patch cut cannot survive (Q4). Hashes and sizes
// describe the original bytes. At most events.MaxAttemptDiffFiles files are
// listed; the rest are counted in Summary.OmittedFiles (Q3).
func (r *Recorder) Diff(ctx context.Context, before, after Snapshot, red Redactor) (Attempt, error) {
	changed := changedPaths(before, after)
	var out Attempt
	diag := firstCode(before.diag, after.diag)
	if len(changed) > events.MaxAttemptDiffFiles {
		out.Summary.OmittedFiles = len(changed) - events.MaxAttemptDiffFiles
		changed = changed[:events.MaxAttemptDiffFiles]
	}
	budget := events.MaxAttemptDiffAttemptBytes
	for _, path := range changed {
		if err := ctx.Err(); err != nil {
			return Attempt{}, classifyGit(err)
		}
		payload, ok := r.filePayload(ctx, sideOf(before, path), sideOf(after, path), path, red, &budget)
		if !ok {
			out.Summary.OmittedFiles++
			diag = firstCode(diag, events.DiffUnsupportedEntry)
			continue
		}
		out.Files = append(out.Files, payload)
		out.Summary.AddedLines += payload.AddedLines
		out.Summary.DeletedLines += payload.DeletedLines
		out.Summary.PatchBytes += len(payload.Patch)
	}
	out.Summary.Files = len(out.Files)
	out.Summary.DiagnosticCode = diag
	if diag != "" {
		out.Summary.OmittedReason = events.DiffOmitUnreadable
	}
	return out, nil
}

func firstCode(current, next string) string {
	if current != "" {
		return current
	}
	return next
}

func (r *Recorder) filePayload(ctx context.Context, b, a side, path string, red Redactor, budget *int) (events.AttemptDiffPayload, bool) {
	p := events.AttemptDiffPayload{Path: path, Change: classify(b, a)}
	p.BeforeSHA256, p.BeforeSize = sideFacts(b)
	p.AfterSHA256, p.AfterSize = sideFacts(a)
	shown, masked := redactBytes(red, path)
	p.Path, p.Redacted = shown, masked
	if !events.ValidAttemptDiffPath(shown) {
		return p, false
	}
	switch reason := omission(b, a); {
	case reason != "":
		p.OmittedReason = reason
		p.Binary = reason == events.DiffOmitBinary
	case p.Change != events.DiffMode:
		r.renderPatch(ctx, &p, b, a, red, budget)
	}
	return p, true
}

func redactBytes(red Redactor, text string) (string, bool) {
	if red == nil {
		return text, false
	}
	out, changed := red.Redact([]byte(text))
	return string(out), changed
}

func classify(b, a side) string {
	switch {
	case !b.present():
		return events.DiffAdded
	case !a.present():
		return events.DiffDeleted
	case b.entry != nil && a.entry != nil && b.entry.sha != "" && b.entry.sha == a.entry.sha:
		return events.DiffMode
	}
	return events.DiffModified
}

func sideFacts(s side) (string, int) {
	if s.entry == nil {
		return "", 0
	}
	return s.entry.sha, int(min(s.entry.size, 1<<31-1))
}

// omission names why a side pair cannot be shown as text, or "".
func omission(b, a side) string {
	if b.ignored != "" || a.ignored != "" {
		return events.DiffOmitIgnored
	}
	for _, s := range []side{a, b} {
		if s.entry != nil && s.entry.omit != "" {
			return s.entry.omit
		}
	}
	return ""
}

// readSide returns the original bytes of a text side; an absent side is empty.
func (r *Recorder) readSide(ctx context.Context, s side) ([]byte, error) {
	e := s.entry
	switch {
	case e == nil || e.size == 0:
		return nil, nil
	case e.kind == kindSymlink:
		return e.inline, nil
	case e.stored:
		return r.scratch.get(e.sha)
	case e.blob != "":
		data, err := r.readBlob(ctx, s.root, e.blob)
		if err == nil && sha256Hex(data) != e.sha {
			return nil, errors.New("blob does not match the observed content")
		}
		return data, err
	}
	return nil, errors.New("no recoverable content")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *Recorder) renderPatch(ctx context.Context, p *events.AttemptDiffPayload, b, a side, red Redactor, budget *int) {
	before, berr := r.readSide(ctx, b)
	after, aerr := r.readSide(ctx, a)
	if berr != nil || aerr != nil {
		p.OmittedReason = events.DiffOmitUnreadable
		return
	}
	before, bmasked := redactBytes2(red, before)
	after, amasked := redactBytes2(red, after)
	p.Redacted = p.Redacted || bmasked || amasked
	if !utf8.Valid(before) || !utf8.Valid(after) {
		p.OmittedReason = events.DiffOmitUnreadable
		return
	}
	res, err := diff.Lines(ctx, before, after, diff.Limits{MaxPatchBytes: events.MaxAttemptDiffPatchBytes})
	switch {
	case errors.Is(err, diff.ErrTooLarge):
		p.OmittedReason = events.DiffOmitTooLarge
	case err != nil:
		p.OmittedReason = events.DiffOmitUnreadable
	case len(res.Patch) > *budget:
		p.AddedLines, p.DeletedLines = res.Added, res.Deleted
		p.OmittedReason = events.DiffOmitBudget
	default:
		*budget -= len(res.Patch)
		p.Patch, p.AddedLines, p.DeletedLines, p.Truncated = res.Patch, res.Added, res.Deleted, res.Truncated
	}
}

func redactBytes2(red Redactor, data []byte) ([]byte, bool) {
	if red == nil {
		return data, false
	}
	return red.Redact(data)
}
