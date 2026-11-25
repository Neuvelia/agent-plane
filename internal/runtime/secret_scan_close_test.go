package runtime

import (
	"context"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/app/trace"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/run"
	"agentplane/internal/domain/secrets"
)

type noopWindow struct{}

func (noopWindow) Scan(context.Context, []secrets.ScanTarget) (secrets.ScanReport, error) {
	return secrets.ScanReport{}, nil
}

// scanTestMachine returns a machine whose emitted events land in the slice.
func scanTestMachine() (*runMachine, *[]events.Event) {
	var emitted []events.Event
	collector := trace.NewCollector(system.Clock{}, "run", "g", "v")
	collector.SetPersist(func(e events.Event) error { emitted = append(emitted, e); return nil })
	machine := &runMachine{
		nodes:   map[string]*run.NodeResult{"a": {NodeRunID: "nr-a"}},
		emitter: collector,
		clock:   system.Clock{},
	}
	return machine, &emitted
}

func secretScanReasons(emitted *[]events.Event) []string {
	var reasons []string
	for _, e := range *emitted {
		if check, ok := e.Payload.(events.GateCheckPayload); ok && check.Kind == events.CheckSecretInWorkspace {
			reasons = append(reasons, check.Reason)
		}
	}
	return reasons
}

// The worker skipped the scan because it expected a retry that the coordinator
// then did not schedule: the open window must become an unobserved result.
func TestOpenScanWindowOfFinishedNodeIsRecordedUnobserved(t *testing.T) {
	m, emitted := scanTestMachine()
	m.scans.store("a", scanSession{window: noopWindow{}})
	m.closeUnscannedWindows(context.Background(), []completedNode{{id: "a"}}, nil)
	if got := secretScanReasons(emitted); len(got) != 1 || got[0] != "secret_scan_unobserved" {
		t.Fatalf("reasons = %v, want one secret_scan_unobserved", got)
	}
	if _, open := m.scans.lookup("a"); open {
		t.Error("the window must be closed")
	}
}

func TestOpenScanWindowOfRetriedNodeStaysOpen(t *testing.T) {
	m, emitted := scanTestMachine()
	m.scans.store("a", scanSession{window: noopWindow{}})
	m.closeUnscannedWindows(context.Background(), []completedNode{{id: "a"}}, []string{"a"})
	if got := secretScanReasons(emitted); len(got) != 0 {
		t.Fatalf("reasons = %v, want none while a retry follows", got)
	}
	if _, open := m.scans.lookup("a"); !open {
		t.Error("the window must stay open for the retry")
	}
}

func TestOpenScanWindowIsDroppedSilentlyOnCancellation(t *testing.T) {
	m, emitted := scanTestMachine()
	m.scans.store("a", scanSession{window: noopWindow{}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.closeUnscannedWindows(ctx, []completedNode{{id: "a"}}, nil)
	if got := secretScanReasons(emitted); len(got) != 0 {
		t.Fatalf("reasons = %v, want none after cancellation", got)
	}
}

func TestFinishScanWithoutWindowIsUnobservedNotAPanic(t *testing.T) {
	m, _ := scanTestMachine()
	redactor, err := newLogRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	check := m.finishScan(context.Background(), m.scans.take("missing"), nil, redactor)
	if check.Reason != "secret_scan_unobserved" || check.Verdict != events.CheckGreen {
		t.Fatalf("check = %s/%s, want green secret_scan_unobserved", check.Verdict, check.Reason)
	}
}
