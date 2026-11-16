package worktree

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRefNamesHostileNodeIDs(t *testing.T) {
	run := "0b6f1a2e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	hostile := []string{
		"../x", "a b", "x.lock", "ünï", "a/b", "-leading", "@{u}", "a..b", "x~1", "x^", "x:y", "x\\y",
		"tab\there", "newline\nhere", strings.Repeat("n", 256), strings.Repeat("ü", 128), ".", "..", "~", "*",
	}
	seen := map[string]string{}
	for _, id := range hostile {
		ref, err := UnitRef(run, id)
		if err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		if other, dup := seen[ref]; dup {
			t.Fatalf("%q and %q collide on %s", id, other, ref)
		}
		seen[ref] = id
		if err := validateRef(ref); err != nil {
			t.Fatalf("%q: generated ref fails own validation: %v", id, err)
		}
		for _, part := range strings.Split(ref, "/") {
			if len(part) > 255 {
				t.Fatalf("%q: ref component exceeds 255 bytes", id)
			}
		}
		if out, err := exec.Command("/usr/bin/git", "check-ref-format", ref).CombinedOutput(); err != nil {
			t.Fatalf("%q -> %s rejected by git: %s", id, ref, out)
		}
	}
	// Sanitization alone would merge these; the hash keeps them distinct.
	a, _ := UnitRef(run, "a b")
	b, _ := UnitRef(run, "a_b")
	if a == b {
		t.Fatal("IDs that sanitize identically must still get distinct refs")
	}
}

func TestRefNamesAreDeterministicAndScopedToTheRun(t *testing.T) {
	run := "0b6f1a2e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	a1, _ := UnitRef(run, "worker")
	a2, _ := UnitRef(run, "worker")
	b, _ := UnitRef("other-run", "worker")
	if a1 != a2 || a1 == b || !strings.HasPrefix(a1, "refs/agentplane/"+run+"/units/worker-") {
		t.Fatalf("unexpected refs: %s %s %s", a1, a2, b)
	}
	tip, err := IntegratedRef(run)
	if err != nil || tip != "refs/agentplane/"+run+"/integrated" {
		t.Fatalf("integrated ref: %q %v", tip, err)
	}
}

func TestRefNamesRejectInvalidInput(t *testing.T) {
	for _, run := range []string{"", "../x", "a/b", "a b", strings.Repeat("a", 65), "x\x00"} {
		if _, err := UnitRef(run, "n"); err == nil {
			t.Errorf("run id %q must be rejected", run)
		}
		if _, err := IntegratedRef(run); err == nil {
			t.Errorf("integrated run id %q must be rejected", run)
		}
	}
	for _, node := range []string{"", "a\x00b", "\xff\xfe", strings.Repeat("x", 4097)} {
		if _, err := UnitRef("run-1", node); err == nil {
			t.Errorf("node id %q must be rejected", node)
		}
	}
	for _, ref := range []string{"refs/heads/main", "refs/agentplane/../x", "refs/agentplane/r/units/", "refs/agentplane/r/other", "HEAD"} {
		if err := validateRef(ref); err == nil {
			t.Errorf("ref %q must be rejected", ref)
		}
	}
}
