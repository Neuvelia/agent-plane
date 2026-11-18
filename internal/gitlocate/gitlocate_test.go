package gitlocate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeStat(present ...string) func(string) (os.FileInfo, error) {
	return func(p string) (os.FileInfo, error) {
		for _, w := range present {
			if p == w {
				return os.Stat(os.Args[0]) // any regular executable file
			}
		}
		return nil, os.ErrNotExist
	}
}

func noLink(string) (string, error) { return "", os.ErrNotExist }

func lookTo(path string) func(string) (string, error) {
	return func(string) (string, error) { return path, nil }
}

func TestDarwinShimWithoutDeveloperToolsIsRefused(t *testing.T) {
	l := Locator{GOOS: "darwin", LookPath: lookTo(DarwinGitShim), Stat: fakeStat(), Readlink: noLink}
	if _, err := l.Resolve(); !errors.Is(err, ErrDeveloperToolsMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestDarwinShimWithDeveloperToolsIsAccepted(t *testing.T) {
	for name, l := range map[string]Locator{
		"command line tools": {Stat: fakeStat(DarwinCLTGit), Readlink: noLink},
		"xcode-select target": {Stat: fakeStat("/Xcode-beta/Contents/Developer/usr/bin/git"), Readlink: func(p string) (string, error) {
			if p != DarwinSelectLink {
				t.Errorf("readlink %q", p)
			}
			return "/Xcode-beta/Contents/Developer", nil
		}},
	} {
		t.Run(name, func(t *testing.T) {
			l.GOOS, l.LookPath = "darwin", lookTo(DarwinGitShim)
			if got, err := l.Resolve(); err != nil || got != DarwinGitShim {
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
}

func TestNonShimGitOnDarwinIsNotGuarded(t *testing.T) {
	l := Locator{GOOS: "darwin", LookPath: lookTo("/opt/homebrew/bin/git"), Stat: fakeStat(), Readlink: noLink}
	if _, err := l.Resolve(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRefusesCurrentDirectoryGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("PATH", ".")
	_, err = ResolveGit()
	if !errors.Is(err, ErrGitNotFound) || !strings.Contains(err.Error(), "relative or current-directory git") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveRefusesNonAbsoluteResult(t *testing.T) {
	if _, err := (Locator{LookPath: lookTo("bin/git")}).Resolve(); !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("got %v", err)
	}
}
