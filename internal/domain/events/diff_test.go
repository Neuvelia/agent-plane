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
	hashA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	hashB = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func diffEvent(version int, p AttemptDiffPayload) Event {
	return Event{SchemaVersion: version, Type: AttemptDiff, Timestamp: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC), Sequence: 9, RunID: "run-1", GraphID: "g", GraphVersion: "sha256-v0:x", NodeID: "worker", NodeRunID: "nr", AttemptID: "att", Payload: p}
}

func modifiedFile() AttemptDiffPayload {
	return AttemptDiffPayload{Path: "dir/a.txt", Change: DiffModified, BeforeSHA256: hashA, AfterSHA256: hashB, BeforeSize: 10, AfterSize: 12, AddedLines: 2, DeletedLines: 1, Patch: "@@ -1 +1,2 @@\n-a\n+b\n+c\n"}
}

func summary() AttemptDiffPayload {
	return AttemptDiffPayload{Files: 2, OmittedFiles: 1, AddedLines: 3, DeletedLines: 1, PatchBytes: 40}
}

func marshals(p AttemptDiffPayload) bool {
	_, err := json.Marshal(diffEvent(SchemaVersion, p))
	return err == nil
}

func TestAttemptDiffRoundTripAndBounds(t *testing.T) {
	valid := map[string]AttemptDiffPayload{
		"modified":           modifiedFile(),
		"added":              {Path: "n.txt", Change: DiffAdded, AfterSHA256: hashB, AfterSize: 3, AddedLines: 1, Patch: "@@ -0,0 +1 @@\n+x\n"},
		"deleted":            {Path: "d.txt", Change: DiffDeleted, BeforeSHA256: hashA, BeforeSize: 3, DeletedLines: 1, Patch: "@@ -1 +0,0 @@\n-x\n"},
		"mode only":          {Path: "run.sh", Change: DiffMode, BeforeSHA256: hashA, AfterSHA256: hashA, BeforeSize: 5, AfterSize: 5},
		"binary":             {Path: "i.png", Change: DiffModified, Binary: true, BeforeSHA256: hashA, AfterSHA256: hashB, BeforeSize: 5, AfterSize: 6, OmittedReason: DiffOmitBinary},
		"ignored no hashes":  {Path: "node_modules/x", Change: DiffModified, OmittedReason: DiffOmitIgnored},
		"budget with hashes": {Path: "big", Change: DiffModified, BeforeSHA256: hashA, AfterSHA256: hashB, BeforeSize: 9 << 20, AfterSize: 9 << 20, OmittedReason: DiffOmitBudget},
		"redacted truncated": {Path: "r.txt", Change: DiffModified, Redacted: true, Truncated: true, Patch: "@@ -1 +1 @@\n-a\n+[REDACTED]\n"},
		"summary":            summary(),
		"summary diagnostic": {OmittedReason: DiffOmitUnreadable, DiagnosticCode: DiffGitUnavailable},
		"max patch":          {Path: "p", Change: DiffModified, Patch: strings.Repeat("a", MaxAttemptDiffPatchBytes)},
		"max path":           {Path: strings.Repeat("a", MaxAttemptDiffPathBytes), Change: DiffAdded},
		"max files":          {Files: MaxAttemptDiffFiles, PatchBytes: MaxAttemptDiffAttemptBytes},
	}
	for name, p := range valid {
		e := diffEvent(SchemaVersion, p)
		data, err := json.Marshal(e)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("%s: decode: %v\n%s", name, err, data)
			continue
		}
		if !reflect.DeepEqual(got, e) {
			t.Errorf("%s: round trip differs:\n%#v\n%#v", name, got, e)
		}
		if again, _ := json.Marshal(got); string(again) != string(data) {
			t.Errorf("%s: re-encoding differs", name)
		}
	}
	over := map[string]AttemptDiffPayload{
		"patch over bound":      {Path: "p", Change: DiffModified, Patch: strings.Repeat("a", MaxAttemptDiffPatchBytes+1)},
		"path over bound":       {Path: strings.Repeat("a", MaxAttemptDiffPathBytes+1), Change: DiffAdded},
		"files over bound":      {Files: MaxAttemptDiffFiles + 1},
		"patch_bytes over":      {PatchBytes: MaxAttemptDiffAttemptBytes + 1},
		"negative size":         {Path: "p", Change: DiffAdded, AfterSize: -1},
		"negative added":        {Path: "p", Change: DiffAdded, AddedLines: -1},
		"negative deleted":      {Path: "p", Change: DiffAdded, DeletedLines: -1},
		"negative omitted":      {OmittedFiles: -1},
		"patch invalid utf8":    {Path: "p", Change: DiffModified, Patch: "\xff"},
		"added with before":     {Path: "p", Change: DiffAdded, BeforeSHA256: hashA},
		"added with before sz":  {Path: "p", Change: DiffAdded, BeforeSize: 1},
		"deleted with after":    {Path: "p", Change: DiffDeleted, AfterSHA256: hashA},
		"deleted with after sz": {Path: "p", Change: DiffDeleted, AfterSize: 1},
		"file without change":   {Path: "p"},
		"unknown change":        {Path: "p", Change: "renamed"},
		"summary with change":   {Change: DiffModified},
		"summary with hash":     {BeforeSHA256: hashA},
		"summary with patch":    {Patch: "x"},
		"summary with size":     {AfterSize: 1},
		"summary binary":        {Binary: true},
		"file with summary":     {Path: "p", Change: DiffAdded, Files: 1},
		"file with diagnostic":  {Path: "p", Change: DiffAdded, DiagnosticCode: DiffTimeout, OmittedReason: DiffOmitUnreadable},
	}
	for name, p := range over {
		if marshals(p) {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

func TestAttemptDiffRequiresSchema12(t *testing.T) {
	if AttemptDiffSchema != 12 {
		t.Fatalf("attempt_diff is the schema 12 fact, got %d", AttemptDiffSchema)
	}
	if SchemaVersion < AttemptDiffSchema {
		t.Fatalf("SchemaVersion %d must include schema %d", SchemaVersion, AttemptDiffSchema)
	}
	for _, below := range []int{1, 10, AttemptDiffSchema - 1} {
		if _, err := json.Marshal(diffEvent(below, modifiedFile())); err == nil {
			t.Errorf("attempt_diff must not be serialized under schema %d", below)
		}
	}
	e := diffEvent(AttemptDiffSchema, modifiedFile())
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, below := range []int{1, 10, AttemptDiffSchema - 1} {
		old := strings.Replace(string(data), fmt.Sprintf(`"event_schema_version":%d`, AttemptDiffSchema), fmt.Sprintf(`"event_schema_version":%d`, below), 1)
		var got Event
		if json.Unmarshal([]byte(old), &got) == nil {
			t.Errorf("a schema %d stream containing attempt_diff must not decode", below)
		}
	}
	if _, err := json.Marshal(diffEvent(0, modifiedFile())); err != nil {
		t.Errorf("schema 0 means current: %v", err)
	}
	if _, err := json.Marshal(diffEvent(SchemaVersion+1, modifiedFile())); err == nil {
		t.Error("a future schema is not representable")
	}
}

func TestAttemptDiffRequiresAttemptCorrelation(t *testing.T) {
	for name, edit := range map[string]func(*Event){
		"no node":     func(e *Event) { e.NodeID = "" },
		"no node run": func(e *Event) { e.NodeRunID = "" },
		"no attempt":  func(e *Event) { e.AttemptID = "" },
	} {
		e := diffEvent(SchemaVersion, modifiedFile())
		edit(&e)
		if _, err := json.Marshal(e); err == nil {
			t.Errorf("%s: attempt_diff is attempt-scoped", name)
		}
	}
	wrong := diffEvent(SchemaVersion, modifiedFile())
	wrong.Type = NodeFinished
	if _, err := json.Marshal(wrong); err == nil {
		t.Error("an attempt_diff payload under another event type must be rejected")
	}
}

func TestAttemptDiffRejectsUnknownReasonAndBadHash(t *testing.T) {
	bad := map[string]func(*AttemptDiffPayload){
		"unknown reason": func(p *AttemptDiffPayload) { p.OmittedReason = "huge" },
		"unknown diagnostic": func(p *AttemptDiffPayload) {
			*p = summary()
			p.DiagnosticCode = "E_OTHER"
			p.OmittedReason = DiffOmitUnreadable
		},
		"short hash":          func(p *AttemptDiffPayload) { p.AfterSHA256 = hashB[:63] },
		"uppercase hash":      func(p *AttemptDiffPayload) { p.AfterSHA256 = strings.ToUpper(hashB) },
		"non hex hash":        func(p *AttemptDiffPayload) { p.BeforeSHA256 = strings.Repeat("g", 64) },
		"sha1 length":         func(p *AttemptDiffPayload) { p.BeforeSHA256 = hashA[:40] },
		"reason with patch":   func(p *AttemptDiffPayload) { p.OmittedReason = DiffOmitTooLarge },
		"diagnostic no flag":  func(p *AttemptDiffPayload) { *p = summary(); p.DiagnosticCode = DiffGitFailed },
		"unreadable no code":  func(p *AttemptDiffPayload) { *p = summary(); p.OmittedReason = DiffOmitUnreadable },
		"summary other cause": func(p *AttemptDiffPayload) { *p = summary(); p.OmittedReason = DiffOmitBudget },
	}
	for name, edit := range bad {
		p := modifiedFile()
		edit(&p)
		if marshals(p) {
			t.Errorf("%s: must be rejected", name)
		}
	}
	for _, code := range []string{DiffGitUnavailable, DiffGitFailed, DiffTimeout, DiffScratch, DiffIgnoredLimit, DiffUnsupportedEntry} {
		if !marshals(AttemptDiffPayload{OmittedReason: DiffOmitUnreadable, DiagnosticCode: code}) {
			t.Errorf("diagnostic %s must be accepted", code)
		}
	}
	for _, reason := range []string{DiffOmitBinary, DiffOmitTooLarge, DiffOmitIgnored, DiffOmitBudget, DiffOmitUnreadable} {
		if !marshals(AttemptDiffPayload{Path: "p", Change: DiffModified, OmittedReason: reason}) {
			t.Errorf("reason %s must be accepted", reason)
		}
	}
}

func TestAttemptDiffRejectsHostilePaths(t *testing.T) {
	for name, path := range map[string]string{
		"parent":          "../x",
		"inner parent":    "a/../b",
		"git component":   "a/.git/b",
		"git root":        ".git/config",
		"git case":        "a/.GIT/b",
		"nul":             "a\x00b",
		"absolute":        "/etc/passwd",
		"dot":             "./a",
		"inner dot":       "a/./b",
		"empty component": "a//b",
		"trailing slash":  "a/",
		"too long":        strings.Repeat("x", MaxAttemptDiffPathBytes+1),
		"invalid utf8":    "bad\xff",
		"backslash":       "a\\b",
		"drive prefix":    "C:/x",
		"drive relative":  "C:x",
		"colon inside":    "a/b:c",
	} {
		if marshals(AttemptDiffPayload{Path: path, Change: DiffAdded}) {
			t.Errorf("%s: path %q must be rejected", name, path)
		}
	}
	for _, path := range []string{"a", "dir/sub/file.txt", "日本語/ファイル", ".gitignore", ".github/workflows/x.yml", "a b/c", "a.git/b", "..x/y"} {
		if !marshals(AttemptDiffPayload{Path: path, Change: DiffAdded}) {
			t.Errorf("path %q must be accepted", path)
		}
	}
}

func TestAttemptDiffPatchMustBeEmptyWhenOmitted(t *testing.T) {
	for _, reason := range []string{DiffOmitBinary, DiffOmitTooLarge, DiffOmitIgnored, DiffOmitBudget, DiffOmitUnreadable} {
		p := AttemptDiffPayload{Path: "p", Change: DiffModified, OmittedReason: reason, Patch: "@@ -1 +1 @@\n"}
		if marshals(p) {
			t.Errorf("%s with a patch must be rejected", reason)
		}
	}
}

func TestAttemptDiffSummaryRules(t *testing.T) {
	for name, p := range map[string]AttemptDiffPayload{
		"plain":      summary(),
		"empty":      {},
		"diagnostic": {OmittedReason: DiffOmitUnreadable, DiagnosticCode: DiffTimeout, Files: 3},
	} {
		if !marshals(p) {
			t.Errorf("%s summary must be accepted", name)
		}
	}
	data, err := json.Marshal(diffEvent(SchemaVersion, summary()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"path"`) || strings.Contains(string(data), `"change"`) {
		t.Fatalf("a summary carries no file fields: %s", data)
	}
}

func TestAttemptDiffWireShapeIsFrozen(t *testing.T) {
	data, err := json.Marshal(diffEvent(SchemaVersion, modifiedFile()))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"event_schema_version":%d,"type":"attempt_diff","timestamp":"2026-11-17T09:00:00Z","sequence":9,"run_id":"run-1","graph_id":"g","graph_version":"sha256-v0:x","node_id":"worker","node_run_id":"nr","attempt_id":"att","payload":{"path":"dir/a.txt","change":"modified","binary":false,"redacted":false,"truncated":false,"before_sha256":"%s","after_sha256":"%s","before_size":10,"after_size":12,"added_lines":2,"deleted_lines":1,"patch":"@@ -1 +1,2 @@\n-a\n+b\n+c\n"}}`, SchemaVersion, hashA, hashB)
	if string(data) != want {
		t.Fatalf("wire shape changed:\n%s\n%s", data, want)
	}
}

func TestAttemptDiffDecodingIsStrict(t *testing.T) {
	wrap := func(payload string) string {
		return fmt.Sprintf(`{"event_schema_version":%d,"type":"attempt_diff","timestamp":"2026-11-17T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","node_id":"n","node_run_id":"nr","attempt_id":"a","payload":%s}`, SchemaVersion, payload)
	}
	for name, raw := range map[string]string{
		"unknown field":   wrap(`{"path":"p","change":"added","extra":1}`),
		"null payload":    wrap(`null`),
		"wrong type":      wrap(`{"path":"p","change":"added","added_lines":"1"}`),
		"huge int":        wrap(`{"path":"p","change":"added","added_lines":99999999999999999999}`),
		"payload of node": wrap(`{"status":"succeeded"}`),
	} {
		var e Event
		if json.Unmarshal([]byte(raw), &e) == nil {
			t.Errorf("%s: malformed event decoded", name)
		}
	}
}

func TestAttemptDiffCloneIsDetached(t *testing.T) {
	e := diffEvent(SchemaVersion, modifiedFile())
	cloned := ClonePayload(e.Payload).(AttemptDiffPayload)
	cloned.Patch = "changed"
	if e.Payload.(AttemptDiffPayload).Patch == "changed" {
		t.Fatal("clone shares state with the original")
	}
	if !reflect.DeepEqual(ClonePayload(e.Payload), e.Payload) {
		t.Fatal("clone differs from the original")
	}
}

// Every earlier schema keeps decoding, and a schema 11 stream (workspace
// facts) is untouched by the schema 12 bump.
func TestSchemas1To11StillDecodeAfterAttemptDiff(t *testing.T) {
	for version := 1; version < AttemptDiffSchema; version++ {
		raw := fmt.Sprintf(`{"event_schema_version":%d,"type":"run_started","timestamp":"2026-11-17T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":1}}`, version)
		var e Event
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Errorf("schema %d: %v", version, err)
		}
	}
	for _, e := range workspaceFacts() {
		e.SchemaVersion = WorkspaceSchema
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("%s: %v", e.Type, err)
		}
		var got Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("schema %d %s: %v", WorkspaceSchema, e.Type, err)
		}
	}
	future := fmt.Sprintf(`{"event_schema_version":%d,"type":"run_started","timestamp":"2026-11-17T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":1}}`, SchemaVersion+1)
	var e Event
	if json.Unmarshal([]byte(future), &e) == nil {
		t.Error("an unknown newer schema must be refused")
	}
}

