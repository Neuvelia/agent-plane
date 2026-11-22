package attemptdiff

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/gitstate"
)

// Runner starts Git; tests substitute a recording one. Production uses
// gitstate.Run, the only way this repository starts Git.
type Runner func(ctx context.Context, dir string, opts gitstate.RunOptions, args ...string) (gitstate.RunResult, error)

const (
	gitTimeout    = 30 * time.Second
	listLimit     = 64 << 20
	blobReadLimit = 2 << 20
)

// run refuses every subcommand outside the allowlist, so a later edit cannot
// make capture run `diff`, `status` or anything that writes.
func (r *Recorder) run(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	if !allowed(args) {
		return nil, fail(events.DiffGitFailed, "git subcommand is not allowed", nil)
	}
	res, err := r.runner(ctx, root, gitstate.RunOptions{Timeout: gitTimeout, StdoutLimit: limit}, args...)
	if err != nil {
		return nil, classifyGit(err)
	}
	return res.Stdout, nil
}

func allowed(args []string) bool {
	switch {
	case len(args) == 0:
		return false
	case args[0] == "ls-files":
		return true
	case args[0] == "cat-file":
		return len(args) == 3 && args[1] == "blob"
	case args[0] == "rev-parse":
		return true
	}
	return false
}

type indexEntry struct {
	mode, blob string
}

// listIndex returns tracked paths with their mode and blob id. An unresolved
// merge stage fails the capture: the content of the path is ambiguous.
func (r *Recorder) listIndex(ctx context.Context, root string) (map[string]indexEntry, error) {
	out, err := r.run(ctx, root, listLimit, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	entries := map[string]indexEntry{}
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return nil, fail(events.DiffGitFailed, fmt.Sprintf("index entry for %d bytes of path is not stage 0", len(path)), nil)
		}
		entries[path] = indexEntry{mode: fields[0], blob: fields[1]}
	}
	return entries, nil
}

func (r *Recorder) listPaths(ctx context.Context, root string, args ...string) ([]string, error) {
	out, err := r.run(ctx, root, listLimit, append([]string{"ls-files", "-z", "--others", "--exclude-standard"}, args...)...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// readBlob returns an object's bytes exactly as stored; cat-file blob applies
// no filter, textconv or attribute.
func (r *Recorder) readBlob(ctx context.Context, root, id string) ([]byte, error) {
	return r.run(ctx, root, blobReadLimit, "cat-file", "blob", id)
}
