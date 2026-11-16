package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	goruntime "runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentplane/internal/capability"
)

func fixedReport(n int32) capability.Report {
	return capability.Report{
		Version:   capability.ReportVersion,
		CheckedAt: time.Unix(int64(n), 0).UTC(),
		Engine:    capability.Engine{Version: "0.0.0-dev", OS: "linux", Arch: "amd64"},
		Tools: []capability.Tool{
			{ID: "git", Kind: "tool", Status: capability.StatusAvailable, Version: "2.43.0"},
			{ID: "codex", Kind: "agent_adapter", Status: capability.StatusNotConfigured, Reason: capability.ReasonNoBinaryConfigured},
		},
	}
}

func TestCapabilityRoutesRequireTheBearerToken(t *testing.T) {
	s, ts := newServer(t, nil)
	var calls atomic.Int32
	s.Capabilities = &capability.Service{Detect: func(context.Context) capability.Report { return fixedReport(calls.Add(1)) }}
	for _, c := range [][2]string{{"GET", "/capabilities"}, {"POST", "/capabilities/check"}} {
		if resp, _ := do(t, ts, c[0], c[1], "", false); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s without token: %d", c[0], c[1], resp.StatusCode)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("an unauthenticated request ran detection")
	}
}

func TestCapabilitiesAreDetectedOnceThenServedFromMemory(t *testing.T) {
	s, ts := newServer(t, nil)
	var calls atomic.Int32
	s.Capabilities = &capability.Service{Detect: func(context.Context) capability.Report { return fixedReport(calls.Add(1)) }}
	before, _ := os.ReadDir(s.Store.Dir)
	for i := 0; i < 3; i++ {
		resp, body := do(t, ts, "GET", "/capabilities", "", true)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("GET: %d %v", resp.StatusCode, resp.Header)
		}
		var r capability.Report
		if err := json.Unmarshal([]byte(body), &r); err != nil || r.Version != 1 || len(r.Tools) != 2 || r.Tools[0].ID != "git" || r.Tools[1].ID != "codex" {
			t.Fatalf("report %s (%v)", body, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d detections for three GETs, want 1", calls.Load())
	}
	for _, body := range []string{"", "{}", " {} "} {
		if resp, _ := do(t, ts, "POST", "/capabilities/check", body, true); resp.StatusCode != http.StatusOK {
			t.Fatalf("check with body %q: %d", body, resp.StatusCode)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("%d detections after three checks, want 4", calls.Load())
	}
	_, body := do(t, ts, "GET", "/capabilities", "", true)
	var r capability.Report
	_ = json.Unmarshal([]byte(body), &r)
	if !r.CheckedAt.Equal(time.Unix(4, 0)) {
		t.Fatalf("GET after check returned %v, want the latest report", r.CheckedAt)
	}
	// Detection is not an execution transition: no run, no store entry.
	after, _ := os.ReadDir(s.Store.Dir)
	if len(before) != len(after) {
		t.Fatalf("store changed: %d -> %d entries", len(before), len(after))
	}
	if resp, body := do(t, ts, "GET", "/runs", "", true); resp.StatusCode != http.StatusOK || body != "{\"runs\":[]}\n" {
		t.Fatalf("runs after detection: %s", body)
	}
}

func TestCapabilityCheckRejectsABodyOtherThanAnEmptyObject(t *testing.T) {
	s, ts := newServer(t, nil)
	var calls atomic.Int32
	s.Capabilities = &capability.Service{Detect: func(context.Context) capability.Report { return fixedReport(calls.Add(1)) }}
	for _, body := range []string{`{"refresh":true}`, `[]`, `{} {}`, `nope`} {
		if resp, _ := do(t, ts, "POST", "/capabilities/check", body, true); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %q: %d", body, resp.StatusCode)
		}
	}
	if resp, _ := do(t, ts, "GET", "/capabilities/check", "", true); resp.StatusCode == http.StatusOK {
		t.Fatal("GET /capabilities/check must not be a route")
	}
	if resp, _ := do(t, ts, "POST", "/capabilities", "", true); resp.StatusCode == http.StatusOK {
		t.Fatal("POST /capabilities must not be a route")
	}
	if calls.Load() != 0 {
		t.Fatalf("a refused request ran detection")
	}
}

func TestCapabilitiesWithoutDetectionAre503(t *testing.T) {
	_, ts := newServer(t, nil)
	for _, c := range [][2]string{{"GET", "/capabilities"}, {"POST", "/capabilities/check"}} {
		if resp, _ := do(t, ts, c[0], c[1], "", true); resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: %d", c[0], c[1], resp.StatusCode)
		}
	}
}

func TestConcurrentCapabilityChecksShareOneDetection(t *testing.T) {
	s, ts := newServer(t, nil)
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	s.Capabilities = &capability.Service{Detect: func(context.Context) capability.Report {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-release
		}
		return fixedReport(n)
	}}
	bodies := make([]string, 10)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, bodies[0] = do(t, ts, "POST", "/capabilities/check", "", true) }()
	<-entered
	for i := 1; i < len(bodies); i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, bodies[i] = do(t, ts, "POST", "/capabilities/check", "", true) }()
	}
	waitForJoiners(t, s.Capabilities, len(bodies)-1)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("%d detections, want 1", calls.Load())
	}
	if slices.ContainsFunc(bodies, func(b string) bool { return b != bodies[0] }) {
		t.Fatalf("checks returned different reports: %q", bodies)
	}
}

// waitForJoiners yields until n requests joined the running detection.
func waitForJoiners(t *testing.T, svc *capability.Service, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for svc.Joined() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d requests joined, want %d", svc.Joined(), n)
		}
		goruntime.Gosched()
	}
}
