package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Tag forms (ADR-0038 decision 4): `roadmap: F1.7-AC1, F1.7-AC2` in a comment,
// or `[F1.7-AC1]` inside a vitest name.
var (
	commentTagPattern = regexp.MustCompile(`roadmap:\s*([^\n]+)`)
	nameTagPattern    = regexp.MustCompile(`\[((?:F\d\.\d+[a-z]?(?:-E\d+)?|FX\.\d+|REL\d+)-AC\d+)\]`)
	tagIDPattern      = regexp.MustCompile(`(?:F\d\.\d+[a-z]?(?:-E\d+)?|FX\.\d+|REL\d+)-AC\d+`)
)

// isTestFile reports whether a path holds tests that may carry criterion tags.
func isTestFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, ".test.ts") ||
		strings.HasSuffix(rel, ".test.tsx") || strings.HasPrefix(rel, "desktop/e2e/")
}

func splitEvidence(item string) (string, string) {
	path, name, _ := strings.Cut(item, "::")
	return path, name
}

func (c *checker) read(rel string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(c.root, rel))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// checkEvidence verifies that each evidence file exists and that a named test
// (or tag, when the name is a criterion ID) occurs in it.
func (c *checker) checkEvidence(location string, items []string) {
	for _, item := range items {
		path, name := splitEvidence(item)
		content, ok := c.read(path)
		if !ok {
			c.out.errorf(location, "evidence file %s does not exist", path)
			continue
		}
		if name != "" && !containsTest(path, content, name) {
			c.out.errorf(location, "evidence %s: %q not found in %s", item, name, path)
		}
	}
}

func containsTest(path, content, name string) bool {
	if criterionIDPattern.MatchString(name) {
		return contains(fileTags(content), name)
	}
	if strings.HasSuffix(path, ".go") {
		return regexp.MustCompile(`(?m)^func ` + regexp.QuoteMeta(name) + `\(`).MatchString(content)
	}
	return strings.Contains(content, name)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// checkTagged requires a met criterion to be carried by at least one of its
// evidence test files, so the link survives test renames in both directions.
func (c *checker) checkTagged(ac *criterion) {
	for _, item := range ac.Evidence {
		path, _ := splitEvidence(item)
		if !isTestFile(path) {
			continue
		}
		if content, ok := c.read(path); ok && contains(fileTags(content), ac.ID) {
			return
		}
	}
	c.out.errorf(ac.ID, "met, but no evidence test file carries the tag `roadmap: %s`", ac.ID)
}

func fileTags(content string) []string {
	var tags []string
	for _, match := range commentTagPattern.FindAllStringSubmatch(content, -1) {
		tags = append(tags, tagIDPattern.FindAllString(match[1], -1)...)
	}
	for _, match := range nameTagPattern.FindAllStringSubmatch(content, -1) {
		tags = append(tags, match[1])
	}
	return tags
}

// checkOrphanTags warns about tags in test files that name no registered
// criterion, for example after a criterion was renumbered.
func (c *checker) checkOrphanTags() {
	files, err := c.testFiles()
	if err != nil {
		c.out.errorf(".", "scan test files: %v", err)
	}
	for _, rel := range files {
		content, _ := c.read(rel)
		for _, tag := range fileTags(content) {
			if !c.criteria[tag] {
				c.out.warnf(rel, "tag %s names no criterion in %s", tag, featuresPath)
			}
		}
	}
}

func (c *checker) testFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(c.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && skipDir(entry.Name()) {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(c.root, path)
		if relErr == nil && !entry.IsDir() && isTestFile(rel) {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func skipDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == "dist" || name == "out" || name == "storybook-static"
}
