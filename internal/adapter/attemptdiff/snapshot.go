package attemptdiff

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // Git object identity, not a security use
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"maps"
	"os"
	"slices"
	"time"
	"unicode/utf8"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

const (
	snapshotTimeout   = 60 * time.Second
	defaultIgnoredCap = 20000
	textProbeBytes    = 8192
	maxSymlinkBytes   = 4096
)

type kind uint8

const (
	kindFile kind = iota + 1
	kindSymlink
	kindGitlink
	kindOther
)

// entry is one working-tree path at snapshot time.
type entry struct {
	kind   kind
	exec   bool
	size   int64
	sha    string // SHA-256 of the content (symlink: of the target text)
	id     string // identity for entries without sha: gitlink commit, special-file signature
	omit   string // why no patch can exist: binary, too_large, budget or unreadable
	blob   string // index blob id when the working bytes equal it (Git can return them)
	inline []byte // symlink target text
	stored bool   // a copy lives in scratch
}

// Snapshot is the content-aware state of one workspace at one moment.
type Snapshot struct {
	root    string
	entries map[string]entry
	ignored map[string]string // path -> size:mtime:mode signature, never read
	diag    string            // first non-fatal diagnostic code
}

// Open creates the scratch directory for one run. Close removes it.
func Open() (*Recorder, error) { return open(gitstate.Run) }

func open(run Runner) (*Recorder, error) {
	s, err := openScratch()
	if err != nil {
		return nil, fail(events.DiffScratch, "scratch directory could not be created", err)
	}
	return &Recorder{scratch: s, runner: run, ignoredCap: defaultIgnoredCap}, nil
}

// Recorder owns the scratch directory of one run and is safe for concurrent use.
type Recorder struct {
	scratch    *scratch
	runner     Runner
	ignoredCap int
}

// ScratchDirForTest names the scratch directory so tests can prove it is
// removed; production code has no use for it.
func (r *Recorder) ScratchDirForTest() string { return r.scratch.dir }

// Close removes every scratch copy; it is idempotent.
func (r *Recorder) Close() error { return r.scratch.remove() }

