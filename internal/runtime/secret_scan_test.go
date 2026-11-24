package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
	"agentplane/internal/runtime"
)

// fakeScanner is a hand-written WorkspaceScanner that is also its own
// ScanWindow. It records calls and never touches the filesystem.
type fakeScanner struct {
	mu         sync.Mutex
	beginRoots []string
	scanned    [][]secrets.ScanTarget
	beginErr   error
	scanErr    error
	report     secrets.ScanReport
}

func (f *fakeScanner) Begin(_ context.Context, root string) (runtime.ScanWindow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginRoots = append(f.beginRoots, root)
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f, nil
}

func (f *fakeScanner) Scan(_ context.Context, targets []secrets.ScanTarget) (secrets.ScanReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scanned = append(f.scanned, targets)
	return f.report, f.scanErr
}

func (f *fakeScanner) counts() (begins, scans int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.beginRoots), len(f.scanned)
}

func leakReport(path string) secrets.ScanReport {
	return secrets.ScanReport{Findings: []secrets.ScanFinding{{Path: path, Secret: "gh_token"}}, Examined: 1}
}

type scanRun struct {
	result run.Result
	events []events.Event
}

func runScanned(t *testing.T, def *graph.Definition, exec runtime.Executor, scanner runtime.WorkspaceScanner) scanRun {
	t.Helper()
	return runScannedWith(t, context.Background(), def, exec, scanner)
}

func runScannedWith(t *testing.T, ctx context.Context, def *graph.Definition, exec runtime.Executor, scanner runtime.WorkspaceScanner) scanRun {
	t.Helper()
	var sink []events.Event
	options := deliveryOptions(tokenResolver{}, &sink)
	if scanner != nil {
		options.WorkspaceScanner = scanner
	}
	result, err := runtime.Run(ctx, def, nil, exec, options)
	if err != nil {
		t.Fatal(err)
	}
	return scanRun{result: result, events: sink}
}

func (s scanRun) secretChecks() []events.GateCheckPayload {
	var checks []events.GateCheckPayload
	for _, e := range s.events {
		if check, ok := e.Payload.(events.GateCheckPayload); ok && check.Kind == events.CheckSecretInWorkspace {
			checks = append(checks, check)
		}
	}
	return checks
}

// requireWorkerAndCoordinatorAgree fails when the coordinator's safety net had
// to close a window: the worker's attemptWillRetry then disagreed with accept.go.
func (s scanRun) requireWorkerAndCoordinatorAgree(t *testing.T) {
	t.Helper()
	for _, check := range s.secretChecks() {
		for _, evidence := range check.Evidence {
			if evidence.Reference != nil && evidence.Reference.Reason == "scan_not_completed" {
				t.Fatalf("a scan window was closed unscanned by the coordinator: %+v", check)
			}
		}
	}
}

func (s scanRun) indexOf(kind events.Type, predicate func(events.Event) bool) int {
	return slices.IndexFunc(s.events, func(e events.Event) bool { return e.Type == kind && predicate(e) })
}

func (s scanRun) requireNoSecretForms(t *testing.T, canaries ...string) {
	t.Helper()
	encoded, err := json.Marshal(s.events)
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, err := json.Marshal(s.result.Events)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range canaries {
		for _, form := range secrets.Forms(canary) {
			if strings.Contains(string(encoded), string(form)) || strings.Contains(string(resultJSON), string(form)) {
				t.Fatalf("events or result hold secret form %q", form)
			}
		}
	}
}

func singleGrantedShell() *graph.Definition {
	return secretGraph(map[string]graph.NodeDefinition{"a": shellNode(map[string]any{"GH_TOKEN": "gh_token"})})
}

