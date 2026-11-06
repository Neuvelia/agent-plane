//go:build darwin || linux

package gate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

func TestPortableDiffComparesOpenedFilesWithoutProcessEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, right string
		red         bool
	}{
		{name: "equal", right: "same bytes"},
		{name: "different", right: "other bytes", red: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := diffNode(t)
			n.Config["command"] = "agentplane.diff"
			cwd := n.Config["cwd"].(string)
			for name, value := range map[string]string{"left": "same bytes", "right": tc.right} {
				if err := os.WriteFile(filepath.Join(cwd, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			fact, err := evaluate(t, n, paths())
			var red RedError
			if fact == nil || errors.As(err, &red) != tc.red || (err != nil && !tc.red) {
				t.Fatalf("fact=%+v err=%v", fact, err)
			}
			if fact.Kind != events.CheckCommand || len(fact.Evidence) != 1 {
				t.Fatalf("unexpected evidence: %+v", fact)
			}
			ev := fact.Evidence[0]
			if ev.Source != "command" || ev.ExitCode == nil || (*ev.ExitCode == 1) != tc.red || len(ev.Stdout) != 0 || len(ev.Stderr) != 0 || ev.StdoutOmitted != 0 || ev.StderrOmitted != 0 {
				t.Fatalf("unexpected portable comparison evidence: %+v", ev)
			}
		})
	}
}

func TestPortableDiffEmptyLargeMissingAndCanceled(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		missing           bool
		cancel            bool
		red               bool
	}{
		{name: "empty"},
		{name: "large equal", left: strings.Repeat("a", 1024*1024+1), right: strings.Repeat("a", 1024*1024+1)},
		{name: "large different", left: strings.Repeat("a", 1024*1024+1), right: strings.Repeat("a", 1024*1024) + "b", red: true},
		{name: "missing right", left: "x", missing: true},
		{name: "canceled", left: "x", right: "x", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := diffNode(t)
			n.Config["command"] = "agentplane.diff"
			cwd := n.Config["cwd"].(string)
			if err := os.WriteFile(filepath.Join(cwd, "left"), []byte(tc.left), 0600); err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				if err := os.WriteFile(filepath.Join(cwd, "right"), []byte(tc.right), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if tc.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			c, err := parse(n)
			if err != nil {
				t.Fatal(err)
			}
			fact, err := portableCommand(ctx, c, paths())
			if tc.missing || tc.cancel {
				if err == nil || fact.Verdict != "" || len(fact.Evidence) != 0 {
					t.Fatalf("unsafe verdict: fact=%+v err=%v", fact, err)
				}
				if tc.cancel && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation error = %v", err)
				}
				return
			}
			if err != nil || (fact.Verdict == "red") != tc.red || len(fact.Evidence) != 1 {
				t.Fatalf("fact=%+v err=%v", fact, err)
			}
		})
	}
}

func TestPortableDiffRejectsSymlinkInCwdAncestor(t *testing.T) {
	n := diffNode(t)
	n.Config["command"] = "agentplane.diff"
	cwd := n.Config["cwd"].(string)
	if err := os.WriteFile(filepath.Join(cwd, "left"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "right"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	aliasBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliasBase, "alias")
	if err := os.Symlink(cwd, alias); err != nil {
		t.Fatal(err)
	}
	n.Config["cwd"] = alias
	if fact, err := evaluate(t, n, paths()); fact != nil || err == nil {
		t.Fatalf("symlinked root accepted: fact=%+v err=%v", fact, err)
	}
	ancestorBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ancestor := filepath.Join(ancestorBase, "ancestor")
	if err := os.Symlink(filepath.Dir(cwd), ancestor); err != nil {
		t.Fatal(err)
	}
	n.Config["cwd"] = filepath.Join(ancestor, filepath.Base(cwd))
	if fact, err := evaluate(t, n, paths()); fact != nil || err == nil {
		t.Fatalf("symlinked cwd ancestor accepted: fact=%+v err=%v", fact, err)
	}
}

func TestPortableDiffRejectsUnsafeRelativeOperands(t *testing.T) {
	for _, variant := range []string{"../left", "/dev/null", "C:/left", "alias/left", "link"} {
		t.Run(variant, func(t *testing.T) {
			n := diffNode(t)
			n.Config["command"] = "agentplane.diff"
			cwd := n.Config["cwd"].(string)
			if err := os.WriteFile(filepath.Join(cwd, "left"), []byte("same"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cwd, "right"), []byte("same"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(".", filepath.Join(cwd, "alias")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("left", filepath.Join(cwd, "link")); err != nil {
				t.Fatal(err)
			}
			in := paths()
			in["left"] = graph.NewStringValue(variant)
			if fact, err := evaluate(t, n, in); fact != nil || err == nil {
				t.Fatalf("unsafe operand accepted: fact=%+v err=%v", fact, err)
			}
		})
	}
}

func TestPortableDiffKeepsOneCwdIdentityAcrossBothInputs(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(base, "workspace")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "left"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := openComparisonRoot(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(cwd, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "right"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := root.Open("right"); err == nil {
		file.Close()
		t.Fatal("replaced cwd accepted for second comparison input")
	}
}

func TestPortableDiffRejectsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode 000 files")
	}
	n := diffNode(t)
	n.Config["command"] = "agentplane.diff"
	cwd := n.Config["cwd"].(string)
	left := filepath.Join(cwd, "left")
	if err := os.WriteFile(left, []byte("same"), 0000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(left, 0000); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "right"), []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	if fact, err := evaluate(t, n, paths()); fact != nil || err == nil {
		t.Fatalf("unreadable input accepted: fact=%+v err=%v", fact, err)
	}
}

func TestPortableDiffDetectsChangedOpenFileMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after and longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyComparisonFile(file, before, "left"); err == nil || !strings.Contains(err.Error(), "changed during read") {
		t.Fatalf("detectable in-place mutation accepted: %v", err)
	}
}