// Snapshot observes workspace, which must be the exact root of a Git worktree.
func (r *Recorder) Snapshot(ctx context.Context, workspace string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	root, err := gitstate.CanonicalWorkspace(ctx, workspace)
	if err != nil {
		return Snapshot{}, unavailable(err)
	}
	index, err := r.listIndex(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	untracked, err := r.listPaths(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{root: root, entries: map[string]entry{}}
	for _, path := range slices.Sorted(maps.Keys(index)) {
		if err := r.observe(ctx, &snap, path, index[path]); err != nil {
			return Snapshot{}, err
		}
	}
	for _, path := range untracked {
		if err := r.observe(ctx, &snap, path, indexEntry{}); err != nil {
			return Snapshot{}, err
		}
	}
	if err := r.observeIgnored(ctx, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func (s *Snapshot) note(code string) {
	if s.diag == "" {
		s.diag = code
	}
}

func (r *Recorder) observe(ctx context.Context, snap *Snapshot, path string, tracked indexEntry) error {
	if err := ctx.Err(); err != nil {
		return classifyGit(err)
	}
	full, err := gitstate.ConfinedPath(snap.root, path)
	if err != nil || !representable(path) {
		snap.note(events.DiffUnsupportedEntry)
		return nil
	}
	if tracked.mode == "160000" {
		snap.entries[path] = entry{kind: kindGitlink, id: tracked.blob, omit: events.DiffOmitUnreadable}
		return nil
	}
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		snap.entries[path] = entry{kind: kindOther, id: "lstat-error", omit: events.DiffOmitUnreadable}
		return nil
	}
	e, derr := r.describe(full, info, tracked)
	if derr != nil {
		snap.note(DiagnosticCode(derr))
	}
	snap.entries[path] = e
	return nil
}

func representable(path string) bool { return events.ValidAttemptDiffPath(path) }

func signature(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d:%o", info.Size(), info.ModTime().UnixNano(), info.Mode())
}

func (r *Recorder) describe(full string, info os.FileInfo, tracked indexEntry) (entry, *Error) {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return describeSymlink(full), nil
	case info.Mode().IsRegular():
		return r.describeFile(full, info, tracked)
	}
	return entry{kind: kindOther, id: signature(info), omit: events.DiffOmitUnreadable}, nil
}

func describeSymlink(full string) entry {
	target, err := os.Readlink(full)
	if err != nil || len(target) > maxSymlinkBytes {
		return entry{kind: kindSymlink, id: "unreadable", omit: events.DiffOmitUnreadable}
	}
	sum := sha256.Sum256([]byte(target))
	return entry{kind: kindSymlink, size: int64(len(target)), sha: hex.EncodeToString(sum[:]), inline: []byte(target)}
}

// gitHasher mirrors Git's blob object id for the repository's object format,
// taken from the length of the index blob id.
func gitHasher(blob string, size int64) hash.Hash {
	var h hash.Hash
	switch len(blob) {
	case 40:
		h = sha1.New() //nolint:gosec // Git object identity, not a security use
	case 64:
		h = sha256.New()
	default:
		return nil
	}
	fmt.Fprintf(h, "blob %d\x00", size)
	return h
}

type fileRead struct {
	sha, blob string
	head      []byte
	size      int64
	nul       bool
}

func (r *Recorder) describeFile(full string, info os.FileInfo, tracked indexEntry) (entry, *Error) {
	read, err := readFile(full, info, tracked.blob)
	if err != nil {
		return entry{kind: kindFile, id: signature(info), omit: events.DiffOmitUnreadable}, nil
	}
	e := entry{kind: kindFile, exec: info.Mode().Perm()&0o111 != 0, size: read.size, sha: read.sha}
	switch {
	case read.nul || (read.size <= events.MaxAttemptDiffFile && !utf8.Valid(read.head)):
		e.omit = events.DiffOmitBinary
	case read.size > events.MaxAttemptDiffFile:
		e.omit = events.DiffOmitTooLarge
	case tracked.blob != "" && read.blob == tracked.blob:
		e.blob = tracked.blob
	default:
		return r.store(e, read.head)
	}
	return e, nil
}

func (r *Recorder) store(e entry, data []byte) (entry, *Error) {
	err := r.scratch.put(e.sha, data)
	switch {
	case err == nil:
		e.stored = true
		return e, nil
	case errors.Is(err, errScratchBudget):
		e.omit = events.DiffOmitBudget
		return e, nil
	}
	e.omit = events.DiffOmitUnreadable
	return e, fail(events.DiffScratch, "scratch copy could not be written", err)
}

// readFile streams the file once. The opened file must be the one that was
// lstat-ed, so a path swapped for a symlink in between is rejected.
func readFile(full string, lstat os.FileInfo, trackedBlob string) (fileRead, error) {
	f, err := os.Open(full)
	if err != nil {
		return fileRead{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(lstat, opened) || !opened.Mode().IsRegular() {
		return fileRead{}, errors.New("path changed while reading")
	}
	sum := sha256.New()
	blob := gitHasher(trackedBlob, opened.Size())
	writers := []io.Writer{sum}
	if blob != nil {
		writers = append(writers, blob)
	}
	var head bytes.Buffer
	writers = append(writers, &limitedHead{buf: &head, limit: events.MaxAttemptDiffFile + 1})
	n, err := io.Copy(io.MultiWriter(writers...), f)
	if err != nil || n != opened.Size() {
		return fileRead{}, errors.New("file changed while reading")
	}
	read := fileRead{sha: hex.EncodeToString(sum.Sum(nil)), head: head.Bytes(), size: n}
	if blob != nil {
		read.blob = hex.EncodeToString(blob.Sum(nil))
	}
	probe := read.head
	if len(probe) > textProbeBytes {
		probe = probe[:textProbeBytes]
	}
	read.nul = bytes.IndexByte(probe, 0) >= 0
	return read, nil
}

// limitedHead keeps the first limit bytes and discards the rest.
type limitedHead struct {
	buf   *bytes.Buffer
	limit int
}

func (l *limitedHead) Write(p []byte) (int, error) {
	if room := l.limit - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// observeIgnored records Lstat signatures of ignored files and never opens
// them. Over ignoredCap entries detection is skipped (Q5).
func (r *Recorder) observeIgnored(ctx context.Context, snap *Snapshot) error {
	paths, err := r.listPaths(ctx, snap.root, "--ignored")
	var typed *Error
	if (errors.As(err, &typed) && typed.limit) || len(paths) > r.ignoredCap {
		snap.note(events.DiffIgnoredLimit)
		return nil
	}
	if err != nil {
		return err
	}
	snap.ignored = make(map[string]string, len(paths))
	for _, path := range paths {
		full, cerr := gitstate.ConfinedPath(snap.root, path)
		info, lerr := os.Lstat(full)
		if cerr != nil || lerr != nil || !representable(path) {
			continue
		}
		snap.ignored[path] = signature(info)
	}
	return nil
}
