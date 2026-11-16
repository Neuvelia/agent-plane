package runtime_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/runtime"
)

// agentScopeFixture is the desktop F2.8a recording (desktop/test/fixtures/agent-scope-retry.json).
// The desktop tests and Storybook read it as engine output, so it is produced
// here by runtime.Run rather than written by hand. Regenerate with:
//
//	AGENTPLANE_UPDATE_FIXTURES=1 go test ./internal/runtime -run TestDesktopAgentScopeRetryFixture
//
// Without the variable the test fails when the committed file no longer matches
// what the engine emits, ignoring only per-run values (IDs, times, durations,
// the temporary repository's commit and graph hash).
const agentScopeFixture = "../../desktop/test/fixtures/agent-scope-retry.json"

func TestDesktopAgentScopeRetryFixture(t *testing.T) {
	dir := scopeRepo(t)
	d := scopeDefinition(dir, "deny")
	d.ID = "agent-scope-retry"
	d.Nodes["work"].Loop.MaxAttempts = 3
	observed := func(paths ...string) []agent.ChangedFile {
		files := make([]agent.ChangedFile, 0, len(paths))
		for _, p := range paths {
			files = append(files, agent.ChangedFile{Path: p, Provenance: agent.ProvenanceObserved})
		}
		return files
	}
	write := func(files map[string]string) error {
		for p, body := range files {
			if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0600); err != nil {
				return err
			}
		}
		return nil
	}
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, call int) (agent.AgentExecutionOutput, error) {
		// Attempt 1 edits outside the allowed scope (red); attempt 2 restores
		// outside.go and adds src/b.go (green).
		if call == 1 {
			if err := write(map[string]string{"src/a.go": "allowed", "outside.go": "forbidden"}); err != nil {
				return agent.AgentExecutionOutput{}, err
			}
			return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`), ChangedFiles: observed("outside.go", "src/a.go")}, nil
		}
		if err := write(map[string]string{"outside.go": "initial", "src/b.go": "allowed"}); err != nil {
			return agent.AgentExecutionOutput{}, err
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`), ChangedFiles: observed("outside.go", "src/a.go", "src/b.go")}, nil
	}
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || r.State != runtime.RunSucceeded {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	got, err := json.MarshalIndent(r.Events, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(bytes.ReplaceAll(got, []byte(dir), []byte("__WORKSPACE__")), '\n')
	if os.Getenv("AGENTPLANE_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(agentScopeFixture, got, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(agentScopeFixture)
	if err != nil {
		t.Fatal(err)
	}
	if g, w := normalizeFixture(t, got), normalizeFixture(t, want); !reflect.DeepEqual(g, w) {
		gj, _ := json.MarshalIndent(g, "", " ")
		wj, _ := json.MarshalIndent(w, "", " ")
		t.Fatalf("%s is stale; regenerate with AGENTPLANE_UPDATE_FIXTURES=1\n--- engine:\n%s\n--- committed:\n%s", agentScopeFixture, gj, wj)
	}
}

// normalizeFixture replaces per-run values: IDs become stable labels in order of
// first appearance (so references between events still have to match), and
// times, durations, the baseline commit and the graph hash are blanked.
func normalizeFixture(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	var walk func(any) any
	walk = func(x any) any {
		switch x := x.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys) // label order must not depend on map iteration
			for _, k := range keys {
				val := x[k]
				switch k {
				case "run_id", "node_run_id", "attempt_id", "previous_attempt_id", "source_run_id", "source_node_run_id":
					s, _ := val.(string)
					if _, ok := ids[s]; !ok {
						ids[s] = fmt.Sprintf("id-%d", len(ids)+1)
					}
					x[k] = ids[s]
				case "timestamp", "duration_ns", "duration_ms", "baseline_head", "graph_version", "source_graph_version":
					x[k] = "<per-run>"
				default:
					x[k] = walk(val)
				}
			}
			return x
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
			return x
		}
		return x
	}
	return walk(v)
}