func TestSecretInWorkspaceRedCheckNamesPathAndSecret(t *testing.T) {
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	got := runScanned(t, singleGrantedShell(), &recordingExecutor{}, scanner)
	checks := got.secretChecks()
	if len(checks) != 1 {
		t.Fatalf("secret checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if check.Verdict != events.CheckRed || check.Reason != "secret_found_in_workspace" || check.CheckID != "secret_in_workspace" {
		t.Fatalf("check = %s/%s/%s", check.CheckID, check.Verdict, check.Reason)
	}
	if evidence := check.Evidence[0]; evidence.Path != "leak.txt" || evidence.SecretName != "gh_token" {
		t.Fatalf("evidence = %+v, want path leak.txt and secret gh_token", evidence)
	}
	if err := check.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, e := range got.events {
		if _, ok := e.Payload.(events.GateCheckPayload); ok && e.AttemptID != "" {
			t.Errorf("secret check carries attempt_id %q", e.AttemptID)
		}
	}
	// Audit only until F3.9: the finding never changes the node or the run.
	if got.result.State != run.RunSucceeded || got.result.Nodes[0].State != run.NodeSucceeded {
		t.Errorf("run = %s, node = %s, want succeeded (audit)", got.result.State, got.result.Nodes[0].State)
	}
	got.requireNoSecretForms(t, deliveredCanary)
	if len(scanner.scanned) != 1 || len(scanner.scanned[0]) != 1 || scanner.scanned[0][0].Name() != "gh_token" || scanner.scanned[0][0].Value().Reveal() != deliveredCanary {
		t.Errorf("scanner targets = %v, want the granted gh_token", scanner.scanned)
	}
}

func TestSecretScanEventPrecedesNodeFinished(t *testing.T) {
	got := runScanned(t, singleGrantedShell(), &recordingExecutor{}, &fakeScanner{report: leakReport("leak.txt")})
	check := got.indexOf(events.GateCheck, func(events.Event) bool { return true })
	finished := got.indexOf(events.NodeFinished, func(e events.Event) bool { return e.NodeID == "a" })
	if check < 0 || finished < 0 || check > finished {
		t.Fatalf("gate_check at %d, node_finished at %d, want the check first", check, finished)
	}
}

func TestScanUnobservedWithoutGitEvidence(t *testing.T) {
	cases := []struct {
		name    string
		scanner runtime.WorkspaceScanner
		reason  string
	}{
		{"begin has no git evidence", &fakeScanner{beginErr: fmt.Errorf("begin: %w", secrets.ErrUnobservable)}, "git_evidence_unavailable"},
		{"scan has no git evidence", &fakeScanner{scanErr: fmt.Errorf("scan: %w", secrets.ErrUnobservable)}, "git_evidence_unavailable"},
		{"scanner fails", &fakeScanner{scanErr: errors.New("disk on fire")}, "observation_failed"},
		{"no scanner configured", nil, "scanner_not_configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runScanned(t, singleGrantedShell(), &recordingExecutor{}, tc.scanner)
			checks := got.secretChecks()
			if len(checks) != 1 {
				t.Fatalf("secret checks = %d, want 1", len(checks))
			}
			check := checks[0]
			if check.Verdict != events.CheckGreen || check.Reason != "secret_scan_unobserved" || check.Evidence[0].Reference.Reason != tc.reason {
				t.Fatalf("check = %s/%s/%+v, want green unobserved (%s)", check.Verdict, check.Reason, check.Evidence[0].Reference, tc.reason)
			}
			if got.result.State != run.RunSucceeded {
				t.Errorf("run = %s, want succeeded", got.result.State)
			}
		})
	}
}

func TestScanReportsCleanAndIncompleteResults(t *testing.T) {
	clean := runScanned(t, singleGrantedShell(), &recordingExecutor{}, &fakeScanner{report: secrets.ScanReport{Examined: 4}})
	if checks := clean.secretChecks(); len(checks) != 1 || checks[0].Reason != "no_secret_in_changed_files" || checks[0].Verdict != events.CheckGreen {
		t.Fatalf("clean checks = %+v", checks)
	}
	gaps := secrets.ScanReport{Gaps: []secrets.ScanGap{{Reason: secrets.GapFileTooLarge, Count: 1}}}
	partial := runScanned(t, singleGrantedShell(), &recordingExecutor{}, &fakeScanner{report: gaps})
	if checks := partial.secretChecks(); len(checks) != 1 || checks[0].Reason != "secret_scan_incomplete" || checks[0].Verdict != events.CheckGreen {
		t.Fatalf("incomplete checks = %+v", checks)
	}
}

func TestScanNeverEmitsValue(t *testing.T) {
	scanner := &fakeScanner{
		report:  leakReport("out/" + deliveredCanary + ".txt"),
		scanErr: nil,
	}
	got := runScanned(t, singleGrantedShell(), &recordingExecutor{}, scanner)
	checks := got.secretChecks()
	if len(checks) != 1 {
		t.Fatalf("secret checks = %d, want 1", len(checks))
	}
	if evidence := checks[0].Evidence[0]; evidence.SecretName != "gh_token" || strings.Contains(evidence.Path, deliveredCanary) || !strings.HasPrefix(evidence.Path, "out/") {
		t.Fatalf("evidence = %+v, want the name kept and the value redacted from the path", evidence)
	}
	got.requireNoSecretForms(t, deliveredCanary)
}

func TestScanNeverEmitsValueFromScannerErrors(t *testing.T) {
	scanner := &fakeScanner{beginErr: errors.New("cannot read " + deliveredCanary)}
	got := runScanned(t, singleGrantedShell(), &recordingExecutor{}, scanner)
	got.requireNoSecretForms(t, deliveredCanary)
	if len(got.secretChecks()) != 1 {
		t.Fatalf("want one unobserved check")
	}
}

func TestScanSkipsNodesWithoutGrant(t *testing.T) {
	def := secretGraph(map[string]graph.NodeDefinition{"a": shellNode(map[string]any{"GH_TOKEN": "gh_token"}), "sibling": shellNode(nil)})
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	got := runScanned(t, def, &recordingExecutor{}, scanner)
	if begins, scans := scanner.counts(); begins != 1 || scans != 1 {
		t.Fatalf("begins/scans = %d/%d, want 1/1 for the one granted node", begins, scans)
	}
	for _, e := range got.events {
		if _, ok := e.Payload.(events.GateCheckPayload); ok && e.NodeID != "a" {
			t.Errorf("node %s got a secret check without a grant", e.NodeID)
		}
	}
}

