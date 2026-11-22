package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/testenv"
)

const valueSecret = "hunter2-value-secret"

// leakyGraph prints a configured secret to stdout and declares stdout as an
// output, so the accepted value contains the secret before redaction. Its
// graph input reaches the node, so a secret supplied as input is recorded too.
func leakyGraph(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "leaky.yaml")
	os.WriteFile(path, []byte(`schema_version: 0
id: leaky-values
inputs:
  note: {type: string, required: true}
nodes:
  echo:
    type: shell
    config: {command: /usr/bin/printenv, args: [AP_VALUE_SECRET], env_allowlist: [AP_VALUE_SECRET]}
    inputs:
      note: {type: string, required: true}
    outputs:
      stdout: {type: string, required: true}
edges: []
graph_input_bindings:
  - {from: inputs.note, to: echo.inputs.note}
outputs: {}
`), 0o600)
	return path
}

func valueEvents(evs []events.Event) map[string]events.ValueRecordedPayload {
	out := map[string]events.ValueRecordedPayload{}
	for _, e := range evs {
		if p, ok := e.Payload.(events.ValueRecordedPayload); ok {
			out[e.NodeID+"/"+p.Port] = p
		}
	}
	return out
}

func assertNoSecretUnder(t *testing.T, dir string) {
	t.Helper()
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if data, _ := os.ReadFile(p); strings.Contains(string(data), valueSecret) {
				t.Fatalf("secret reached %s", p)
			}
		}
		return nil
	})
}

func TestRunRecordsDemoValuesOnlyWhenAsked(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/printf", "/usr/bin/printenv")
	code, out, errout := cli(t, "run", demoGraphPath(t), "--approve", "approve", "--json")
	if code != 0 || strings.Contains(out, "value_recorded") {
		t.Fatalf("recording off: code=%d %s", code, errout)
	}
	dir := t.TempDir()
	code, out, errout = cli(t, "run", demoGraphPath(t), "--approve", "approve", "--json", "--record-values", "--store-dir", dir)
	if code != 0 {
		t.Fatalf("code=%d %s", code, errout)
	}
	var live struct {
		RunID  string         `json:"run_id"`
		Events []events.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &live); err != nil {
		t.Fatal(err)
	}
	values := valueEvents(live.Events)
	if p, ok := values["fast/stdout"]; !ok || p.Value == nil || *p.Value != "fast branch done" || p.Truncated || p.Redacted {
		t.Fatalf("fast.stdout = %+v", p)
	}
	if p, ok := values["flaky/json"]; !ok || p.Value == nil || !strings.Contains(*p.Value, `"ready":true`) || p.AttemptID == "" {
		t.Fatalf("flaky.json = %+v", p)
	}
	if p, ok := values["approve/decision"]; !ok || p.Value == nil || *p.Value != "approved" {
		t.Fatalf("approve.decision = %+v", p)
	}
	if len(values) != 3 {
		t.Fatalf("unexpected values: %v", values)
	}
	for key, p := range values {
		if p.Value != nil && strings.Contains(*p.Value, `"ready":false`) {
			t.Fatalf("rejected attempt recorded under %s", key)
		}
	}
	// A fresh invocation reads the same values back from the store.
	_, shown, _ := cli(t, "runs", "show", live.RunID, "--store-dir", dir, "--json")
	var stored struct {
		Events []events.Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(shown), &stored); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(valueEvents(live.Events))
	b, _ := json.Marshal(valueEvents(stored.Events))
	if string(a) != string(b) {
		t.Fatalf("stored values differ:\n%s\n%s", a, b)
	}
	if _, list, _ := cli(t, "runs", "list", "--store-dir", dir, "--json"); !strings.Contains(list, `"graph_version":"sha256-v0:`) {
		t.Fatalf("summary lacks graph_version: %s", list)
	}
}

func TestRunRecordedValuesNeverContainASecret(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/printf", "/usr/bin/printenv")
	t.Setenv("AP_VALUE_SECRET", valueSecret)
	dir := t.TempDir()
	code, out, errout := cli(t, "run", leakyGraph(t), "--input", "note=uses "+valueSecret, "--redact-env", "AP_VALUE_SECRET", "--record-values", "--store-dir", dir, "--json")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errout)
	}
	if strings.Contains(out, valueSecret) {
		t.Fatal("secret reached CLI JSON events")
	}
	assertNoSecretUnder(t, dir)
	var doc struct {
		Events []events.Event `json:"events"`
	}
	json.Unmarshal([]byte(out), &doc)
	values := valueEvents(doc.Events)
	if p := values["echo/stdout"]; !p.Redacted || p.Value == nil || strings.Contains(*p.Value, "hunter2") {
		t.Fatalf("stdout = %+v", p)
	}
	if p := values["/inputs.note"]; !p.Redacted || p.Value == nil || !strings.HasPrefix(*p.Value, "uses ") {
		t.Fatalf("input = %+v", p)
	}
}

