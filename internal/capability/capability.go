// Package capability reports what this machine can run (F2.11, ADR-0025): the
// engine version, the Git binary the engine uses, and each agent adapter the
// engine is composed with. Detection is read-only and bounded. Nothing it learns
// leaves the report except fixed status and reason codes and a version string
// that matches a strict pattern: no path, environment value or raw tool output.
package capability

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/gitlocate"
)

// ReportVersion is the version of the Report JSON contract.
const ReportVersion = 1

type Status string

const (
	StatusAvailable           Status = "available"
	StatusMissing             Status = "missing"
	StatusNotConfigured       Status = "not_configured"
	StatusUnsupportedPlatform Status = "unsupported_platform"
	StatusUnsupportedVersion  Status = "unsupported_version"
	StatusError               Status = "error"
)

// Reason codes are a closed set; a new one needs a spec change.
const (
	ReasonNone                  = ""
	ReasonNoBinaryConfigured    = "no_binary_configured"
	ReasonNotExecutable         = "not_executable"
	ReasonProbeFailed           = "probe_failed"
	ReasonProbeTimeout          = "probe_timeout"
	ReasonOutputTooLarge        = "output_too_large"
	ReasonUnrecognizedVersion   = "unrecognized_version"
	ReasonMissingRequiredFlags  = "missing_required_flags"
	ReasonPlatformNotSupported  = "platform_not_supported"
	ReasonDeveloperToolsMissing = "developer_tools_missing"
)

// Reasons lists every reason code, for tests and clients.
var Reasons = []string{ReasonNone, ReasonNoBinaryConfigured, ReasonNotExecutable, ReasonProbeFailed, ReasonProbeTimeout, ReasonOutputTooLarge, ReasonUnrecognizedVersion, ReasonMissingRequiredFlags, ReasonPlatformNotSupported, ReasonDeveloperToolsMissing}

type Engine struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

type Tool struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // "tool" or "agent_adapter"
	Status  Status `json:"status"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

type Report struct {
	Version   int       `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
	Engine    Engine    `json:"engine"`
	Tools     []Tool    `json:"tools"`
}

// Agent is one adapter the engine is composed with. A nil Adapter means the
// adapter exists but has no binary configured; it is reported without probing.
type Agent struct {
	ID      string
	Adapter agent.AgentAdapter
}

// Detector holds everything detection depends on, so tests substitute fakes.
type Detector struct {
	EngineVersion string
	GOOS, GOARCH  string // default: the running platform
	GitPath       string // default: git resolved from PATH
	Agents        []Agent
	Timeout       time.Duration // per Git probe; default 10s
	Now           func() time.Time
	// Stat and Readlink inspect the filesystem without executing anything, so
	// the macOS developer-tools decision is testable on any OS. Defaults:
	// os.Stat and os.Readlink.
	Stat     func(string) (os.FileInfo, error)
	Readlink func(string) (string, error)
}

const maxProbeOutput = 4 << 10

var (
	gitVersionPattern = regexp.MustCompile(`^git version ([0-9][0-9A-Za-z.+-]*)`)
	safeVersion       = regexp.MustCompile(`^[0-9A-Za-z.+-]{1,64}$`)
)

// Detect runs Git and adapter detection concurrently and never fails: every
// problem becomes a status and reason on its own row.
func (d Detector) Detect(ctx context.Context) Report {
	goos, goarch := d.GOOS, d.GOARCH
	if goos == "" {
		goos = goruntime.GOOS
	}
	if goarch == "" {
		goarch = goruntime.GOARCH
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	agents := append([]Agent(nil), d.Agents...)
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })
	tools := make([]Tool, 1+len(agents))
	var wg sync.WaitGroup
	run := func(i int, id, kind string, probe func() Tool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if recover() != nil {
					tools[i] = Tool{ID: id, Kind: kind, Status: StatusError, Reason: ReasonProbeFailed}
				}
			}()
			tools[i] = probe()
		}()
	}
	run(0, "git", "tool", func() Tool { return d.git(ctx, goos) })
	for i, a := range agents {
		if !posix(goos) {
			// Every adapter today needs Darwin/Linux process ownership (ADR-0010),
			// so no probe process is started elsewhere.
			tools[i+1] = Tool{ID: a.ID, Kind: "agent_adapter", Status: StatusUnsupportedPlatform, Reason: ReasonPlatformNotSupported}
			continue
		}
		run(i+1, a.ID, "agent_adapter", func() Tool { return adapterTool(ctx, a) })
	}
	wg.Wait()
	return Report{
		Version:   ReportVersion,
		CheckedAt: now().UTC().Truncate(time.Millisecond),
		Engine:    Engine{Version: d.EngineVersion, OS: goos, Arch: goarch},
		Tools:     tools,
	}
}

func posix(goos string) bool { return goos == "linux" || goos == "darwin" }

