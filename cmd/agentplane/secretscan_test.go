package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/testenv"
)

// scanRepo is a committed Git repository whose .gitignore hides .env, the
// typical place a leaked token lands.
func scanRepo(t *testing.T) string {
	t.Helper()
	testenv.RequireDarwinOrLinux(t, "the workspace scan reads files with O_NOFOLLOW")
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolationGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	isolationGit(t, repo, "add", ".gitignore")
	isolationGit(t, repo, "commit", "-qm", "base")
	return repo
}

func writerNode(cwd, script string) string {
	return "  writer:\n    type: shell\n    config: {command: /bin/sh, args: [-c, '" + script + "'], cwd: '" + cwd + "', secret_env: {GH_TOKEN: tok}}\n"
}

// secretChecks reads the persisted secret_in_workspace Check Results.
func secretChecks(t *testing.T, store string) []events.GateCheckPayload {
	t.Helper()
	var checks []events.GateCheckPayload
	err := filepath.WalkDir(store, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || entry.Name() != "events.jsonl" {
			return walkErr
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 8<<20)
		for scanner.Scan() {
			var event events.Event
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				return err
			}
			if check, ok := event.Payload.(events.GateCheckPayload); ok && check.Kind == events.CheckSecretInWorkspace {
				checks = append(checks, check)
			}
		}
		return scanner.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return checks
}

func runWriter(t *testing.T, cwd, script string) (store, stdout, stderr string) {
	t.Helper()
	return runWriterWithValue(t, cwd, script, deliveryValue)
}

func runWriterWithValue(t *testing.T, cwd, script, value string) (store, stdout, stderr string) {
	t.Helper()
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, value)
	store = t.TempDir()
	path := writeGraph(t, secretGraphText(writerNode(cwd, script)))
	exit, out, errout := cli(t, "run", path, "--store-dir", store)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	return store, out, errout
}

func evidencePaths(check events.GateCheckPayload) map[string]string {
	paths := map[string]string{}
	for _, e := range check.Evidence {
		if e.Path != "" {
			paths[e.Path] = e.SecretName
		}
	}
	return paths
}

func TestSecretScanIntegrationShellLeaksToTrackedAndIgnoredFiles(t *testing.T) {
	repo := scanRepo(t)
	store, stdout, stderr := runWriter(t, repo, `printf %s "$GH_TOKEN" > leak.txt; printf "T=%s" "$GH_TOKEN" > .env; echo clean > other.txt`)
	checks := secretChecks(t, store)
	if len(checks) != 1 || checks[0].Verdict != events.CheckRed || checks[0].Reason != "secret_found_in_workspace" {
		t.Fatalf("checks = %+v, want one red secret_found_in_workspace", checks)
	}
	paths := evidencePaths(checks[0])
	if len(paths) != 2 || paths["leak.txt"] != "tok" || paths[".env"] != "tok" {
		t.Fatalf("evidence paths = %v, want leak.txt and the ignored .env, both naming tok", paths)
	}
	if !strings.Contains(stderr, "secret=tok") {
		t.Errorf("stderr does not name the secret:\n%s", stderr)
	}
	testenv.AssertNoSecretBytes(t, store, deliveryValue)
	testenv.AssertNoSecretText(t, "stdout", stdout, deliveryValue)
	testenv.AssertNoSecretText(t, "stderr", stderr, deliveryValue)
}

func TestSecretScanIntegrationFileNamedAfterTheValueIsRedacted(t *testing.T) {
	repo := scanRepo(t)
	const nameValue = "c47-NameCanary_9f8e7d6c5b4a"
	store, stdout, stderr := runWriterWithValue(t, repo, `printf %s "$GH_TOKEN" > "$GH_TOKEN".txt`, nameValue)
	checks := secretChecks(t, store)
	if len(checks) != 1 || checks[0].Verdict != events.CheckRed {
		t.Fatalf("checks = %+v, want one red check for the file named after the value", checks)
	}
	for _, e := range checks[0].Evidence {
		if e.SecretName != "tok" || e.Path == "" || strings.Contains(e.Path, nameValue) {
			t.Errorf("evidence = %+v, want the name kept and the value redacted from the path", e)
		}
	}
	testenv.AssertNoSecretBytes(t, store, nameValue)
	testenv.AssertNoSecretText(t, "stdout", stdout, nameValue)
	testenv.AssertNoSecretText(t, "stderr", stderr, nameValue)
}

func TestSecretScanIntegrationCleanRunIsGreen(t *testing.T) {
	repo := scanRepo(t)
	store, _, _ := runWriter(t, repo, `echo hello > note.txt`)
	checks := secretChecks(t, store)
	if len(checks) != 1 || checks[0].Verdict != events.CheckGreen || checks[0].Reason != "no_secret_in_changed_files" {
		t.Fatalf("checks = %+v, want one green no_secret_in_changed_files", checks)
	}
}

func TestSecretScanIntegrationNonGitWorkspaceIsUnobserved(t *testing.T) {
	plain, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, _, _ := runWriter(t, plain, `printf %s "$GH_TOKEN" > leak.txt`)
	checks := secretChecks(t, store)
	if len(checks) != 1 || checks[0].Verdict != events.CheckGreen || checks[0].Reason != "secret_scan_unobserved" {
		t.Fatalf("checks = %+v, want one green secret_scan_unobserved", checks)
	}
}

func TestSecretScanIntegrationSubdirectoryCwdIsUnobserved(t *testing.T) {
	repo := scanRepo(t)
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	store, _, _ := runWriter(t, sub, `printf %s "$GH_TOKEN" > leak.txt`)
	checks := secretChecks(t, store)
	if len(checks) != 1 || checks[0].Reason != "secret_scan_unobserved" {
		t.Fatalf("checks = %+v, want secret_scan_unobserved for a cwd below the repository root", checks)
	}
}

func TestSecretScanIntegrationSymlinkOutsideRootIsNotFollowed(t *testing.T) {
	repo := scanRepo(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte(deliveryValue), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _, _ := runWriter(t, repo, `ln -s "`+outside+`" link.txt`)
	checks := secretChecks(t, store)
	if len(checks) != 1 || checks[0].Verdict != events.CheckGreen {
		t.Fatalf("checks = %+v, want green: the link target outside the root must not be read", checks)
	}
}

func TestSecretScanNotRecordedForGraphWithoutSecrets(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	store := t.TempDir()
	path := writeGraph(t, corpusHead+"inputs: {}\nnodes:\n"+nodeShell("a", "")+"edges: []\noutputs: {}\n")
	if exit, out, errout := cli(t, "run", path, "--store-dir", store); exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	if checks := secretChecks(t, store); len(checks) != 0 {
		t.Fatalf("checks = %+v, want none without a grant", checks)
	}
}