// A JSON output spells the secret only through escapes, so byte redaction
// cannot see it; the recorded value must still never decode to the secret.
func TestRunRecordedJSONValueNeverDecodesToASecret(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/printf", "/usr/bin/printenv")
	const secret = `q"b\s&é-secret`
	t.Setenv("AP_JSON_SECRET", secret)
	path := filepath.Join(t.TempDir(), "escaped.yaml")
	os.WriteFile(path, []byte(`schema_version: 0
id: escaped-json
inputs: {}
nodes:
  emit:
    type: shell
    config:
      command: /usr/bin/printf
      args: ['%s', '{"v":"q\"b\\s&é-secret"}']
      stdout_json: true
    outputs:
      json: {type: json, required: true}
edges: []
outputs: {}
`), 0o600)
	dir := t.TempDir()
	code, out, errout := cli(t, "run", path, "--redact-env", "AP_JSON_SECRET", "--record-values", "--store-dir", dir, "--json")
	if code != 0 {
		t.Fatalf("code=%d %s", code, errout)
	}
	if strings.Contains(out, secret) {
		t.Fatal("secret reached CLI JSON events")
	}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if data, _ := os.ReadFile(p); strings.Contains(string(data), secret) {
				t.Fatalf("secret reached %s", p)
			}
		}
		return nil
	})
	var doc struct {
		Events []events.Event `json:"events"`
	}
	json.Unmarshal([]byte(out), &doc)
	p, ok := valueEvents(doc.Events)["emit/json"]
	if !ok || !p.Redacted || !p.Truncated {
		t.Fatalf("json = %+v", p)
	}
	if p.Value != nil {
		var decoded struct{ V string }
		if json.Unmarshal([]byte(*p.Value), &decoded) == nil && strings.Contains(decoded.V, secret) {
			t.Fatal("recorded JSON value decodes to the secret")
		}
	}
}

func TestServeWithoutRecordingEmitsNoValues(t *testing.T) {
	t.Setenv("AP_VALUE_SECRET", "not-recorded")
	s := startServe(t)
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(leakyGraph(t))+`,"inputs":["note=hello"]}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal([]byte(body), &started)
	_, stream := s.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	if live := sseEvents(t, stream); len(live) == 0 || strings.Contains(stream, "value_recorded") {
		t.Fatalf("recording off: %s", stream)
	}
	var logs int
	filepath.Walk(s.dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Name() == "events.jsonl" {
			logs++
			if data, _ := os.ReadFile(p); len(data) == 0 || strings.Contains(string(data), "value_recorded") {
				t.Fatalf("%s: %s", p, data)
			}
		}
		return nil
	})
	if logs != 1 {
		t.Fatalf("found %d events.jsonl files, want 1", logs)
	}
}

func TestServeRefusesAnUnsetRedactionVariable(t *testing.T) {
	code, out, _ := cli(t, "serve", "--store-dir", t.TempDir(), "--record-values", "--redact-env", "AP_E6_SURELY_UNSET")
	if code == 0 || strings.Contains(out, "token") {
		t.Fatalf("serve started without its redaction configuration: code=%d %s", code, out)
	}
}

func sseEvents(t *testing.T, stream string) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(stream, "\n") {
		if strings.HasPrefix(line, `data: {"event_schema_version"`) {
			lines = append(lines, line)
		}
	}
	if !strings.Contains(stream, "event: end") {
		t.Fatalf("stream did not end: %s", stream)
	}
	return lines
}

func TestServeRecordsRedactedValuesAndReplaysThemAfterRestart(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/printf", "/usr/bin/printenv")
	t.Setenv("AP_VALUE_SECRET", valueSecret)
	s := startServe(t, "--record-values", "--redact-env", "AP_VALUE_SECRET")
	graph := leakyGraph(t)
	code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(graph)+`,"inputs":["note=uses `+valueSecret+`"]}`)
	if code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal([]byte(body), &started)
	_, stream := s.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	live := sseEvents(t, stream)
	joined := strings.Join(live, "\n")
	if strings.Count(joined, `"type":"value_recorded"`) != 2 || strings.Contains(stream, valueSecret) || !strings.Contains(joined, `"redacted":true`) {
		t.Fatalf("live values: %s", joined)
	}
	if _, list := s.call(t, "GET", "/runs", ""); !strings.Contains(list, `"graph_version":"sha256-v0:`) || strings.Contains(list, valueSecret) {
		t.Fatalf("runs list: %s", list)
	}
	// A graph that embeds the configured secret is refused before a run exists.
	secretGraph := filepath.Join(t.TempDir(), "secret.yaml")
	os.WriteFile(secretGraph, []byte("schema_version: 0\nid: s\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/printf, args: [\""+valueSecret+"\"]}\nedges: []\noutputs: {}\n"), 0o600)
	if code, body := s.call(t, "POST", "/runs", `{"graph_path":`+quote(secretGraph)+`}`); code != http.StatusUnprocessableEntity || strings.Contains(body, valueSecret) {
		t.Fatalf("secret graph: %d %s", code, body)
	}
	entries, _ := os.ReadDir(s.dir)
	runDirs := 0
	for _, entry := range entries {
		if entry.IsDir() {
			runDirs++
		}
	}
	if runDirs != 1 {
		t.Fatalf("store holds %d runs, want 1", runDirs)
	}
	s.closeStdin()
	select {
	case <-s.exit:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not exit")
	}
	assertNoSecretUnder(t, s.dir)
	// A fresh server with recording off still replays the stored values.
	again := startServe(t, "--store-dir", s.dir)
	_, replayed := again.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	if got := strings.Join(sseEvents(t, replayed), "\n"); got != joined {
		t.Fatalf("replay differs from live:\n%s\n---\n%s", got, joined)
	}
}

