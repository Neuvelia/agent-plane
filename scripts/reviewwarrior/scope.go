package main

import (
	"fmt"
	"strings"
)

// Diff budgets. About 50k tokens fit inline; above that the reviewer gets a
// smaller part and reads the rest itself, which needs room in its context.
const (
	inlineDiffMaxBytes  = 200_000
	partialDiffMaxBytes = 120_000
)

// reviewScope says which diff this run reviews.
type reviewScope struct {
	base, head string
	// since is the commit the previous review covered; empty for a full review.
	since string
}

func (s reviewScope) incremental() bool {
	return s.since != ""
}

// name is the scope as the workflow and the comment print it.
func (s reviewScope) name() string {
	if s.incremental() {
		return "incremental"
	}
	return "full"
}

// diffArgs are the git diff arguments of this scope, without the command.
func (s reviewScope) diffArgs() []string {
	if s.incremental() {
		return []string{"--find-renames", s.since, s.head}
	}
	return []string{"--find-renames", s.base + "..." + s.head}
}

func (s reviewScope) diffCommand() string {
	return "git diff " + strings.Join(s.diffArgs(), " ")
}

// chooseScope reviews only the commits after the previous review when that
// review's commit is still in this pull request's history: after a force push
// or a base change it is not, and the whole pull request is read again.
func chooseScope(repo gitRepository, base, head, previous string, full bool) (reviewScope, error) {
	scope := reviewScope{base: base, head: head}
	if full || previous == "" || !repo.hasCommit(previous) {
		return scope, nil
	}
	onHead, err := repo.isAncestor(previous, head)
	if err != nil {
		return scope, err
	}
	afterBase, err := repo.isAncestor(base, previous)
	if err != nil {
		return scope, err
	}
	if onHead && afterBase {
		scope.since = previous
	}
	return scope, nil
}

// fileDiff is the diff of one file, starting at its `diff --git` line.
type fileDiff struct {
	path string
	text string
}

func splitDiff(diff string) []fileDiff {
	var files []fileDiff
	for _, chunk := range strings.SplitAfter(diff, "\n") {
		if strings.HasPrefix(chunk, "diff --git ") || len(files) == 0 {
			files = append(files, fileDiff{path: diffPath(chunk)})
		}
		files[len(files)-1].text += chunk
	}
	return files
}

// diffPath reads the new path from a `diff --git a/x b/y` line.
func diffPath(header string) string {
	header = strings.TrimSuffix(header, "\n")
	if index := strings.LastIndex(header, " b/"); index >= 0 {
		return header[index+3:]
	}
	return header
}

// budgetDiff returns the diff to inline and the files left out. A diff under
// the inline budget goes in whole; a larger one keeps whole files, in order,
// up to the partial budget.
func budgetDiff(diff string) (string, []string) {
	if len(diff) <= inlineDiffMaxBytes {
		return diff, nil
	}
	var kept strings.Builder
	var omitted []string
	for _, file := range splitDiff(diff) {
		if kept.Len()+len(file.text) > partialDiffMaxBytes {
			omitted = append(omitted, file.path)
			continue
		}
		kept.WriteString(file.text)
	}
	return kept.String(), omitted
}

// scopeInstructions is the `## Review scope` section, which is an instruction
// written by the tool, not pull request content.
func scopeInstructions(scope reviewScope, diffEmpty bool, omitted []string) string {
	var text strings.Builder
	text.WriteString("## Review scope\n\n")
	whole := reviewScope{base: scope.base, head: scope.head}.diffCommand()
	switch {
	case scope.incremental() && diffEmpty:
		fmt.Fprintf(&text, "No commit was added since the last review (`%s`). Recheck every previous finding against the author responses and the current code. The diff of the whole pull request is `%s`; read one file of it at a time.\n", short(scope.since), whole)
	case scope.incremental():
		fmt.Fprintf(&text, "This run reviews only the commits after the last review: `%s`. Recheck every previous finding against the current code. The diff of the whole pull request is `%s`; read one file of it at a time, and only when a change since the last review depends on it.\n", scope.diffCommand(), whole)
	default:
		fmt.Fprintf(&text, "This run reviews the whole pull request: `%s`.\n", whole)
	}
	if len(omitted) > 0 {
		fmt.Fprintf(&text, "\nThe diff is too large to inline whole. These files are not in the prompt. Read the diff of each one with `%s -- <path>`, one file at a time:\n\n", scope.diffCommand())
		for _, name := range omitted {
			fmt.Fprintf(&text, "- `%s`\n", name)
		}
	}
	return text.String()
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
