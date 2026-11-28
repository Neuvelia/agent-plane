package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"agentplane/internal/capability"
	"agentplane/internal/version"
)

func TestServeRejectsARelativeCodexBinBeforeAnnouncing(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runCommand(context.Background(), []string{"serve", "--store-dir", t.TempDir(), "--codex-bin", "codex"}, &out, &errOut)
	if code != exitUsage || out.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, out.String())
	}
	if !strings.Contains(errOut.String(), "--codex-bin must be an absolute path") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

func TestServeCapabilityCompositionReportsGitCodexThenAgentCLIs(t *testing.T) {
	r := capabilityDetector("", "").Detect(context.Background())
	if r.Version != 1 || r.Engine.Version != version.String() || r.Engine.OS != goruntime.GOOS || r.Engine.Arch != goruntime.GOARCH {
		t.Fatalf("report header %+v", r)
	}
	ids := make([]string, len(r.Tools))
	for i, tool := range r.Tools {
		ids[i] = tool.ID
	}
	if strings.Join(ids, ",") != "git,codex,claude,cursor-agent,gemini,shell,docker" {
		t.Fatalf("tools %+v", r.Tools)
	}
	for _, tool := range r.Tools[2:5] {
		if tool.Kind != "agent_cli" || (tool.Status != capability.StatusMissing && tool.Status != capability.StatusDetectedNoAdapter) {
			t.Fatalf("agent CLI row %+v", tool)
		}
	}
	posix := goruntime.GOOS == "linux" || goruntime.GOOS == "darwin"
	if want := map[bool]capability.Status{true: capability.StatusNotConfigured, false: capability.StatusUnsupportedPlatform}[posix]; r.Tools[1].Status != want {
		t.Fatalf("codex without --codex-bin: %+v", r.Tools[1])
	}
	absent := filepath.Join(t.TempDir(), "codex")
	r = capabilityDetector(absent, "").Detect(context.Background())
	if want := map[bool]capability.Status{true: capability.StatusMissing, false: capability.StatusUnsupportedPlatform}[posix]; r.Tools[1].Status != want {
		t.Fatalf("codex with an absent binary: %+v", r.Tools[1])
	}
}

func TestServeExposesTheCapabilityReport(t *testing.T) {
	s := startServe(t)
	status, body := s.call(t, "GET", "/capabilities", "")
	if status != http.StatusOK {
		t.Fatalf("GET /capabilities: %d %s", status, body)
	}
	var r capability.Report
	if err := json.Unmarshal([]byte(body), &r); err != nil || len(r.Tools) != 7 || r.Engine.Version != version.String() {
		t.Fatalf("report %s (%v)", body, err)
	}
	if status, _ := s.call(t, "POST", "/capabilities/check", ""); status != http.StatusOK {
		t.Fatalf("POST /capabilities/check: %d", status)
	}
}
