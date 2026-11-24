//go:build unix

package secretscan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// confinedPath reports whether rel is a clean, relative, slash-separated path
// that cannot leave the root or name Git's own metadata. It mirrors the rule of
// gitstate.ConfinedPath; adapters may not import each other (R2), so the small
// rule is repeated here and tested here.
func confinedPath(rel string) bool {
	if rel == "" || !utf8.ValidString(rel) || strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." || part == ".git" {
			return false
		}
	}
	return true
}

// ancestry is the verdict on the directories above a path.
type ancestry int

const (
	ancestorsPlain   ancestry = iota // every ancestor is a real directory
	ancestorsMissing                 // an ancestor does not exist: the path was deleted
	ancestorsRefused                 // an ancestor is a symlink, a file or unreadable
)

// checkAncestors Lstats each directory between root and rel's final component.
// A symlinked ancestor could lead outside the root, so it is refused and never
// followed.
func checkAncestors(root, rel string) ancestry {
	parts := strings.Split(rel, "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return ancestorsMissing
		case err != nil || !info.IsDir():
			return ancestorsRefused
		}
	}
	return ancestorsPlain
}