func TestScanIgnoresGraphWithoutSecrets(t *testing.T) {
	def := secretGraph(map[string]graph.NodeDefinition{"a": shellNode(nil)})
	def.Secrets = nil
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	with := runScanned(t, def, &recordingExecutor{}, scanner)
	without := runScanned(t, def, &recordingExecutor{}, nil)
	if begins, scans := scanner.counts(); begins != 0 || scans != 0 {
		t.Fatalf("begins/scans = %d/%d, want 0/0", begins, scans)
	}
	if len(with.events) != len(without.events) || len(with.secretChecks()) != 0 {
		t.Fatalf("events with scanner = %d, without = %d, secret checks = %d", len(with.events), len(without.events), len(with.secretChecks()))
	}
}

func TestScanRootComesFromNodeConfig(t *testing.T) {
	node := shellNode(map[string]any{"GH_TOKEN": "gh_token"})
	node.Config["cwd"] = "work/dir"
	scanner := &fakeScanner{}
	runScanned(t, secretGraph(map[string]graph.NodeDefinition{"a": node}), &recordingExecutor{}, scanner)
	if len(scanner.beginRoots) != 1 || scanner.beginRoots[0] != "work/dir" {
		t.Fatalf("begin roots = %v, want [work/dir]", scanner.beginRoots)
	}
	scanner = &fakeScanner{}
	runScanned(t, singleGrantedShell(), &recordingExecutor{}, scanner)
	if len(scanner.beginRoots) != 1 || scanner.beginRoots[0] != "" {
		t.Fatalf("begin roots = %v, want the empty (runner) root", scanner.beginRoots)
	}
}

func TestScanRunsOncePerNodeRunAfterFinalAttempt(t *testing.T) {
	def := secretGraph(map[string]graph.NodeDefinition{"work": loopingShellNode(3)})
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	got := runScanned(t, def, &recordingExecutor{greenAfter: 3}, scanner)
	if begins, scans := scanner.counts(); begins != 1 || scans != 1 {
		t.Fatalf("begins/scans = %d/%d across 3 attempts, want 1/1", begins, scans)
	}
	if len(got.secretChecks()) != 1 {
		t.Fatalf("secret checks = %d, want 1", len(got.secretChecks()))
	}
	got.requireWorkerAndCoordinatorAgree(t)
	finished := 0
	for _, e := range got.events {
		payload, ok := e.Payload.(events.AttemptFinishedPayload)
		if !ok {
			continue
		}
		finished++
		if payload.CheckID != "ok" {
			t.Errorf("attempt_finished.check_id = %q, want the embedded Gate check ok", payload.CheckID)
		}
	}
	if finished != 3 {
		t.Errorf("attempt_finished events = %d, want 3", finished)
	}
}

func TestScanRunsWhenLoopNeedsIntervention(t *testing.T) {
	def := secretGraph(map[string]graph.NodeDefinition{"work": loopingShellNode(2)})
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	got := runScanned(t, def, &recordingExecutor{greenAfter: 99}, scanner)
	if begins, scans := scanner.counts(); begins != 1 || scans != 1 {
		t.Fatalf("begins/scans = %d/%d, want 1/1", begins, scans)
	}
	if got.result.State != run.RunFailed || len(got.secretChecks()) != 1 {
		t.Fatalf("run = %s, secret checks = %d, want failed run with one check", got.result.State, len(got.secretChecks()))
	}
	got.requireWorkerAndCoordinatorAgree(t)
}

type failingExecutor struct{ recordingExecutor }

func (*failingExecutor) ExecuteNodeObserved(context.Context, string, graph.NodeDefinition, map[string]graph.Value, runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	return nil, nil, errors.New("shell wrote the token and then failed")
}

func TestScanRunsWhenExecutorFails(t *testing.T) {
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	got := runScanned(t, singleGrantedShell(), &failingExecutor{}, scanner)
	if _, scans := scanner.counts(); scans != 1 {
		t.Fatalf("scans = %d, want 1 for a failed node", scans)
	}
	if got.result.State != run.RunFailed || len(got.secretChecks()) != 1 {
		t.Fatalf("run = %s, secret checks = %d, want failed run with one check", got.result.State, len(got.secretChecks()))
	}
}

type cancellingExecutor struct {
	recordingExecutor
	cancel context.CancelFunc
}

func (c *cancellingExecutor) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	c.cancel()
	return c.recordingExecutor.ExecuteNodeObserved(ctx, id, n, in, o)
}

func TestScanSkippedOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scanner := &fakeScanner{report: leakReport("leak.txt")}
	got := runScannedWith(t, ctx, singleGrantedShell(), &cancellingExecutor{cancel: cancel}, scanner)
	if _, scans := scanner.counts(); scans != 0 {
		t.Fatalf("scans = %d, want 0 after cancellation", scans)
	}
	if len(got.secretChecks()) != 0 {
		t.Fatalf("secret checks = %d, want none after cancellation", len(got.secretChecks()))
	}
}
