package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"agentplane/internal/gitstate"
)

const (
	minGitMajor = 2
	minGitMinor = 38
	// Preflight inspects a possibly large repository once per run; the default
	// five seconds of gitstate.Run would be too tight for status and ls-files.
	preflightTimeout = 60 * time.Second
)

// Source is the immutable description of the repository an isolated run starts
// from. BaseSHA is resolved once and used by every worktree of the run.
type Source struct {
	Root    string // canonical absolute path of the source worktree root
	BaseSHA string // full commit id of HEAD at preflight time
}

var gitVersionPattern = regexp.MustCompile(`git version (\d+)\.(\d+)`)
var shaPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// checkGitVersion enforces the `merge-tree --write-tree` requirement.
func checkGitVersion(output string) error {
	m := gitVersionPattern.FindStringSubmatch(output)
	if m == nil {
		return fail(CodeGitVersion, "cannot determine the Git version; Git 2.38 or newer is required", nil)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < minGitMajor || (major == minGitMajor && minor < minGitMinor) {
		return fail(CodeGitVersion, fmt.Sprintf("Git %d.%d is too old; Git 2.38 or newer is required for workspace isolation", major, minor), nil)
	}
	return nil
}

// Preflight validates the source repository and worktree root before a run
// starts and resolves the base commit. It changes nothing on disk.
func Preflight(ctx context.Context, sourceWorkspace, worktreeRoot string) (Source, error) {
	root, err := gitstate.CanonicalWorkspace(ctx, sourceWorkspace)
	if err != nil {
		return Source{}, fail(CodeWorkspace, "source workspace must be the root of a non-bare Git worktree with a committed HEAD", err)
	}
	version, err := gitstate.Run(ctx, root, gitstate.RunOptions{}, "--version")
	if err != nil {
		return Source{}, fail(CodeGitVersion, "cannot run /usr/bin/git; Git 2.38 or newer is required", err)
	}
	if err := checkGitVersion(string(version.Stdout)); err != nil {
		return Source{}, err
	}
	if err := checkWorktreeRoot(root, worktreeRoot); err != nil {
		return Source{}, err
	}
	if err := rejectSubmodules(ctx, root); err != nil {
		return Source{}, err
	}
	if err := rejectDirtyTracked(ctx, root); err != nil {
		return Source{}, err
	}
	if err := rejectFilterDrivers(ctx, root); err != nil {
		return Source{}, err
	}
	head, err := gitstate.Run(ctx, root, gitstate.RunOptions{}, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Source{}, fail(CodeWorkspace, "cannot resolve HEAD to a commit", err)
	}
	sha := strings.TrimSpace(string(head.Stdout))
	if !shaPattern.MatchString(sha) {
		return Source{}, fail(CodeWorkspace, "HEAD did not resolve to a full commit id", nil)
	}
	return Source{Root: root, BaseSHA: sha}, nil
}

// checkWorktreeRoot requires an absolute root that, once symlinks are resolved
// (even for a root that does not exist yet), lies outside the source. A
// worktree nested in the source would be observed by F1.8 snapshots and by the
// source guard as the agent's own changes.
func checkWorktreeRoot(sourceRoot, worktreeRoot string) error {
	if !filepath.IsAbs(worktreeRoot) || strings.ContainsAny(worktreeRoot, "\x00\n\r") {
		return fail(CodeWorktreeRoot, "worktree root must be an absolute path", nil)
	}
	resolved, err := resolveLoose(filepath.Clean(worktreeRoot))
	if err != nil {
		return fail(CodeWorktreeRoot, "cannot resolve the worktree root", err)
	}
	if within(sourceRoot, resolved) {
		return fail(CodeWorktreeRoot, "worktree root must not be inside the source workspace; choose a directory outside the repository", nil)
	}
	return nil
}

// resolveLoose resolves symlinks in the longest existing prefix of path and
// appends the remainder, so a root that has not been created yet is judged by
// where it would really end up.
func resolveLoose(path string) (string, error) {
	rest := ""
	current := path
	for {
		_, err := os.Lstat(current)
		if err == nil {
			real, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			return filepath.Join(real, rest), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing ancestor of %q", path)
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}

func within(parent, child string) bool {
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, strings.TrimSuffix(parent, string(filepath.Separator))+string(filepath.Separator))
}

func rejectSubmodules(ctx context.Context, root string) error {
	res, err := gitstate.Run(ctx, root, gitstate.RunOptions{Timeout: preflightTimeout}, "ls-files", "--stage", "-z")
	if err != nil {
		return fail(CodeWorkspace, "cannot list tracked files in the source workspace", err)
	}
	if hasGitlink(res.Stdout) {
		return fail(CodeSubmodule, "the source repository contains submodules; workspace isolation does not support them", nil)
	}
	return nil
}

// hasGitlink reports a mode-160000 entry in `ls-files --stage -z` output.
func hasGitlink(stage []byte) bool {
	for _, entry := range strings.Split(string(stage), "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			return true
		}
	}
	return false
}

func rejectDirtyTracked(ctx context.Context, root string) error {
	res, err := gitstate.Run(ctx, root, gitstate.RunOptions{Timeout: preflightTimeout}, "status", "--porcelain=v1", "-z", "--untracked-files=no")
	if err != nil {
		return fail(CodeWorkspace, "cannot read the source workspace status", err)
	}
	var paths []string
	for _, entry := range strings.Split(string(res.Stdout), "\x00") {
		if len(entry) > 3 {
			paths = append(paths, entry[3:])
		}
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	shown := paths
	if len(shown) > 5 {
		shown = shown[:5]
	}
	quoted := make([]string, len(shown))
	for i, p := range shown {
		quoted[i] = strconv.Quote(p)
	}
	more := ""
	if len(paths) > len(shown) {
		more = fmt.Sprintf(" and %d more", len(paths)-len(shown))
	}
	return fail(CodeDirtyTracked, fmt.Sprintf("the source workspace has uncommitted changes to tracked files (%s%s); commit or stash them, because isolated worktrees start from HEAD and would not contain them", strings.Join(quoted, ", "), more), nil)
}

var filterKeyPattern = regexp.MustCompile(`^filter\.(.+)\.(smudge|clean|process)$`)

// rejectFilterDrivers refuses repositories whose configuration (including
// included files) defines a filter driver. Checkout runs smudge/process
// filters even with a cleaned environment and disabled hooks, and an unknown
// driver cannot be overridden safely. Only driver names are reported, never
// their commands.
func rejectFilterDrivers(ctx context.Context, root string) error {
	res, err := gitstate.Run(ctx, root, gitstate.RunOptions{}, "config", "--get-regexp", `^filter\..+\.(smudge|clean|process)$`)
	if err != nil {
		var runErr *gitstate.RunError
		if asRunError(err, &runErr) && runErr.Class == gitstate.RunExit && runErr.ExitCode == 1 {
			return nil // exit status 1 means no matching key
		}
		return fail(CodeWorkspace, "cannot read the repository Git configuration", err)
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		key := line
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			key = line[:i]
		}
		if m := filterKeyPattern.FindStringSubmatch(key); m != nil {
			names[m[1]] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, strconv.Quote(n))
	}
	sort.Strings(list)
	return fail(CodeFilterDriver, fmt.Sprintf("the repository configures Git filter driver(s) %s (for example Git LFS); creating a checkout would run them, so workspace isolation refuses this repository. Remove the filter configuration or use isolation: none", strings.Join(list, ", ")), nil)
}