// F2.3 E5b.1 (requirement 16): the editor offers a run as "same version" by
// comparing the run summary's graph_version with the document API's version, so
// for the same bytes they must be the same string, and the stored run holds at
// most one value event per (node, port).
func TestServeRunSummaryVersionEqualsTheDocumentVersionForTheSameBytes(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh", "/usr/bin/printf")
	s := startServe(t, "--record-values")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "versioned.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 0\nid: versioned\ninputs: {}\nnodes:\n  say:\n    type: shell\n    config: {command: /usr/bin/printf, args: [hello]}\n    outputs:\n      stdout: {type: string, required: true}\nedges: []\noutputs: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var grant struct {
		Grant string `json:"grant"`
	}
	code, body := s.call(t, "POST", "/graphs/grants", `{"path":`+quote(path)+`,"mode":"open"}`)
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &grant) != nil {
		t.Fatalf("grant: %d %s", code, body)
	}
	var doc struct {
		Version string `json:"version"`
	}
	code, body = s.call(t, "POST", "/graphs/open", `{"grant":`+quote(grant.Grant)+`}`)
	if code != http.StatusOK || json.Unmarshal([]byte(body), &doc) != nil || !strings.HasPrefix(doc.Version, "sha256-v0:") {
		t.Fatalf("open: %d %s", code, body)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	code, body = s.call(t, "POST", "/runs", `{"graph_grant":`+quote(grant.Grant)+`}`)
	if code != http.StatusAccepted || json.Unmarshal([]byte(body), &started) != nil {
		t.Fatalf("run: %d %s", code, body)
	}
	_, stream := s.call(t, "GET", "/runs/"+started.RunID+"/events", "")
	keys := map[string]bool{}
	for _, line := range sseEvents(t, stream) {
		var e events.Event
		if err := json.Unmarshal([]byte(line[len("data: "):]), &e); err != nil {
			t.Fatal(err)
		}
		if p, ok := e.Payload.(events.ValueRecordedPayload); ok {
			key := string(e.NodeID) + "\x00" + p.Port
			if keys[key] {
				t.Fatalf("two value events for %q", key)
			}
			keys[key] = true
		}
	}
	if len(keys) != 1 {
		t.Fatalf("value keys = %v, want the one output of say", keys)
	}
	var list struct {
		Runs []struct {
			RunID        string `json:"run_id"`
			GraphVersion string `json:"graph_version"`
		} `json:"runs"`
	}
	_, body = s.call(t, "GET", "/runs", "")
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Runs) != 1 || list.Runs[0].RunID != started.RunID {
		t.Fatalf("runs: %s (%v)", body, err)
	}
	if list.Runs[0].GraphVersion != doc.Version {
		t.Fatalf("run graph_version %q != document version %q for the same bytes", list.Runs[0].GraphVersion, doc.Version)
	}
}
