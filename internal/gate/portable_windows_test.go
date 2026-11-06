//go:build windows

package gate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agentplane/internal/graph"
	"golang.org/x/sys/windows"
)

func TestWindowsPortableDiffComparesFiles(t *testing.T) {
	for _, tc := range []struct {
		name, right string
		red         bool
	}{{"equal", "same", false}, {"different", "other", true}} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			for name, contents := range map[string]string{"left": "same", "right": tc.right} {
				if err := os.WriteFile(filepath.Join(cwd, name), []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			n := node("command")
			n.Config["cwd"] = cwd
			n.Config["command"] = "agentplane.diff"
			n.Config["left_input"] = "left"
			n.Config["right_input"] = "right"
			n.Inputs["left"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
			n.Inputs["right"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
			fact, err := evaluate(t, n, paths())
			var red RedError
			if fact == nil || errors.As(err, &red) != tc.red || (err != nil && !tc.red) {
				t.Fatalf("fact=%+v err=%v", fact, err)
			}
			if len(fact.Evidence) != 1 || fact.Evidence[0].ExitCode == nil || (*fact.Evidence[0].ExitCode == 1) != tc.red {
				t.Fatalf("wrong comparison status: %+v", fact.Evidence)
			}
		})
	}
}

func TestWindowsPortableRootRejectsAlternateNamespaces(t *testing.T) {
	for _, path := range []string{`\\server\share\project`, `\\?\C:\project`, `C:project`, `\project`} {
		if root, err := openComparisonRoot(path); err == nil {
			root.Close()
			t.Fatalf("unsafe cwd accepted: %q", path)
		} else if !strings.Contains(err.Error(), "drive-absolute") {
			t.Fatalf("unsafe cwd %q lacks actionable diagnostic: %v", path, err)
		}
	}
}

func TestWindowsPortableRejectsDeviceAliases(t *testing.T) {
	for _, name := range []string{"CON", "con.txt", "CON .txt", "NUL", "COM1", "LPT9", "COM¹", "COM².txt", "COM³", "LPT¹", "LPT².txt", "LPT³", "data:stream", "trailing. ", `a\b`} {
		if err := validWindowsComponent(name); err == nil {
			t.Fatalf("unsafe component accepted: %q", name)
		}
	}
}

func TestWindowsPortableEmptyLargeMissingAndCanceled(t *testing.T) {
	for _, tc := range []struct {
		name, left, right string
		missing, cancel   bool
		status            int
	}{
		{name: "empty"},
		{name: "large equal", left: strings.Repeat("a", 1024*1024+1), right: strings.Repeat("a", 1024*1024+1)},
		{name: "large different", left: strings.Repeat("a", 1024*1024+1), right: strings.Repeat("a", 1024*1024) + "b", status: 1},
		{name: "missing right", left: "x", missing: true},
		{name: "canceled", left: "x", right: "x", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			if err := os.WriteFile(filepath.Join(cwd, "left"), []byte(tc.left), 0600); err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				if err := os.WriteFile(filepath.Join(cwd, "right"), []byte(tc.right), 0600); err != nil {
					t.Fatal(err)
				}
			}
			n := windowsDiffNode(cwd)
			c, err := parse(n)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			fact, err := portableCommand(ctx, c, paths())
			if tc.missing || tc.cancel {
				if err == nil || fact.Verdict != "" || len(fact.Evidence) != 0 {
					t.Fatalf("error returned comparison verdict: fact=%+v err=%v", fact, err)
				}
				if tc.cancel && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation error = %v", err)
				}
				return
			}
			if err != nil || len(fact.Evidence) != 1 || fact.Evidence[0].ExitCode == nil || *fact.Evidence[0].ExitCode != tc.status {
				t.Fatalf("wrong comparison result: fact=%+v err=%v", fact, err)
			}
		})
	}
}

func TestWindowsPortableRejectsEscapingOperands(t *testing.T) {
	root, err := openComparisonRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, operand := range []string{"../left", "/left", `C:/left`, `a\left`, "data:stream", "COM¹"} {
		if file, err := root.Open(operand); err == nil {
			file.Close()
			t.Fatalf("unsafe operand accepted: %q", operand)
		}
	}
}

func TestWindowsPortableRejectsAlternateDataStream(t *testing.T) {
	root, err := openComparisonRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if file, err := root.Open(`data.txt:private`); err == nil {
		file.Close()
		t.Fatal("alternate data stream accepted")
	}
}

