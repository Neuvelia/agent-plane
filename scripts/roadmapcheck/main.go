// Command roadmapcheck validates the roadmap registry (docs/roadmap/features.yaml
// and phases.yaml) against the repository and keeps docs/roadmap/STATUS.md in
// sync with it (ADR-0038).
//
// Usage:
//
//	go run ./scripts/roadmapcheck          # validate; fail if STATUS.md is stale
//	go run ./scripts/roadmapcheck -write   # validate and regenerate STATUS.md
//
// Errors exit with status 1; warnings are printed and never fail the run.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	write := flag.Bool("write", false, "regenerate docs/roadmap/STATUS.md")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	mode := verifyStatus
	if *write {
		mode = writeStatus
	}
	failed, err := run(*root, mode, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "roadmapcheck:", err)
		os.Exit(1)
	}
	if failed {
		os.Exit(1)
	}
}

type statusMode int

const (
	verifyStatus statusMode = iota
	writeStatus
)

// run validates the registry, handles STATUS.md and reports findings. It
// returns true when at least one error was found.
func run(root string, mode statusMode, out io.Writer) (bool, error) {
	reg, phases, err := loadRegistry(root)
	if err != nil {
		return false, err
	}
	c := newChecker(root, reg)
	c.checkRegistry(reg)
	c.checkPhases(phases)
	c.checkOrphanTags()
	if err := syncStatus(c, root, renderStatus(reg, phases), mode); err != nil {
		return false, err
	}
	if err := c.out.report(out); err != nil {
		return false, err
	}
	errors := c.out.errorCount()
	_, err = fmt.Fprintf(out, "roadmapcheck: %d error(s), %d warning(s)\n", errors, len(c.out)-errors)
	return errors > 0, err
}

func syncStatus(c *checker, root, rendered string, mode statusMode) error {
	path := filepath.Join(root, statusPath)
	if mode == writeStatus {
		return os.WriteFile(path, []byte(rendered), 0o644)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != rendered {
		c.out.errorf(statusPath, "out of date; run `make roadmap` and commit the result")
	}
	return nil
}
