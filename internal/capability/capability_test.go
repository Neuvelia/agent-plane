package capability

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/agent/adaptertest"
	"agentplane/internal/agent/codex"
)

// absent is a Git path that never exists, so tests about adapters start no
// Git process.
const absent = "/nonexistent/agentplane-capability-test/git"

func requirePOSIX(t *testing.T) {
	t.Helper()
	if goruntime.GOOS != "linux" && goruntime.GOOS != "darwin" {
		t.Skip("fake binaries are POSIX shell scripts")
	}
}

// fakeBinary writes an executable shell script and returns its path.
func fakeBinary(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitRow(t *testing.T, d Detector) Tool {
	t.Helper()
	if d.GOOS == "" {
		d.GOOS = "linux"
	}
	r := d.Detect(context.Background())
	if r.Tools[0].ID != "git" || r.Tools[0].Kind != "tool" {
		t.Fatalf("first row %+v", r.Tools[0])
	}
	return r.Tools[0]
}

func TestGitProbeOutcomes(t *testing.T) {
	requirePOSIX(t)
	notExec := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 30 bytes x 300 = 9000 bytes using builtins only: the probe has no PATH.
	big := `i=0; while [ $i -lt 300 ]; do printf '%s' 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; i=$((i+1)); done`
	cases := []struct {
		name string
		path string
		want Tool
	}{
		{"available", fakeBinary(t, `printf 'git version 2.43.0\n'`), Tool{Status: StatusAvailable, Version: "2.43.0"}},
		{"missing", filepath.Join(t.TempDir(), "absent"), Tool{Status: StatusMissing}},
		{"not executable", notExec, Tool{Status: StatusError, Reason: ReasonNotExecutable}},
		{"directory", t.TempDir(), Tool{Status: StatusError, Reason: ReasonNotExecutable}},
		{"exit status", fakeBinary(t, `exit 3`), Tool{Status: StatusError, Reason: ReasonProbeFailed}},
		{"too large", fakeBinary(t, big), Tool{Status: StatusError, Reason: ReasonOutputTooLarge}},
		{"unrecognized", fakeBinary(t, `printf 'hello\n'`), Tool{Status: StatusError, Reason: ReasonUnrecognizedVersion}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := gitRow(t, Detector{GitPath: c.path})
			if got.Status != c.want.Status || got.Version != c.want.Version || got.Reason != c.want.Reason {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestGitProbeTimesOutAndTerminatesTheProcess(t *testing.T) {
	requirePOSIX(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := fakeBinary(t, `printf '%s' "$$" > `+pidFile+`; exec /bin/sleep 30`)
	start := time.Now()
	got := gitRow(t, Detector{GitPath: bin, Timeout: 300 * time.Millisecond})
	if got.Status != StatusError || got.Reason != ReasonProbeTimeout {
		t.Fatalf("got %+v", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("probe took %v", elapsed)
	}
	pid, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	// kill -0 fails once the process is gone (and reaped by the probe).
	if err := exec.Command("/bin/kill", "-0", string(pid)).Run(); err == nil {
		t.Fatalf("probe process %s still exists after timeout", pid)
	}
}

func TestGitProbeUsesExactArgvMinimalEnvironmentAndNoRepository(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("reads the initial environment from /proc")
	}
	rec := t.TempDir()
	bin := fakeBinary(t, `printf '%s\n' "$@" > `+rec+`/argv; /bin/cat /proc/$$/environ > `+rec+`/env; pwd > `+rec+`/cwd; printf 'git version 2.1.0\n'`)
	if got := gitRow(t, Detector{GitPath: bin}); got.Status != StatusAvailable {
		t.Fatalf("got %+v", got)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(rec, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if argv := read("argv"); argv != "--version\n" {
		t.Fatalf("argv %q", argv)
	}
	env := strings.Split(strings.TrimRight(read("env"), "\x00"), "\x00")
	slices.Sort(env)
	if want := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "LC_ALL=C"}; !slices.Equal(env, want) {
		t.Fatalf("environment %q, want %q", env, want)
	}
	cwd := strings.TrimSpace(read("cwd"))
	repo, _ := filepath.Abs("../..")
	if strings.HasPrefix(cwd, repo) {
		t.Fatalf("probe ran inside the repository: %s", cwd)
	}
}

func TestUnsupportedPlatformStartsNoProcess(t *testing.T) {
	requirePOSIX(t)
	marker := filepath.Join(t.TempDir(), "ran")
	bin := fakeBinary(t, `: > `+marker+`; printf 'git version 2.43.0\n'`)
	r := Detector{GOOS: "windows", GitPath: bin, Agents: []Agent{{ID: "codex", Adapter: codex.Adapter{Binary: bin}}}}.Detect(context.Background())
	for _, tool := range r.Tools {
		if tool.Status != StatusUnsupportedPlatform || tool.Reason != ReasonPlatformNotSupported {
			t.Fatalf("row %+v", tool)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a probe process ran on an unsupported platform")
	}
	if r.Engine.OS != "windows" {
		t.Fatalf("engine %+v", r.Engine)
	}
}

type panicAdapter struct{ adaptertest.MockAdapter }

func (*panicAdapter) Detect(context.Context) (agent.CapabilityReport, error) { panic("boom") }

type countingAdapter struct {
	adaptertest.MockAdapter
	calls atomic.Int32
}

func (c *countingAdapter) Detect(ctx context.Context) (agent.CapabilityReport, error) {
	c.calls.Add(1)
	return c.MockAdapter.Detect(ctx)
}

func TestAdapterDetectMapping(t *testing.T) {
	version := func(s string) *string { return &s }
	execErr := func(kind string) error {
		return &agent.ExecutionError{AdapterID: "codex", Kind: agent.ErrorKind(kind), Cause: errors.New("secret detail /home/x")}
	}
	cases := []struct {
		name    string
		adapter agent.AgentAdapter
		want    Tool
	}{
		{"not configured", nil, Tool{Status: StatusNotConfigured, Reason: ReasonNoBinaryConfigured}},
		{"available", &adaptertest.MockAdapter{Report: agent.CapabilityReport{Available: true, Version: version("0.163.0")}}, Tool{Status: StatusAvailable, Version: "0.163.0"}},
		{"missing", &adaptertest.MockAdapter{}, Tool{Status: StatusMissing}},
		{"platform", &adaptertest.MockAdapter{DetectErr: execErr("codex_unsupported_platform")}, Tool{Status: StatusUnsupportedPlatform, Reason: ReasonPlatformNotSupported}},
		{"flags", &adaptertest.MockAdapter{DetectErr: execErr("codex_unsupported_capability")}, Tool{Status: StatusUnsupportedVersion, Reason: ReasonMissingRequiredFlags}},
		{"detection", &adaptertest.MockAdapter{DetectErr: execErr("codex_detection")}, Tool{Status: StatusError, Reason: ReasonProbeFailed}},
		{"plain error", &adaptertest.MockAdapter{DetectErr: errors.New("x")}, Tool{Status: StatusError, Reason: ReasonProbeFailed}},
		{"unsafe version", &adaptertest.MockAdapter{Report: agent.CapabilityReport{Available: true, Version: version("1.0 /home/me")}}, Tool{Status: StatusError, Reason: ReasonUnrecognizedVersion}},
		{"no version", &adaptertest.MockAdapter{Report: agent.CapabilityReport{Available: true}}, Tool{Status: StatusError, Reason: ReasonUnrecognizedVersion}},
		{"panic", &panicAdapter{}, Tool{Status: StatusError, Reason: ReasonProbeFailed}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Detector{GOOS: "linux", GitPath: absent, Agents: []Agent{{ID: "codex", Adapter: c.adapter}}}.Detect(context.Background())
			got := r.Tools[1]
			if got.ID != "codex" || got.Kind != "agent_adapter" || got.Status != c.want.Status || got.Version != c.want.Version || got.Reason != c.want.Reason {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestUnconfiguredAdapterIsNeverDetected(t *testing.T) {
	counting := &countingAdapter{}
	r := Detector{GOOS: "linux", GitPath: absent, Agents: []Agent{{ID: "codex"}, {ID: "zz", Adapter: counting}}}.Detect(context.Background())
	if ids := []string{r.Tools[0].ID, r.Tools[1].ID, r.Tools[2].ID}; !slices.Equal(ids, []string{"git", "codex", "zz"}) {
		t.Fatalf("order %v", ids)
	}
	if counting.calls.Load() != 1 || r.Tools[1].Status != StatusNotConfigured {
		t.Fatalf("calls %d, codex %+v", counting.calls.Load(), r.Tools[1])
	}
}

func TestRealCodexAdapterWithAbsentBinaryIsMissing(t *testing.T) {
	requirePOSIX(t)
	r := Detector{Agents: []Agent{{ID: "codex", Adapter: codex.Adapter{Binary: filepath.Join(t.TempDir(), "codex")}}}}.Detect(context.Background())
	if got := r.Tools[1]; got.Status != StatusMissing {
		t.Fatalf("got %+v", got)
	}
}

func TestReportCarriesNoPathEnvironmentOrRawOutput(t *testing.T) {
	requirePOSIX(t)
	const marker = "LEAKMARKER"
	t.Setenv("AP_CAPABILITY_TEST_SECRET", marker+"-env")
	dir := filepath.Join(t.TempDir(), marker)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitBin := filepath.Join(dir, "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nprintf 'git version 2.5.0 "+marker+"\\n'; printf '"+marker+"' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	failing := filepath.Join(dir, "codex")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nprintf 'codex-cli "+marker+"\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := Detector{GitPath: gitBin, Agents: []Agent{{ID: "codex", Adapter: codex.Adapter{Binary: failing}}}}.Detect(context.Background())
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), marker) || strings.Contains(string(data), dir) {
		t.Fatalf("report leaks probe data: %s", data)
	}
	if r.Tools[0].Version != "2.5.0" {
		t.Fatalf("git row %+v", r.Tools[0])
	}
	for _, tool := range r.Tools {
		if !slices.Contains(Reasons, tool.Reason) {
			t.Fatalf("reason %q outside the closed set", tool.Reason)
		}
	}
}

func TestServiceRunsOneDetectionForConcurrentChecks(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	entered := make(chan struct{})
	s := &Service{Detect: func(context.Context) Report {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return Report{Version: ReportVersion, CheckedAt: time.Unix(int64(calls.Load()), 0).UTC()}
	}}
	ctx := context.Background()
	first := make(chan Report, 1)
	go func() { r, _ := s.Check(ctx, ctx); first <- r }()
	<-entered
	var wg sync.WaitGroup
	results := make([]Report, 9)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i], _ = s.Check(ctx, ctx) }()
	}
	// Release only once all nine joined the running detection.
	waitJoined(t, s, len(results))
	close(release)
	wg.Wait()
	want := <-first
	for _, r := range results {
		if !r.CheckedAt.Equal(want.CheckedAt) {
			t.Fatalf("joined check returned a different report")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d detections, want 1", calls.Load())
	}
	if r, _ := s.Latest(ctx, ctx); calls.Load() != 1 || !r.CheckedAt.Equal(want.CheckedAt) {
		t.Fatalf("Latest detected again")
	}
}

// waitJoined yields until n callers have joined the running detection.
func waitJoined(t *testing.T, s *Service, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		joined := s.joined
		s.mu.Unlock()
		if joined == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d callers joined, want %d", joined, n)
		}
		goruntime.Gosched()
	}
}

func TestServiceWaitEndsWithTheCallerContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	s := &Service{Detect: func(context.Context) Report { <-release; return Report{} }}
	wait, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Check(context.Background(), wait); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestPackageHasNoNetworkDependency(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not on PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "net" || strings.HasPrefix(dep, "net/") {
			t.Fatalf("capability depends on %s", dep)
		}
	}
}
