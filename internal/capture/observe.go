// Package capture observes bounded file images within a pinned workspace.
// It never attributes shared-workspace writes to a particular agent.
package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"agentplane/internal/events"
)

const MaxFileBytes = 10 << 20
const MaxRunBytes = 100 << 20
const MaxInventoryItems = 65536

var ErrWorkspaceChanged = errors.New("capture workspace identity changed")
var ErrInventoryLimit = errors.New("capture inventory exceeds bound")
var ErrUnsafePath = errors.New("capture inventory contains unsafe path")

// Workspace is returned by a registry-backed resolver. Identity is the
// selected registered directory identity, not a graph-supplied path string.
type Workspace struct{ Path, Identity string }
type Resolver func() (Workspace, error)

// DirectoryIdentity uses the same OS identity representation as the desktop
// registry. It is exposed for deterministic fixtures; production still gets
// its expected identity from Registry.ResolveWorkspace.
func DirectoryIdentity(selected string) (string, error) {
	info, err := os.Lstat(selected)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrWorkspaceChanged
	}
	return directoryIdentity(info)
}

type Budget struct {
	mu   sync.Mutex
	used int64
}

func (b *Budget) Reserve(size int64) bool {
	if b == nil || size < 0 || size > MaxRunBytes {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used > MaxRunBytes-size {
		return false
	}
	b.used += size
	return true
}
func (b *Budget) Used() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.used }

type Image struct {
	Path        string
	Present     bool
	Content     []byte
	Digest      [32]byte
	Unavailable string
}
type Snapshot struct {
	Workspace Workspace
	Images    map[string]Image
}
type Change struct {
	Path          string
	Before, After Image
}

func normalizeScope(scope []string) ([]string, error) {
	if scope == nil {
		return nil, errors.New("capture scope is absent")
	}
	rules, err := events.NormalizeUnitScope(scope)
	if err != nil {
		return nil, err
	}
	for i, rule := range rules {
		if strings.HasSuffix(rule, "/**") {
			rule = strings.TrimSuffix(rule, "**")
		}
		if strings.ContainsAny(rule, "*?[]") {
			return nil, errors.New("capture scope pattern is unsupported")
		}
		rules[i] = rule
	}
	sort.Strings(rules)
	return rules, nil
}

func safeRelative(value string) bool {
	if value == "" || value == "." || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") || len(value) > 512 || !utf8.ValidString(value) || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || part == ".git" {
			return false
		}
	}
	return true
}

func safeComponents(root *os.Root, relative string) (os.FileInfo, error) {
	if relative == "." {
		return root.Stat(".")
	}
	if !safeRelative(relative) {
		return nil, os.ErrInvalid
	}
	current := ""
	var info os.FileInfo
	for _, component := range strings.Split(relative, "/") {
		if current == "" {
			current = component
		} else {
			current += "/" + component
		}
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, os.ErrInvalid
		}
	}
	return info, nil
}

func openPinned(resolver Resolver) (*os.Root, Workspace, error) {
	selected, err := resolver()
	if err != nil || selected.Path == "" || selected.Identity == "" {
		return nil, Workspace{}, ErrWorkspaceChanged
	}
	before, err := os.Lstat(selected.Path)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, Workspace{}, ErrWorkspaceChanged
	}
	root, err := os.OpenRoot(selected.Path)
	if err != nil {
		return nil, Workspace{}, ErrWorkspaceChanged
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		root.Close()
		return nil, Workspace{}, ErrWorkspaceChanged
	}
	actual, err := directoryIdentity(opened)
	if err != nil || actual != selected.Identity {
		root.Close()
		return nil, Workspace{}, ErrWorkspaceChanged
	}
	confirmed, err := resolver()
	if err != nil || confirmed != selected {
		root.Close()
		return nil, Workspace{}, ErrWorkspaceChanged
	}
	return root, selected, nil
}

