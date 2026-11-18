package events

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	shaA = "0123456789abcdef0123456789abcdef01234567"
	shaB = "89abcdef0123456789abcdef0123456789abcdef"
	ref1 = "refs/agentplane/run-1/units/worker-0123456789abcdef"
	ref2 = "refs/agentplane/run-1/integrated"
)

func wsEvent(version int, typ Type, payload Payload) Event {
	e := Event{SchemaVersion: version, Type: typ, Timestamp: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC), Sequence: 7, RunID: "run-1", GraphID: "g", GraphVersion: "sha256-v0:x", NodeID: "worker", NodeRunID: "nr", Payload: payload}
	switch typ {
	case WorkspaceIntegrated, WorkspaceConflict:
		e.AttemptID = "att"
	case SourceWorkspaceChanged:
		e.NodeID, e.NodeRunID = "", ""
	}
	return e
}

func workspaceFacts() []Event {
	return []Event{
		wsEvent(SchemaVersion, WorkspacePrepared, WorkspacePreparedPayload{BaseSHA: shaA, Mode: WorkspaceModeWorktree, Path: "/cache/agentplane/wt", SourceRoot: "/home/u/repo"}),
		wsEvent(SchemaVersion, WorkspaceIntegrated, WorkspaceIntegratedPayload{UnitRef: ref1, UnitCommit: shaA, IntegratedRef: ref2, IntegratedCommit: shaB, ChangedPaths: 3}),
		wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{"a.txt", "dir/b.txt"}, Omitted: 2}),
		wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRetained, Reason: ReleaseCleanupError, Path: "/cache/agentplane/wt", CleanupError: "worktree_remove"}),
		wsEvent(SchemaVersion, SourceWorkspaceChanged, SourceWorkspaceChangedPayload{Paths: []string{"x.txt"}}),
		wsEvent(SchemaVersion, SourceWorkspaceChanged, SourceWorkspaceChangedPayload{Paths: []string{}, Indeterminate: true}),
	}
}

func TestWorkspaceEventsRoundTripStrictly(t *testing.T) {
	for _, e := range workspaceFacts() {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("%s: %v", e.Type, err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("%s: %v\n%s", e.Type, err, data)
		}
		if !reflect.DeepEqual(got, e) {
			t.Fatalf("%s round trip differs:\n%#v\n%#v", e.Type, got, e)
		}
		again, _ := json.Marshal(got)
		if string(again) != string(data) {
			t.Fatalf("%s re-encoding differs", e.Type)
		}
	}
}