func TestAttemptDiffSixteenKiBPatchSurvivesJSONEscaping(t *testing.T) {
	patch := strings.Repeat("+say \"héllo\" \\ 日本\t\n", 700)
	patch = patch[:MaxAttemptDiffPatchBytes]
	patch = strings.ToValidUTF8(patch, "")
	p := AttemptDiffPayload{Path: "q.txt", Change: DiffModified, Patch: patch}
	data, err := json.Marshal(diffEvent(SchemaVersion, p))
	if err != nil {
		t.Fatal(err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Payload.(AttemptDiffPayload).Patch != patch {
		t.Fatal("patch changed in the JSON round trip")
	}
}

// One JSONL line per schema 1..12, the last one an attempt_diff with a 16 KiB patch.
func TestAttemptDiffJSONLinesOfEverySchemaDecode(t *testing.T) {
	unit := "+say \"héllo\" \\ 日本\t\n"
	patch := strings.ToValidUTF8(strings.Repeat(unit, 1000)[:MaxAttemptDiffPatchBytes], "")
	line, err := json.Marshal(diffEvent(SchemaVersion, AttemptDiffPayload{Path: "q.txt", Change: DiffModified, Patch: patch}))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for version := 1; version < AttemptDiffSchema; version++ {
		lines = append(lines, fmt.Sprintf(`{"event_schema_version":%d,"type":"run_started","timestamp":"2026-11-17T09:00:00Z","sequence":1,"run_id":"r","graph_id":"g","graph_version":"v","payload":{"node_count":1}}`, version))
	}
	lines = append(lines, string(line))
	for i, l := range lines {
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if got, ok := e.Payload.(AttemptDiffPayload); ok && got.Patch != patch {
			t.Fatal("16 KiB patch changed in the round trip")
		}
	}
}
