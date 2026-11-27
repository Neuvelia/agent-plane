package main

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var errInvalidSHA = errors.New("not a full commit SHA")

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// validateSHA accepts only a full lowercase hex commit name, so a value read
// from an API response or a comment can never reach Git as an option or a
// revision expression.
func validateSHA(value string) error {
	if !fullSHA.MatchString(value) {
		return fmt.Errorf("%w: %q", errInvalidSHA, truncate(value, 60))
	}
	return nil
}

// gitRepository runs Git in one checkout with structured argv.
type gitRepository struct {
	dir string
}

func (g gitRepository) output(args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", g.dir}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}

// isAncestor reports whether ancestor is reachable from descendant. Exit code
// 1 is Git's "no"; any other failure is an error.
func (g gitRepository) isAncestor(ancestor, descendant string) (bool, error) {
	err := exec.Command("git", "-C", g.dir, "merge-base", "--is-ancestor", ancestor, descendant).Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
	}
	return true, nil
}

// hasCommit reports whether the checkout holds the commit.
func (g gitRepository) hasCommit(sha string) bool {
	return exec.Command("git", "-C", g.dir, "cat-file", "-e", sha+"^{commit}").Run() == nil
}

func (g gitRepository) changedPaths(base, head string) ([]string, error) {
	output, err := g.output("diff", "--name-only", "--no-renames", base+"..."+head)
	if err != nil {
		return nil, err
	}
	return strings.Fields(output), nil
}
