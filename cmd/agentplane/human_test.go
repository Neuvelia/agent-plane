package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/events"
)

func demoGraphPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, ".agents", "graphs", "demo-vertical.yaml")
}

func runDemo(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errout bytes.Buffer
	code := runCommand(context.Background(), append([]string{"run", demoGraphPath(t), "--json"}, args...), &out, &errout)
	return code, out.String(), errout.String()
}

func TestDemoVerticalApproveRunsScopedRetryAndApply(t *testing.T) {
	code, out, errout := runDemo(t, "--approve", "approve")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errout)
	}
	var doc struct {
		Events []events.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	outcomes := attemptsByNode(doc.Events)
	if strings.Join(outcomes["flaky"], ",") != "rejected,accepted" || len(outcomes["fast"]) != 0 {
		t.Fatalf("outcomes %v", outcomes)
	}
	requested, decided := 0, 0
	for _, e := range doc.Events {
		switch e.Type {
		case events.ApprovalRequested:
			requested++
		case events.ApprovalDecided:
			decided++
			if e.Payload.(events.ApprovalDecidedPayload).Decision != "approved" {
				t.Fatal("expected approved")
			}
		}
	}
	if requested != 1 || decided != 1 {
		t.Fatalf("requested=%d decided=%d", requested, decided)
	}
}

func TestDemoVerticalRejectExitsFourAndSkipsApply(t *testing.T) {
	code, out, _ := runDemo(t, "--reject", "approve")
	if code != 4 {
		t.Fatalf("code=%d", code)
	}
	var doc struct {
		Failure struct {
			Kind string `json:"kind"`
		} `json:"failure"`
		Nodes []struct {
			NodeID string `json:"node_id"`
			Status string `json:"status"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Failure.Kind != "rejected" {
		t.Fatalf("failure kind %q", doc.Failure.Kind)
	}
	for _, n := range doc.Nodes {
		if n.NodeID == "apply" && n.Status != "skipped" {
			t.Fatalf("apply=%s", n.Status)
		}
	}
}

func TestDemoVerticalWithoutDecisionSourceFailsPreflightBeforeAnyNodeRuns(t *testing.T) {
	code, out, _ := runDemo(t)
	if code != 3 || !strings.Contains(out, "E_HUMAN_NO_PROVIDER") || !strings.Contains(out, `"approve"`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if strings.Contains(out, "fast branch done") || strings.Contains(out, `"type":"node_started"`) {
		t.Fatal("no node may execute when preflight fails")
	}
}

func TestApproveFlagMustNameAHumanNode(t *testing.T) {
	for _, flags := range [][]string{{"--approve", "fast"}, {"--approve", "approve", "--reject", "approve"}, {"--approve", "nope"}} {
		if code, _, _ := runDemo(t, flags...); code != 2 {
			t.Fatalf("flags %v: code=%d, want usage error 2", flags, code)
		}
	}
}
