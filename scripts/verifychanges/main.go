// Command verifychanges reports local Git changes without running checks.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"unicode/utf8"
)

type change struct {
	Path    string   `json:"path"`
	Sources []string `json:"sources"`
}

type report struct {
	ChecksExecuted bool     `json:"checks_executed"`
	Changes        []change `json:"changes"`
}

func main() {
	if err := run(os.Stdout, "."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer, cwd string) error {
	rootBytes, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("Git root lookup: %w", err)
	}
	root := strings.TrimSuffix(string(rootBytes), "\n")
	if root == "" {
		return fmt.Errorf("Git root lookup returned an empty path")
	}

	queries := []struct {
		source string
		args   []string
	}{
		{"staged", []string{"diff", "--cached", "--no-renames", "--no-ext-diff", "--name-only", "-z", "--"}},
		{"unstaged", []string{"diff", "--no-renames", "--no-ext-diff", "--name-only", "-z", "--"}},
		{"untracked", []string{"ls-files", "--others", "--exclude-standard", "-z", "--"}},
	}
	paths := make(map[string]map[string]bool)
	for _, query := range queries {
		output, err := gitOutput(root, query.args...)
		if err != nil {
			return fmt.Errorf("Git %s query: %w", query.source, err)
		}
		for _, path := range strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00") {
			if path == "" {
				continue
			}
			if !utf8.ValidString(path) {
				return fmt.Errorf("Git %s query returned a path with invalid UTF-8", query.source)
			}
			if paths[path] == nil {
				paths[path] = make(map[string]bool)
			}
			paths[path][query.source] = true
		}
	}

	result := report{Changes: make([]change, 0, len(paths))}
	for path, sourceSet := range paths {
		sources := make([]string, 0, len(sourceSet))
		for source := range sourceSet {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		result.Changes = append(result.Changes, change{Path: path, Sources: sources})
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		return result.Changes[i].Path < result.Changes[j].Path
	})
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode change inventory: %w", err)
	}
	encoded = append(encoded, '\n')
	_, err = out.Write(encoded)
	return err
}

func gitOutput(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return output, nil
}