func (d Detector) git(ctx context.Context, goos string) Tool {
	t := Tool{ID: "git", Kind: "tool"}
	if !posix(goos) {
		t.Status, t.Reason = StatusUnsupportedPlatform, ReasonPlatformNotSupported
		return t
	}
	loc := gitlocate.Locator{GOOS: goos, Stat: d.Stat, Readlink: d.Readlink}
	path := d.GitPath
	if path == "" {
		var err error
		path, err = loc.Resolve()
		switch {
		case errors.Is(err, gitlocate.ErrDeveloperToolsMissing):
			t.Status, t.Reason = StatusMissing, ReasonDeveloperToolsMissing
			return t
		case err != nil:
			t.Status = StatusMissing
			return t
		}
	} else {
		// An explicit path stands in for the resolved git, so it is also the
		// path the macOS shim guard applies to.
		loc.Shim = path
	}
	stat := d.Stat
	if stat == nil {
		stat = os.Stat
	}
	info, err := stat(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Status = StatusMissing
		return t
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		t.Status, t.Reason = StatusError, ReasonNotExecutable
		return t
	}
	if errors.Is(loc.Check(path), gitlocate.ErrDeveloperToolsMissing) {
		t.Status, t.Reason = StatusMissing, ReasonDeveloperToolsMissing
		return t
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	out, reason := probe(ctx, timeout, path, "--version")
	if reason != ReasonNone {
		t.Status, t.Reason = StatusError, reason
		return t
	}
	m := gitVersionPattern.FindStringSubmatch(out)
	if m == nil || !safeVersion.MatchString(m[1]) {
		t.Status, t.Reason = StatusError, ReasonUnrecognizedVersion
		return t
	}
	t.Status, t.Version = StatusAvailable, m[1]
	return t
}

// probe runs one bounded process: structured argv, a minimal explicit
// environment (no PATH, HOME or credentials), stdin closed, a working directory
// outside any repository, capped output, and termination on timeout.
func probe(ctx context.Context, timeout time.Duration, path string, args ...string) (string, string) {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(pctx, path, args...)
	cmd.Env = []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C"}
	cmd.Dir = os.TempDir()
	cmd.Stdin = nil
	out := &capped{limit: maxProbeOutput}
	cmd.Stdout = out
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		// The caller (the server) stopped the probe; it did not time out.
		return "", ReasonProbeFailed
	case pctx.Err() != nil:
		return "", ReasonProbeTimeout
	case out.overflow:
		return "", ReasonOutputTooLarge
	case err != nil:
		return "", ReasonProbeFailed
	}
	return out.String(), ReasonNone
}

type capped struct {
	strings.Builder
	limit    int
	overflow bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if space := c.limit - c.Len(); len(p) > space {
		c.overflow = true
		p = p[:space]
	}
	c.Builder.Write(p)
	return n, nil
}

// adapterTool maps an adapter's existing Detect result (ADR-0009) onto the
// report. Error text is never copied; only the error kind selects a reason.
func adapterTool(ctx context.Context, a Agent) Tool {
	t := Tool{ID: a.ID, Kind: "agent_adapter"}
	if a.Adapter == nil {
		t.Status, t.Reason = StatusNotConfigured, ReasonNoBinaryConfigured
		return t
	}
	report, err := a.Adapter.Detect(ctx)
	if err != nil {
		var execErr *agent.ExecutionError
		kind := ""
		if errors.As(err, &execErr) {
			kind = string(execErr.Kind)
		}
		switch {
		case strings.HasSuffix(kind, "unsupported_platform"):
			t.Status, t.Reason = StatusUnsupportedPlatform, ReasonPlatformNotSupported
		case strings.HasSuffix(kind, "unsupported_capability"):
			t.Status, t.Reason = StatusUnsupportedVersion, ReasonMissingRequiredFlags
		default:
			t.Status, t.Reason = StatusError, ReasonProbeFailed
		}
		return t
	}
	if !report.Available {
		t.Status = StatusMissing
		return t
	}
	if report.Version == nil || !safeVersion.MatchString(*report.Version) {
		t.Status, t.Reason = StatusError, ReasonUnrecognizedVersion
		return t
	}
	t.Status, t.Version = StatusAvailable, *report.Version
	return t
}

// Service keeps the latest report in memory and runs at most one detection at
// a time; callers that arrive while one runs share its result.
type Service struct {
	Detect func(context.Context) Report

	mu      sync.Mutex
	last    *Report
	running chan struct{}
	joined  int // callers that joined a running detection; observed by tests
}

// Joined reports how many callers joined an already running detection, so
// tests can release a detection only after every caller is waiting on it.
func (s *Service) Joined() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.joined
}

// Latest returns the last report, detecting first if there is none.
func (s *Service) Latest(detectCtx, waitCtx context.Context) (Report, error) {
	s.mu.Lock()
	if s.last != nil {
		r := *s.last
		s.mu.Unlock()
		return r, nil
	}
	s.mu.Unlock()
	return s.Check(detectCtx, waitCtx)
}

// Check runs detection, or joins the one already running. detectCtx bounds the
// shared detection (it outlives a single client); waitCtx ends only this wait.
func (s *Service) Check(detectCtx, waitCtx context.Context) (Report, error) {
	s.mu.Lock()
	done := s.running
	if done != nil {
		s.joined++
	} else {
		done = make(chan struct{})
		s.running = done
		go func() {
			// If Detect panics, r keeps this error report, and the deferred
			// cleanup still clears running and closes done, so neither the
			// process nor later checks are affected.
			r := Report{Version: ReportVersion, Tools: []Tool{{ID: "detection", Kind: "tool", Status: StatusError, Reason: ReasonProbeFailed}}}
			defer func() {
				if recover() != nil {
					r.CheckedAt = time.Now().UTC().Truncate(time.Millisecond)
				}
				s.mu.Lock()
				s.last = &r
				s.running = nil
				s.mu.Unlock()
				close(done)
			}()
			r = s.Detect(detectCtx)
		}()
	}
	s.mu.Unlock()
	select {
	case <-done:
	case <-waitCtx.Done():
		return Report{}, waitCtx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.last, nil
}
