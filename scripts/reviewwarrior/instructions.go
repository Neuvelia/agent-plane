package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Paths inside the instructions tree, which the workflow checks out at the
// pull request's base commit.
const (
	promptPath = ".github/review-warrior/prompt.md"
	rulesDir   = ".github/review-warrior/rules"
	agentsFile = "AGENTS.md"
)

var errInvalidRule = errors.New("invalid review rule")

// rule is one domain review fragment: a body that applies when a changed path
// matches one of its include globs.
type rule struct {
	path    string
	include []string
	body    string
}

// parseRule reads a fragment with front matter of the form
//
//	---
//	include:
//	  - "internal/runtime/**"
//	---
func parseRule(name, source string) (rule, error) {
	lines := strings.Split(source, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return rule{}, fmt.Errorf("%w: %s: missing front matter", errInvalidRule, name)
	}
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing == -1 {
		return rule{}, fmt.Errorf("%w: %s: unterminated front matter", errInvalidRule, name)
	}
	include := parseIncludes(lines[1:closing])
	if len(include) == 0 {
		return rule{}, fmt.Errorf("%w: %s: front matter has no include globs", errInvalidRule, name)
	}
	body := strings.TrimSpace(strings.Join(lines[closing+1:], "\n"))
	return rule{path: name, include: include, body: body}, nil
}

var includeItem = regexp.MustCompile(`^\s*-\s*"?([^"]+?)"?\s*$`)

func parseIncludes(frontMatter []string) []string {
	var include []string
	inList := false
	for _, line := range frontMatter {
		if strings.TrimSpace(line) == "include:" {
			inList = true
			continue
		}
		if match := includeItem.FindStringSubmatch(line); inList && match != nil {
			include = append(include, match[1])
			continue
		}
		inList = false
	}
	return include
}

var globWildcard = regexp.MustCompile(`\*\*/|\*\*|\*`)

var wildcardPatterns = map[string]string{"**/": "(?:.*/)?", "**": ".*", "*": "[^/]*"}

// matchGlob matches a slash-separated path against a glob in which `*` stays
// inside one path segment and `**` crosses segments.
func matchGlob(glob, name string) bool {
	parts := globWildcard.Split(glob, -1)
	wildcards := globWildcard.FindAllString(glob, -1)
	var pattern strings.Builder
	pattern.WriteString("^")
	for i, part := range parts {
		pattern.WriteString(regexp.QuoteMeta(part))
		if i < len(wildcards) {
			pattern.WriteString(wildcardPatterns[wildcards[i]])
		}
	}
	pattern.WriteString("$")
	return regexp.MustCompile(pattern.String()).MatchString(name)
}

// loadRules reads every fragment under rulesDir, sorted by file name.
func loadRules(root string) ([]rule, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rulesDir)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read review rules: %w", err)
	}
	var rules []rule
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := path.Join(rulesDir, entry.Name())
		source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("read review rule: %w", err)
		}
		parsed, err := parseRule(name, string(source))
		if err != nil {
			return nil, err
		}
		rules = append(rules, parsed)
	}
	return rules, nil
}

// selectRules returns the rules that match at least one changed path.
func selectRules(rules []rule, changed []string) []rule {
	var selected []rule
	for _, candidate := range rules {
		if ruleMatches(candidate, changed) {
			selected = append(selected, candidate)
		}
	}
	return selected
}

func ruleMatches(candidate rule, changed []string) bool {
	for _, glob := range candidate.include {
		for _, name := range changed {
			if matchGlob(glob, name) {
				return true
			}
		}
	}
	return false
}

// agentsPaths returns the root AGENTS.md and every AGENTS.md in a directory
// that contains a changed path, as sorted slash paths that exist under root.
func agentsPaths(root string, changed []string) []string {
	candidates := map[string]bool{agentsFile: true}
	for _, name := range changed {
		for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			candidates[path.Join(dir, agentsFile)] = true
		}
	}
	var existing []string
	for candidate := range candidates {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate))); err == nil && info.Mode().IsRegular() {
			existing = append(existing, candidate)
		}
	}
	sort.Strings(existing)
	return existing
}
