package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

func TestSelfCheckGraphIsValidAndShellOnly(t *testing.T) {
	def, err := yamlgraph.Parse([]byte(selfCheckGraph))
	if err != nil || graph.Validate(def).HasErrors() {
		t.Fatalf("embedded graph: parse %v", err)
	}
	for id, node := range def.Nodes {
		if node.Type != "shell" {
			t.Fatalf("node %s is %s; the self-check must not start an agent", id, node.Type)
		}
	}
}

func TestSelfCheckPassesAndWritesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the self-check graph needs /bin/sh")
	}
	dir := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	var out, errOut bytes.Buffer
	if code := runCommand(context.Background(), []string{"self-check"}, &out, &errOut); code != exitOK {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "self-check passed") {
		t.Fatalf("stdout %q", out.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("self-check wrote %v to %s", entries, filepath.Base(dir))
	}
}

func TestSelfCheckRejectsArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCommand(context.Background(), []string{"self-check", "x"}, &out, &errOut); code != exitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestVerifyOneRetryRequiresRejectedThenAccepted(t *testing.T) {
	attempt := func(outcome string) events.Event {
		return events.Event{Type: events.AttemptFinished, Payload: events.AttemptFinishedPayload{Outcome: outcome}}
	}
	for name, tt := range map[string]struct {
		log []events.Event
		ok  bool
	}{
		"rejected then accepted":             {[]events.Event{attempt("rejected"), attempt("accepted")}, true},
		"accepted on attempt one (no retry)": {[]events.Event{attempt("accepted")}, false},
		"no attempts":                        {nil, false},
		"two retries":                        {[]events.Event{attempt("rejected"), attempt("rejected"), attempt("accepted")}, false},
		"accepted then rejected":             {[]events.Event{attempt("accepted"), attempt("rejected")}, false},
	} {
		if err := verifyOneRetry(tt.log); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
