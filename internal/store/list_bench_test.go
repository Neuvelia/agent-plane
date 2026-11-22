package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"agentplane/internal/domain/events"
)

// listBudget is the F3.2 target for listing seedRuns finished runs with warm
// caches on Linux. Other platforms record the number but do not gate on it.
const (
	seedRuns   = 5000
	listBudget = time.Second
)

func seededRunID(n int) string { return fmt.Sprintf("%08x-0000-4000-8000-000000000000", n+1) }

// seedStore writes n finished runs directly, with their snapshots and without
// an fsync per file: the benchmark measures listing, not creation.
func seedStore(tb testing.TB, n int) Store {
	tb.Helper()
	s := Store{Dir: tb.TempDir()}
	base := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		id := seededRunID(i)
		dir := filepath.Join(s.Dir, id)
		if err := os.Mkdir(dir, dirMode); err != nil {
			tb.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Second)
		summary := Summary{RunID: id}
		var log []byte
		for _, e := range []events.Event{started(id, at), finished(id, 2, "succeeded", at.Add(time.Millisecond))} {
			line, err := json.Marshal(e)
			if err != nil {
				tb.Fatal(err)
			}
			log = append(append(log, line...), '\n')
			summary.EventCount++
			summary.observe(e)
		}
		snapshot, err := json.Marshal(snapshotRecord{Summary: summary, LogBytes: int64(len(log))})
		if err != nil {
			tb.Fatal(err)
		}
		for name, data := range map[string][]byte{eventsFile: log, graphFile: []byte("schema_version: 0\n"), snapshotFile: snapshot} {
			if err := os.WriteFile(filepath.Join(dir, name), data, fileMode); err != nil {
				tb.Fatal(err)
			}
		}
	}
	return s
}

func TestListFiveThousandRunsMeetsTheBudget(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("budget gate: skipped under -short and the race detector")
	}
	s := seedStore(t, seedRuns)
	parsed := parseCounter(&s)
	if _, err := s.List(); err != nil { // warm the caches
		t.Fatal(err)
	}
	start := time.Now()
	list, err := s.List()
	elapsed := time.Since(start)
	if err != nil || len(list) != seedRuns {
		t.Fatalf("%v, %d runs", err, len(list))
	}
	if len(*parsed) != 0 {
		t.Fatalf("List parsed %d logs although every snapshot is valid", len(*parsed))
	}
	t.Logf("List of %d runs, warm: %v (budget %v, %s)", seedRuns, elapsed, listBudget, runtime.GOOS)
	if runtime.GOOS == "linux" && elapsed > listBudget {
		t.Fatalf("List of %d runs took %v, budget %v", seedRuns, elapsed, listBudget)
	}
}

func BenchmarkList5000(b *testing.B) {
	s := seedStore(b, seedRuns)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.List(); err != nil {
			b.Fatal(err)
		}
	}
}