// Take inventories even files whose bytes cannot be captured, so later
// mutations are not falsely classified as creations with an empty preimage.
func Take(ctx context.Context, resolver Resolver, scope []string, budget *Budget) (Snapshot, error) {
	rules, err := normalizeScope(scope)
	if err != nil {
		return Snapshot{}, err
	}
	root, workspace, err := openPinned(resolver)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	snapshot := Snapshot{Workspace: workspace, Images: make(map[string]Image)}
	seenDirs := make(map[string]bool)
	var visit func(string) error
	visit = func(relative string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, seen := snapshot.Images[relative]; seen {
			return nil
		}
		if seenDirs[relative] {
			return nil
		}
		if len(snapshot.Images)+len(seenDirs) >= MaxInventoryItems {
			return ErrInventoryLimit
		}
		info, err := safeComponents(root, relative)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			if relative != "." && safeRelative(relative) {
				snapshot.Images[relative] = Image{Path: relative, Present: true, Unavailable: "unsafe_path"}
			}
			return nil
		}
		if info.IsDir() {
			if seenDirs[relative] {
				return nil
			}
			seenDirs[relative] = true
			directory, err := root.Open(relative)
			if err != nil {
				return ErrWorkspaceChanged
			}
			opened, err := directory.Stat()
			if err != nil || !os.SameFile(info, opened) {
				directory.Close()
				return ErrWorkspaceChanged
			}
			allEntries := make([]os.DirEntry, 0)
			for {
				entries, readErr := directory.ReadDir(128)
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					directory.Close()
					return readErr
				}
				if len(entries) > MaxInventoryItems-len(allEntries) {
					directory.Close()
					return ErrInventoryLimit
				}
				allEntries = append(allEntries, entries...)
				if errors.Is(readErr, io.EOF) || len(entries) == 0 {
					break
				}
			}
			sort.Slice(allEntries, func(i, j int) bool { return allEntries[i].Name() < allEntries[j].Name() })
			for _, entry := range allEntries {
				if entry.Name() == ".git" {
					continue
				}
				child := entry.Name()
				if relative != "." {
					child = relative + "/" + child
				}
				if !safeRelative(child) {
					directory.Close()
					return ErrUnsafePath
				}
				if err := visit(child); err != nil {
					directory.Close()
					return err
				}
			}
			return directory.Close()
		}
		image := Image{Path: relative, Present: true}
		if !info.Mode().IsRegular() {
			image.Unavailable = "unsafe_path"
		} else if info.Size() > MaxFileBytes {
			image.Unavailable = "size_limit"
		} else {
			file, openErr := root.Open(relative)
			if openErr != nil {
				image.Unavailable = "unsafe_path"
			} else {
				opened, statErr := file.Stat()
				if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
					image.Unavailable = "unsafe_path"
				} else {
					content, readErr := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
					if readErr != nil {
						image.Unavailable = "capture_failed"
					} else if len(content) > MaxFileBytes {
						image.Unavailable = "size_limit"
					} else if !budget.Reserve(int64(len(content))) {
						image.Unavailable = "size_limit"
					} else {
						image.Content = content
						image.Digest = sha256.Sum256(content)
					}
				}
				file.Close()
			}
		}
		snapshot.Images[relative] = image
		return nil
	}
	for _, rule := range rules {
		if rule == "./" {
			if err := visit("."); err != nil {
				return Snapshot{}, err
			}
			continue
		}
		selected := strings.TrimSuffix(rule, "/")
		if !safeRelative(selected) {
			return Snapshot{}, os.ErrInvalid
		}
		if err := visit(selected); err != nil {
			return Snapshot{}, err
		}
	}
	current, err := resolver()
	if err != nil || current != workspace {
		return Snapshot{}, ErrWorkspaceChanged
	}
	opened, err := root.Stat(".")
	latest, statErr := os.Lstat(workspace.Path)
	if err != nil || statErr != nil || !os.SameFile(opened, latest) {
		return Snapshot{}, ErrWorkspaceChanged
	}
	return snapshot, nil
}

func Changes(before, after Snapshot) ([]Change, error) {
	if before.Workspace != after.Workspace {
		return nil, ErrWorkspaceChanged
	}
	paths := make(map[string]bool, len(before.Images)+len(after.Images))
	for name := range before.Images {
		paths[name] = true
	}
	for name := range after.Images {
		paths[name] = true
	}
	keys := make([]string, 0, len(paths))
	for name := range paths {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	changes := make([]Change, 0)
	for _, name := range keys {
		left, hasLeft := before.Images[name]
		right, hasRight := after.Images[name]
		if !hasLeft {
			left = Image{Path: name}
		}
		if !hasRight {
			right = Image{Path: name}
		}
		if left.Present == right.Present && left.Unavailable == "" && right.Unavailable == "" && left.Digest == right.Digest {
			continue
		}
		changes = append(changes, Change{Path: name, Before: left, After: right})
	}
	return changes, nil
}

// TextDiff is a deterministic replacement hunk after removing common line
// prefix and suffix. It is bounded, and never guesses at binary content.
func TextDiff(before, after []byte) ([]byte, bool) {
	if !utf8.Valid(before) || !utf8.Valid(after) || bytes.IndexByte(before, 0) >= 0 || bytes.IndexByte(after, 0) >= 0 {
		return nil, false
	}
	left := strings.SplitAfter(string(before), "\n")
	right := strings.SplitAfter(string(after), "\n")
	if len(left)+len(right) > 100000 {
		return nil, false
	}
	start := 0
	for start < len(left) && start < len(right) && left[start] == right[start] {
		start++
	}
	endLeft, endRight := len(left), len(right)
	for endLeft > start && endRight > start && left[endLeft-1] == right[endRight-1] {
		endLeft--
		endRight--
	}
	var out strings.Builder
	out.WriteString("--- before\n+++ after\n@@\n")
	for _, line := range left[start:endLeft] {
		out.WriteByte('-')
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	for _, line := range right[start:endRight] {
		out.WriteByte('+')
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	if out.Len() > MaxFileBytes {
		return nil, false
	}
	return []byte(out.String()), true
}