func TestWindowsPortableInvalidStreamHasNoVerdictOrFileChange(t *testing.T) {
	cwd := t.TempDir()
	left := filepath.Join(cwd, "left")
	if err := os.WriteFile(left, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	n := node("command")
	n.Config["cwd"] = cwd
	n.Config["command"] = "agentplane.diff"
	n.Config["left_input"] = "left"
	n.Config["right_input"] = "right"
	n.Inputs["left"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
	n.Inputs["right"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
	in := map[string]graph.Value{"left": graph.NewStringValue("left"), "right": graph.NewStringValue("right:private")}
	if fact, err := evaluate(t, n, in); fact != nil || err == nil || !strings.Contains(err.Error(), "component") {
		t.Fatalf("invalid stream gave verdict or unclear error: fact=%+v err=%v", fact, err)
	}
	if contents, err := os.ReadFile(left); err != nil || string(contents) != "retained" {
		t.Fatalf("comparison changed input: %q %v", contents, err)
	}
}

func TestWindowsPortableDetectsCwdReplacement(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "workspace")
	if err := os.Mkdir(cwd, 0700); err != nil {
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
		t.Fatal("replaced cwd accepted")
	}
}

func TestWindowsPortableRejectsReparseDirectory(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "real", "data"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(cwd, "real"), filepath.Join(cwd, "link")); err != nil {
		t.Fatalf("native reparse test requires symlink creation: %v", err)
	}
	root, err := openComparisonRoot(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if file, err := root.Open("link/data"); err == nil {
		file.Close()
		t.Fatal("reparse directory accepted")
	}
	if rejected, err := openComparisonRoot(filepath.Join(cwd, "link")); err == nil {
		rejected.Close()
		t.Fatal("reparse cwd accepted")
	}
	if err := os.Symlink(filepath.Join(cwd, "real", "data"), filepath.Join(cwd, "alias")); err != nil {
		t.Fatalf("native reparse test requires file symlink creation: %v", err)
	}
	if file, err := root.Open("alias"); err == nil {
		file.Close()
		t.Fatal("reparse input accepted")
	}
}

func TestWindowsPortableRejectsReparseCwdAncestor(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatalf("native reparse test requires symlink creation: %v", err)
	}
	if root, err := openComparisonRoot(filepath.Join(alias, "workspace")); err == nil {
		root.Close()
		t.Fatal("reparse cwd ancestor accepted")
	}
}

func TestWindowsPortablePinnedDirectorySurvivesPathReplacement(t *testing.T) {
	cwd := t.TempDir()
	branch := filepath.Join(cwd, "branch")
	if err := os.Mkdir(branch, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(branch, "data"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := openComparisonRoot(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	pinned, err := openWindowsChild(root.(*windowsComparisonRoot).handle, "branch", true)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pinned)
	if err := os.Rename(branch, filepath.Join(cwd, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(branch, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(branch, "data"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	handle, err := openWindowsChild(pinned, "data", false)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(handle), "pinned comparison input")
	defer file.Close()
	bytes, err := io.ReadAll(file)
	if err != nil || string(bytes) != "old" {
		t.Fatalf("pinned walk escaped into replacement: %q %v", bytes, err)
	}
}

func TestWindowsPortableDetectsChangedOpenFileMetadata(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "input")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := openComparisonRoot(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.Open("input")
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
	if err := verifyComparisonFile(file, before, "input"); err == nil || !strings.Contains(err.Error(), "changed during read") {
		t.Fatalf("detectable mutation accepted: %v", err)
	}
}

func TestWindowsPortableRejectsACLDeniedInput(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "left")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "right"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Skipf("ACL inspection unavailable on this filesystem: %v", err)
	}
	if original == nil {
		t.Skip("filesystem returned no security descriptor")
	}
	originalDACL, _, err := original.DACL()
	if err != nil {
		t.Skipf("filesystem returned no DACL: %v", err)
	}
	denied, err := windows.SecurityDescriptorFromString("D:(D;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	deniedDACL, _, err := denied.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, deniedDACL, nil); err != nil {
		t.Skipf("ACL denial unavailable on this filesystem: %v", err)
	}
	runtime.KeepAlive(denied)
	defer func() {
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, originalDACL, nil); err != nil {
			t.Errorf("restore input ACL: %v", err)
		}
		runtime.KeepAlive(original)
	}()
	if file, err := os.Open(path); err == nil {
		file.Close()
		t.Skip("ACL denial could not be reproduced under this token")
	}
	if fact, err := evaluate(t, windowsDiffNode(cwd), paths()); fact != nil || err == nil {
		t.Fatalf("ACL-denied input produced a verdict: fact=%+v err=%v", fact, err)
	}
}

func windowsDiffNode(cwd string) graph.NodeDefinition {
	n := node("command")
	n.Config["cwd"] = cwd
	n.Config["command"] = "agentplane.diff"
	n.Config["left_input"] = "left"
	n.Config["right_input"] = "right"
	n.Inputs["left"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
	n.Inputs["right"] = graph.PortDefinition{Type: graph.ValueTypeString, Required: true}
	return n
}
