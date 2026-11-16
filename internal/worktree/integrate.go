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
func (*Manager) Integrate(ctx context.Context, src Source, tipRef, unitCommit string) (IntegrateResult, error) {
	if err := validateIntegrateInput(src, tipRef, unitCommit); err != nil {
		return IntegrateResult{}, err
	}
	opts := gitstate.RunOptions{Timeout: mutateTimeout}
	tip, expected, err := readTip(ctx, src, opts, tipRef)
	if err != nil {
		return IntegrateResult{}, err
	}
	tree, conflict, err := mergeUnit(ctx, src, opts, tip, unitCommit)
	if err != nil {
		return IntegrateResult{}, err
	}
	if conflict != nil {
		return *conflict, nil
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

func validateIntegrateInput(src Source, tipRef, unitCommit string) error {
	if err := validateRef(tipRef); err != nil || !strings.HasSuffix(tipRef, "/integrated") {
		return fail(CodeInvalidInput, "tip ref must be a run's integrated ref", nil)
	}
	if !shaPattern.MatchString(unitCommit) || !shaPattern.MatchString(src.BaseSHA) || !strings.HasPrefix(src.Root, "/") {
		return fail(CodeInvalidInput, "source or unit commit is invalid", nil)
	}
	return nil
}

// isGitExit reports whether err is Git running and exiting with code.
func isGitExit(err error, code int) bool {
	var runErr *gitstate.RunError
	return errors.As(err, &runErr) && runErr.Class == gitstate.RunExit && runErr.ExitCode == code
}

// readTip returns the current integration tip and the value the compare-and-swap
// must expect. A missing ref starts at the base commit and expects a zero id.
func readTip(ctx context.Context, src Source, opts gitstate.RunOptions, tipRef string) (tip, expected string, err error) {
	tip, expected = src.BaseSHA, strings.Repeat("0", len(src.BaseSHA)) // zero id: the ref must not exist yet
	cur, runErr := gitstate.Run(ctx, src.Root, opts, "rev-parse", "--verify", "--quiet", tipRef+"^{commit}")
	if runErr == nil {
		tip = strings.TrimSpace(string(cur.Stdout))
		expected = tip
	} else if !isGitExit(runErr, 1) {
		return "", "", fail(CodeIntegrate, "git could not read the integration tip", runErr)
	}
	if !shaPattern.MatchString(tip) {
		return "", "", fail(CodeIntegrate, "git returned an unexpected tip id", nil)
	}
	return tip, expected, nil
}

// mergeUnit three-way merges the unit onto tip. A conflict is a normal result,
// not an error: it is returned with an unchanged tip and bounded paths.
func mergeUnit(ctx context.Context, src Source, opts gitstate.RunOptions, tip, unitCommit string) (tree string, conflict *IntegrateResult, err error) {
	merged, runErr := gitstate.Run(ctx, src.Root, opts, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", "--merge-base="+src.BaseSHA, tip, unitCommit)
	if runErr != nil {
		if !isGitExit(runErr, 1) {
			return "", nil, fail(CodeIntegrate, "git could not merge the unit", runErr)
		}
		return "", conflictResult(merged.Stdout, tip), nil
	}
	tree = strings.TrimSpace(strings.Split(string(merged.Stdout), "\x00")[0])
	if !shaPattern.MatchString(tree) {
		return "", nil, fail(CodeIntegrate, "git returned an unexpected merged tree id", nil)
	}
	return tree, nil, nil
}

func conflictResult(output []byte, tip string) *IntegrateResult {
	paths := conflictPaths(output)
	res := IntegrateResult{Conflict: true, TipCommit: tip}
	if len(paths) > MaxConflictPaths {
		res.Omitted = len(paths) - MaxConflictPaths
		paths = paths[:MaxConflictPaths]
	}
	res.ConflictPaths = paths
	return &res
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
