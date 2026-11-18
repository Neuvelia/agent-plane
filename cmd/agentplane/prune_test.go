package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/store"
)

func pruneRunID(n int) string { return fmt.Sprintf("%08x-0000-4000-8000-000000000000", n) }

// finishedStoredRun writes a finished run that started `days` ago.
func finishedStoredRun(t *testing.T, dir string, n, days int) string {
	t.Helper()
	id := pruneRunID(n)
	at := time.Now().UTC().AddDate(0, 0, -days)
	event := func(seq uint64, typ events.Type, p events.Payload) events.Event {
		return events.Event{SchemaVersion: events.SchemaVersion, Type: typ, Timestamp: at, Sequence: seq, RunID: events.RunID(id), GraphID: "g", GraphVersion: "sha256-v0:x", Payload: p}
	}
	w := store.Store{Dir: dir}.NewWriter([]byte("schema_version: 0\n"))
	for _, e := range []events.Event{event(1, events.RunStarted, events.RunStartedPayload{NodeCount: 1}), event(2, events.RunFinished, events.RunFinishedPayload{Status: "succeeded"})} {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return id
}

func configureRetention(t *testing.T, dir string, limits store.Retention) {
	t.Helper()
	data, err := json.Marshal(store.Format{StoreFormat: store.CurrentFormat, Retention: &limits})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runOnDisk(dir, id string) bool {
	_, err := os.Stat(filepath.Join(dir, id))
	return err == nil
}

func TestRunsPruneCommand(t *testing.T) {
	dir := t.TempDir()
	old, recent := finishedStoredRun(t, dir, 1, 90), finishedStoredRun(t, dir, 2, 1)
	configureRetention(t, dir, store.Retention{MaxAgeDays: 30})

	code, out, _ := cli(t, "runs", "prune", "--store-dir", dir, "--dry-run")
	if code != 0 || !strings.Contains(out, "Would remove 1 run(s)") || !strings.Contains(out, old) || !runOnDisk(dir, old) {
		t.Fatalf("dry run: code=%d out=%q", code, out)
	}
	code, out, _ = cli(t, "runs", "prune", "--store-dir", dir)
	if code != 0 || !strings.Contains(out, "Removed 1 run(s)") || !strings.Contains(out, old) || runOnDisk(dir, old) || !runOnDisk(dir, recent) {
		t.Fatalf("prune: code=%d out=%q", code, out)
	}
	if code, out, _ = cli(t, "runs", "prune", "--store-dir", dir); code != 0 || !strings.Contains(out, "Nothing to prune") {
		t.Fatalf("second prune: code=%d out=%q", code, out)
	}
}

func TestRunsPruneJSONListsTheRemovedRunIDs(t *testing.T) {
	dir := t.TempDir()
	old := finishedStoredRun(t, dir, 1, 90)
	configureRetention(t, dir, store.Retention{MaxAgeDays: 30})
	var doc struct {
		Status string `json:"status"`
		Prune  struct {
			DryRun  bool     `json:"dry_run"`
			Removed []string `json:"removed"`
			Skipped []string `json:"skipped"`
			Failed  []any    `json:"failed"`
		} `json:"prune"`
	}
	for _, dry := range []bool{true, false} {
		args := []string{"runs", "prune", "--store-dir", dir, "--json"}
		if dry {
			args = append(args, "--dry-run")
		}
		code, out, _ := cli(t, args...)
		if err := json.Unmarshal([]byte(out), &doc); err != nil || code != 0 || doc.Status != "ok" || doc.Prune.DryRun != dry || len(doc.Prune.Removed) != 1 || doc.Prune.Removed[0] != old || doc.Prune.Skipped == nil || doc.Prune.Failed == nil {
			t.Fatalf("dry=%v code=%d out=%s err=%v", dry, code, out, err)
		}
	}
	if runOnDisk(dir, old) {
		t.Fatal("the second call was not a dry run and must have removed the run")
	}
}

func TestRunsPruneWithoutRetentionDoesNothing(t *testing.T) {
	dir := t.TempDir()
	id := finishedStoredRun(t, dir, 1, 900)
	code, out, _ := cli(t, "runs", "prune", "--store-dir", dir)
	if code != 0 || !strings.Contains(out, "Nothing to prune") || !runOnDisk(dir, id) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestRunsPruneUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{{"runs", "prune", "--nope"}, {"runs", "prune", "extra"}} {
		if code, _, _ := cli(t, args...); code != exitUsage {
			t.Fatalf("%v exit %d, want %d", args, code, exitUsage)
		}
	}
}

func TestRunsPruneExitsOneWhenADeletionFails(t *testing.T) {
	dir := t.TempDir()
	broken, fine := finishedStoredRun(t, dir, 1, 90), finishedStoredRun(t, dir, 2, 80)
	// run.lock as a directory makes taking the run's lock fail for a reason other
	// than a live writer, on every platform and for root as well.
	lock := filepath.Join(dir, broken, "run.lock")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	configureRetention(t, dir, store.Retention{MaxAgeDays: 30})
	code, out, errOut := cli(t, "runs", "prune", "--store-dir", dir)
	if code != exitInternal || !strings.Contains(out, "Failed "+broken) || !strings.Contains(errOut, "error:") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if runOnDisk(dir, fine) || !runOnDisk(dir, broken) {
		t.Fatal("the failing run stays and the others are still removed")
	}
}

func TestServeStartPrunesOnlyWhenRetentionIsConfigured(t *testing.T) {
	configured, plain := t.TempDir(), t.TempDir()
	oldConfigured, oldPlain := finishedStoredRun(t, configured, 1, 90), finishedStoredRun(t, plain, 1, 90)
	configureRetention(t, configured, store.Retention{MaxAgeDays: 30})
	startServe(t, "--store-dir", configured)
	startServe(t, "--store-dir", plain)
	if runOnDisk(configured, oldConfigured) {
		t.Fatal("serve must apply the configured retention at start")
	}
	if !runOnDisk(plain, oldPlain) {
		t.Fatal("serve must not prune a store that configures no retention")
	}
}
