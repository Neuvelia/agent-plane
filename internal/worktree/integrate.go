package worktree

import (
	"context"
	"errors"
	"sort"
	"strings"

	"agentplane/internal/gitstate"
)

// MaxConflictPaths bounds the conflicting paths reported for one integration.
const MaxConflictPaths = 64

// IntegrateResult is the outcome of merging one unit onto the run's tip.
type IntegrateResult struct {
	Conflict      bool
	ConflictPaths []string // sorted, at most MaxConflictPaths
	Omitted       int      // conflicting paths beyond MaxConflictPaths
	TipCommit     string   // new tip on success; unchanged tip on conflict
	ChangedPaths  int      // paths this unit changed relative to the previous tip
}

// Integrate three-way merges unitCommit onto the commit at tipRef with
// src.BaseSHA as merge base, using `git merge-tree --write-tree`. That needs no
// working tree or index, so the user's checkout is never involved. A missing
// tipRef starts at the base commit. On success tipRef advances to a merge commit
// (parents: previous tip, unit); on conflict nothing is written. There is no
// automatic resolution.
func (m *Manager) Integrate(ctx context.Context, src Source, tipRef, unitCommit string) (IntegrateResult, error) {
	if err := validateRef(tipRef); err != nil || !strings.HasSuffix(tipRef, "/integrated") {
		return IntegrateResult{}, fail(CodeInvalidInput, "tip ref must be a run's integrated ref", nil)
	}
	if !shaPattern.MatchString(unitCommit) || !shaPattern.MatchString(src.BaseSHA) || !strings.HasPrefix(src.Root, "/") {
		return IntegrateResult{}, fail(CodeInvalidInput, "source or unit commit is invalid", nil)
	}
	opts := gitstate.RunOptions{Timeout: mutateTimeout}

	tip, expected := src.BaseSHA, strings.Repeat("0", len(src.BaseSHA)) // zero id: the ref must not exist yet
	cur, err := gitstate.Run(ctx, src.Root, opts, "rev-parse", "--verify", "--quiet", tipRef+"^{commit}")
	switch {
	case err == nil:
		tip = strings.TrimSpace(string(cur.Stdout))
		expected = tip
	default:
		var runErr *gitstate.RunError
		if !errors.As(err, &runErr) || runErr.Class != gitstate.RunExit || runErr.ExitCode != 1 {
			return IntegrateResult{}, fail(CodeIntegrate, "git could not read the integration tip", err)
		}
	}
	if !shaPattern.MatchString(tip) {
		return IntegrateResult{}, fail(CodeIntegrate, "git returned an unexpected tip id", nil)
	}

	merged, err := gitstate.Run(ctx, src.Root, opts, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", "--merge-base="+src.BaseSHA, tip, unitCommit)
	if err != nil {
		var runErr *gitstate.RunError
		if errors.As(err, &runErr) && runErr.Class == gitstate.RunExit && runErr.ExitCode == 1 {
			paths := conflictPaths(merged.Stdout)
			res := IntegrateResult{Conflict: true, TipCommit: tip}
			if len(paths) > MaxConflictPaths {
				res.Omitted = len(paths) - MaxConflictPaths
				paths = paths[:MaxConflictPaths]
			}
			res.ConflictPaths = paths
			return res, nil
		}
		return IntegrateResult{}, fail(CodeIntegrate, "git could not merge the unit", err)
	}
	fields := strings.Split(string(merged.Stdout), "\x00")
	tree := strings.TrimSpace(fields[0])
	if !shaPattern.MatchString(tree) {
		return IntegrateResult{}, fail(CodeIntegrate, "git returned an unexpected merged tree id", nil)
	}
	commit, err := commitTree(ctx, src.Root, tree, "agentplane integrate unit", tip, unitCommit)
	if err != nil {
		return IntegrateResult{}, err
	}
	// Compare-and-swap: a concurrent writer of the same ref fails the update
	// instead of being overwritten.
	if _, err := gitstate.Run(ctx, src.Root, opts, "update-ref", tipRef, commit, expected); err != nil {
		return IntegrateResult{}, fail(CodeIntegrate, "git could not advance the integration ref", err)
	}
	changed, err := countChanged(ctx, src.Root, tip, commit)
	if err != nil {
		return IntegrateResult{}, err
	}
	return IntegrateResult{TipCommit: commit, ChangedPaths: changed}, nil
}

// conflictPaths extracts the sorted unique path names from `merge-tree
// --name-only -z` output, which is the merged tree id followed by names.
func conflictPaths(output []byte) []string {
	fields := strings.Split(string(output), "\x00")
	seen := map[string]bool{}
	var paths []string
	for _, f := range fields[1:] {
		if f != "" && !seen[f] {
			seen[f] = true
			paths = append(paths, f)
		}
	}
	sort.Strings(paths)
	return paths
}