func TestWorkspaceWireShapeIsFrozen(t *testing.T) {
	data, err := json.Marshal(workspaceFacts()[1])
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"event_schema_version":%d,"type":"workspace_integrated","timestamp":"2026-11-16T09:00:00Z","sequence":7,"run_id":"run-1","graph_id":"g","graph_version":"sha256-v0:x","node_id":"worker","node_run_id":"nr","attempt_id":"att","payload":{"unit_ref":"%s","unit_commit":"%s","integrated_ref":"%s","integrated_commit":"%s","changed_paths":3}}`, SchemaVersion, ref1, shaA, ref2, shaB)
	if string(data) != want {
		t.Fatalf("wire shape changed:\n%s\n%s", data, want)
	}
}

func TestWorkspaceEventsRequireTheirSchema(t *testing.T) {
	// 10 is the schema just below: value_recorded owns it, workspace facts must not.
	for _, below := range []int{9, WorkspaceSchema - 1} {
		for _, e := range workspaceFacts() {
			e.SchemaVersion = below
			if _, err := json.Marshal(e); err == nil {
				t.Fatalf("%s must not be serialized under schema %d", e.Type, below)
			}
			e.SchemaVersion = WorkspaceSchema
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatalf("%s under schema %d: %v", e.Type, WorkspaceSchema, err)
			}
			old := strings.Replace(string(data), fmt.Sprintf(`"event_schema_version":%d`, WorkspaceSchema), fmt.Sprintf(`"event_schema_version":%d`, below), 1)
			var got Event
			if json.Unmarshal([]byte(old), &got) == nil {
				t.Fatalf("%s must not decode under schema %d", e.Type, below)
			}
		}
	}
}

func TestWorkspaceEventCorrelation(t *testing.T) {
	facts := workspaceFacts()
	mutate := func(i int, f func(*Event)) error {
		e := facts[i]
		f(&e)
		_, err := json.Marshal(e)
		return err
	}
	for _, i := range []int{0, 1, 2, 3} { // node-scoped
		if mutate(i, func(e *Event) { e.NodeID = "" }) == nil || mutate(i, func(e *Event) { e.NodeRunID = "" }) == nil {
			t.Fatalf("%s needs node and node run IDs", facts[i].Type)
		}
	}
	if mutate(0, func(e *Event) { e.AttemptID = "a" }) == nil || mutate(3, func(e *Event) { e.AttemptID = "a" }) == nil {
		t.Fatal("prepared and released are not attempt-scoped")
	}
	if mutate(1, func(e *Event) { e.AttemptID = "" }) == nil || mutate(2, func(e *Event) { e.AttemptID = "" }) == nil {
		t.Fatal("integrated and conflict belong to the accepted attempt")
	}
	if mutate(4, func(e *Event) { e.NodeID, e.NodeRunID = "n", "r" }) == nil || mutate(4, func(e *Event) { e.AttemptID = "a" }) == nil {
		t.Fatal("source_workspace_changed is run-level")
	}
}

func TestWorkspacePayloadValidation(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	many := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("f%03d", i)
		}
		return out
	}
	prepared := func(p WorkspacePreparedPayload) Event { return wsEvent(SchemaVersion, WorkspacePrepared, p) }
	good := WorkspacePreparedPayload{BaseSHA: shaA, Mode: WorkspaceModeWorktree, Path: "/w", SourceRoot: "/s"}
	with := func(f func(*WorkspacePreparedPayload)) WorkspacePreparedPayload { p := good; f(&p); return p }
	bad := map[string]Event{
		"short sha":         prepared(with(func(p *WorkspacePreparedPayload) { p.BaseSHA = "abc123" })),
		"uppercase sha":     prepared(with(func(p *WorkspacePreparedPayload) { p.BaseSHA = strings.ToUpper(shaA) })),
		"ref name as sha":   prepared(with(func(p *WorkspacePreparedPayload) { p.BaseSHA = "HEAD" })),
		"unknown mode":      prepared(with(func(p *WorkspacePreparedPayload) { p.Mode = "clone" })),
		"relative path":     prepared(with(func(p *WorkspacePreparedPayload) { p.Path = "wt" })),
		"newline in path":   prepared(with(func(p *WorkspacePreparedPayload) { p.Path = "/a\nb" })),
		"nul in root":       prepared(with(func(p *WorkspacePreparedPayload) { p.SourceRoot = "/a\x00b" })),
		"path too long":     prepared(with(func(p *WorkspacePreparedPayload) { p.Path = "/" + long(MaxWorkspaceLocalPath) })),
		"invalid utf8 path": prepared(with(func(p *WorkspacePreparedPayload) { p.Path = "/\xff" })),

		"integrated ref outside": wsEvent(SchemaVersion, WorkspaceIntegrated, WorkspaceIntegratedPayload{UnitRef: "refs/heads/main", UnitCommit: shaA, IntegratedRef: ref2, IntegratedCommit: shaB}),
		"integrated bad commit":  wsEvent(SchemaVersion, WorkspaceIntegrated, WorkspaceIntegratedPayload{UnitRef: ref1, UnitCommit: "x", IntegratedRef: ref2, IntegratedCommit: shaB}),
		"ref too long":           wsEvent(SchemaVersion, WorkspaceIntegrated, WorkspaceIntegratedPayload{UnitRef: "refs/agentplane/" + long(MaxWorkspaceRefBytes), UnitCommit: shaA, IntegratedRef: ref2, IntegratedCommit: shaB}),
		"ref double slash":       wsEvent(SchemaVersion, WorkspaceIntegrated, WorkspaceIntegratedPayload{UnitRef: "refs/agentplane/a//b", UnitCommit: shaA, IntegratedRef: ref2, IntegratedCommit: shaB}),

		"conflict nil paths":       wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Omitted: 1}),
		"conflict no paths at all": wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{}}),
		"conflict too many":        wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: many(MaxWorkspacePaths + 1)}),
		"conflict unsorted":        wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{"b", "a"}}),
		"conflict duplicate":       wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{"a", "a"}}),
		"conflict absolute path":   wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{"/etc/passwd"}}),
		"conflict traversal":       wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{"a/../b"}}),
		"conflict long path":       wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{long(MaxWorkspacePathBytes + 1)}}),
		"conflict invalid utf8":    wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{"\xff"}}),
		"conflict empty path":      wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{""}}),

		"release unknown action":        wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: "deleted", Reason: ReleaseIntegrated, Path: "/w"}),
		"removed with retained reason":  wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRemoved, Reason: ReleaseConflict, Path: "/w"}),
		"retained with removed reason":  wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRetained, Reason: ReleaseIntegrated, Path: "/w"}),
		"unknown reason":                wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRetained, Reason: "because", Path: "/w"}),
		"cleanup_failed without class":  wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRetained, Reason: ReleaseCleanupError, Path: "/w"}),
		"class without cleanup_failed":  wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRetained, Reason: ReleaseError, Path: "/w", CleanupError: "x"}),
		"error class carries free text": wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRetained, Reason: ReleaseCleanupError, Path: "/w", CleanupError: "fatal: cannot remove /secret"}),
		"release relative path":         wsEvent(SchemaVersion, WorkspaceReleased, WorkspaceReleasedPayload{Action: WorkspaceRemoved, Reason: ReleaseIntegrated, Path: "w"}),

		"source nil paths":                 wsEvent(SchemaVersion, SourceWorkspaceChanged, SourceWorkspaceChangedPayload{}),
		"source unchanged is not an event": wsEvent(SchemaVersion, SourceWorkspaceChanged, SourceWorkspaceChangedPayload{Paths: []string{}}),
		"indeterminate with paths":         wsEvent(SchemaVersion, SourceWorkspaceChanged, SourceWorkspaceChangedPayload{Paths: []string{"a"}, Indeterminate: true}),
		"indeterminate with omitted":       wsEvent(SchemaVersion, SourceWorkspaceChanged, SourceWorkspaceChangedPayload{Paths: []string{}, Omitted: 1, Indeterminate: true}),
	}
	for name, e := range bad {
		if _, err := json.Marshal(e); err == nil {
			t.Errorf("%s: invalid payload was accepted", name)
		}
	}
	// The boundary values themselves are valid.
	edge := []Event{
		wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: many(MaxWorkspacePaths), Omitted: 1 << 40}),
		wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: []string{long(MaxWorkspacePathBytes)}}),
		prepared(with(func(p *WorkspacePreparedPayload) { p.Path = "/" + long(MaxWorkspaceLocalPath-1) })),
		prepared(with(func(p *WorkspacePreparedPayload) { p.BaseSHA = strings.Repeat("a", 64) })), // SHA-256 repositories
	}
	for i, e := range edge {
		if _, err := json.Marshal(e); err != nil {
			t.Errorf("edge %d rejected: %v", i, err)
		}
	}
}

func TestWorkspaceDecodingIsStrict(t *testing.T) {
	wrap := func(typ, payload string, extra string) string {
		return fmt.Sprintf(`{"event_schema_version":%d,"type":%q,"timestamp":"2026-11-16T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v",%s"payload":%s}`, SchemaVersion, typ, extra, payload)
	}
	node := `"node_id":"n","node_run_id":"nr",`
	cases := map[string]string{
		"unknown field":         wrap("workspace_prepared", fmt.Sprintf(`{"base_sha":%q,"mode":"worktree","path":"/w","source_root":"/s","extra":1}`, shaA), node),
		"null payload":          wrap("workspace_prepared", `null`, node),
		"null conflict paths":   wrap("workspace_conflict", fmt.Sprintf(`{"unit_ref":%q,"unit_commit":%q,"tip_commit":%q,"paths":null,"omitted":1}`, ref1, shaA, shaB), node+`"attempt_id":"a",`),
		"missing conflict path": wrap("workspace_conflict", fmt.Sprintf(`{"unit_ref":%q,"unit_commit":%q,"tip_commit":%q,"omitted":1}`, ref1, shaA, shaB), node+`"attempt_id":"a",`),
		"missing base sha":      wrap("workspace_prepared", `{"mode":"worktree","path":"/w","source_root":"/s"}`, node),
		"wrong type for count":  wrap("workspace_integrated", fmt.Sprintf(`{"unit_ref":%q,"unit_commit":%q,"integrated_ref":%q,"integrated_commit":%q,"changed_paths":"3"}`, ref1, shaA, ref2, shaB), node+`"attempt_id":"a",`),
		"negative count":        wrap("workspace_integrated", fmt.Sprintf(`{"unit_ref":%q,"unit_commit":%q,"integrated_ref":%q,"integrated_commit":%q,"changed_paths":-1}`, ref1, shaA, ref2, shaB), node+`"attempt_id":"a",`),
		"payload of other type": wrap("workspace_released", `{"decision":"approved"}`, node),
	}
	for name, raw := range cases {
		var e Event
		if json.Unmarshal([]byte(raw), &e) == nil {
			t.Errorf("%s: malformed event decoded", name)
		}
	}
}

func TestBoundWorkspacePaths(t *testing.T) {
	kept, omitted := BoundWorkspacePaths([]string{"b", "a", "a", "dir/c"})
	if !reflect.DeepEqual(kept, []string{"a", "b", "dir/c"}) || omitted != 0 {
		t.Fatalf("sorted and de-duplicated: %v %d", kept, omitted)
	}
	hostile := []string{"ok", "", "/abs", "../up", "a/../b", "bad\xff", strings.Repeat("x", MaxWorkspacePathBytes+1), "ok2"}
	kept, omitted = BoundWorkspacePaths(hostile)
	if !reflect.DeepEqual(kept, []string{"ok", "ok2"}) || omitted != 6 {
		t.Fatalf("unrepresentable paths must be counted, not lost: %v %d", kept, omitted)
	}
	many := make([]string, 100)
	for i := range many {
		many[i] = fmt.Sprintf("p%03d", i)
	}
	kept, omitted = BoundWorkspacePaths(many)
	if len(kept) != MaxWorkspacePaths || omitted != 36 || kept[0] != "p000" {
		t.Fatalf("cap: %d kept, %d omitted", len(kept), omitted)
	}
	kept, omitted = BoundWorkspacePaths(nil)
	if kept == nil || len(kept) != 0 || omitted != 0 {
		t.Fatal("an empty result must be a non-nil empty list so it serializes as []")
	}
	// Whatever the helper returns must be a valid payload.
	e := wsEvent(SchemaVersion, WorkspaceConflict, WorkspaceConflictPayload{UnitRef: ref1, UnitCommit: shaA, TipCommit: shaB, Paths: kept, Omitted: omitted + 1})
	if _, err := json.Marshal(e); err != nil {
		t.Fatal(err)
	}
	// The input is not modified.
	in := []string{"b", "a"}
	BoundWorkspacePaths(in)
	if in[0] != "b" {
		t.Fatal("input was reordered")
	}
}

// Older records keep decoding exactly as before; none of them can carry the
// new facts, and a reader must not invent any.
func TestOlderSchemaFixturesStillDecode(t *testing.T) {
	fixtures := map[string]string{
		"schema 1 run_started":                 `{"event_schema_version":1,"type":"run_started","timestamp":"2026-10-18T12:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":2}}`,
		"schema 2 node_evidence":               `{"event_schema_version":2,"type":"node_evidence","timestamp":"2026-10-18T12:00:00Z","sequence":2,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{"adapter_id":"codex","adapter_version":"1","changed_files":[]}}`,
		"schema 3 status":                      `{"event_schema_version":3,"type":"status","timestamp":"2026-10-18T12:00:00Z","sequence":3,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{"code":"working"}}`,
		"schema 7 declared_scope_conflicts":    `{"event_schema_version":7,"type":"declared_scope_conflicts","timestamp":"2026-10-18T12:00:00Z","sequence":4,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"details":[],"omitted":0}}`,
		"schema 9 approval_requested":          `{"event_schema_version":9,"type":"approval_requested","timestamp":"2026-11-14T09:00:00Z","sequence":5,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","payload":{"prompt":"ship?","context":[]}}`,
		"schema 9 run_finished (previous top)": `{"event_schema_version":9,"type":"run_finished","timestamp":"2026-11-14T09:00:00Z","sequence":6,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"status":"succeeded","duration_ms":1,"node_counts":{"succeeded":1,"failed":0,"skipped":0,"cancelled":0}}}`,
	}
	for name, raw := range fixtures {
		var e Event
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if SchemaVersion != WorkspaceSchema {
		t.Logf("schema %d is newer than the workspace facts' %d (expected once a later feature bumps it)", SchemaVersion, WorkspaceSchema)
	}
}
