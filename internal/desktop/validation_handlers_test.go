package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func callDocumentHandler(t *testing.T, handlers map[string]Handler, name, params string) (any, *ServiceError) {
	t.Helper()
	handler, ok := handlers[name]
	if !ok {
		t.Fatalf("missing handler %s", name)
	}
	return handler(context.Background(), json.RawMessage(params))
}

func TestDocumentHandlersOpenValidateAndStrictRequests(t *testing.T) {
	registry, stateDir, projectDir := registryFixture(t)
	projectGrant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(projectGrant.Token, projectGrant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	store := NewDocumentStore(registry, stateDir)
	preflight := NewPreflightService(registry, store, nil)
	handlers := NewDocumentHandlers(store, preflight)
	if _, e := callDocumentHandler(t, handlers, "documents.open", `{"project_id":"`+project.ProjectID+`","relative_path":"../escape"}`); e == nil || e.Code != "invalid_request" {
		t.Fatalf("escape = %#v", e)
	}
	if _, e := callDocumentHandler(t, handlers, "documents.open", `{"project_id":"`+project.ProjectID+`","relative_path":"graph.yaml","extra":1}`); e == nil || e.Code != "invalid_request" {
		t.Fatalf("unknown field = %#v", e)
	}
	if _, e := callDocumentHandler(t, handlers, "graphs.validate", `{"document_id":"00000000-0000-0000-0000-000000000000","revision":1}`); e == nil || e.Code != "not_found" {
		t.Fatalf("missing document = %#v", e)
	}
}

func TestPreflightHandlerRejectsUnknownNestedFieldsAndNulls(t *testing.T) {
	service, doc, workspace, _ := preflightFixture(t, validGraphText)
	handlers := NewDocumentHandlers(service.documents, service)
	base := `{"document_id":"` + doc.DocumentID + `","revision":1,"workspace_id":"` + workspace.WorkspaceID + `","inputs":{},"max_concurrency":1`
	for _, params := range []string{
		base + `,"extra":true}`,
		base + `,"inputs":null}`,
		`{"document_id":"` + doc.DocumentID + `","revision":1,"workspace_id":"` + workspace.WorkspaceID + `","inputs":{"x":{"type":"string","value":"a","extra":1}},"max_concurrency":1}`,
	} {
		if _, e := callDocumentHandler(t, handlers, "runs.preflight", params); e == nil || e.Code != "invalid_request" {
			t.Fatalf("params %s accepted: %#v", params, e)
		}
	}
	result, e := callDocumentHandler(t, handlers, "runs.preflight", base+`}`)
	if e != nil {
		t.Fatalf("valid preflight params error = %#v", e)
	}
	if preflightResult, ok := result.(PreflightResult); !ok || preflightResult.PreflightToken == "" {
		t.Fatalf("preflight result = %#v", result)
	}
}

func TestDocumentHandlersFailClosedOnUnmappableRecoveryIntent(t *testing.T) {
	registry, stateDir, _ := registryFixture(t)
	path := filepath.Join(stateDir, "save-00000000-0000-0000-0000-000000000000.intent")
	if err := os.WriteFile(path, []byte("not an intent"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewDocumentStore(registry, stateDir)
	handlers := NewDocumentHandlers(store, NewPreflightService(registry, store, nil))
	for _, method := range []string{"documents.open", "graphs.validate", "runs.preflight"} {
		if _, e := callDocumentHandler(t, handlers, method, `{}`); e == nil || e.Code != "unavailable" {
			t.Fatalf("%s recovery error = %#v", method, e)
		}
	}
}

func TestDocumentHandlersPreserveDraftAndSavedText(t *testing.T) {
	service, doc, workspace, graphPath := preflightFixture(t, validGraphText)
	handlers := NewDocumentHandlers(service.documents, service)
	update := func(revision uint64, text string) documentDTO {
		t.Helper()
		params, err := json.Marshal(map[string]any{"document_id": doc.DocumentID, "expected_revision": revision,
			"text_base64": base64.StdEncoding.EncodeToString([]byte(text))})
		if err != nil {
			t.Fatal(err)
		}
		result, e := callDocumentHandler(t, handlers, "documents.update", string(params))
		if e != nil {
			t.Fatal(e)
		}
		return result.(documentDTO)
	}
	invalid := update(1, "not a graph: [")
	if invalid.Validity != DocumentInvalid || !invalid.HasErrors || invalid.Diagnostics[0].Code != "E_GRAPH_PARSE" {
		t.Fatalf("invalid draft = %#v", invalid)
	}
	if decoded, err := base64.StdEncoding.DecodeString(invalid.TextBase64); err != nil || string(decoded) != "not a graph: [" {
		t.Fatalf("draft text encoding = %q, %v", decoded, err)
	}
	validation, e := callDocumentHandler(t, handlers, "graphs.validate", `{"document_id":"`+doc.DocumentID+`","revision":2}`)
	if e != nil || !validation.(DiagnosticSummary).HasErrors {
		t.Fatalf("draft validation = %#v, %#v", validation, e)
	}
	if _, e := callDocumentHandler(t, handlers, "runs.preflight", `{"document_id":"`+doc.DocumentID+`","revision":2,"workspace_id":"`+workspace.WorkspaceID+`","inputs":{},"max_concurrency":1}`); e != nil {
		t.Fatalf("invalid draft should return findings: %#v", e)
	}
	text := strings.Replace(validGraphText, "id: local", "id: changed", 1)
	valid := update(2, text)
	if valid.Validity != DocumentValid || valid.Revision != 3 {
		t.Fatalf("valid draft = %#v", valid)
	}
	saved, e := callDocumentHandler(t, handlers, "documents.save", `{"document_id":"`+doc.DocumentID+`","expected_revision":3,"expected_disk_digest":"`+doc.DiskDigest+`","accept_reformat":false}`)
	if e != nil || saved.(documentDTO).DiskDigest == doc.DiskDigest {
		t.Fatalf("saved = %#v, %#v", saved, e)
	}
	data, err := os.ReadFile(graphPath)
	if err != nil || string(data) != text {
		t.Fatalf("saved bytes = %q, %v", data, err)
	}
}

func TestDocumentTransportFitsFrameForMaximallyEscapedText(t *testing.T) {
	text := strings.Repeat("\n", MaxDocumentBytes)
	dto := toDocumentDTO(Document{DocumentID: "id", Text: text, Validity: DocumentInvalid})
	encoded, err := json.Marshal(dto)
	if err != nil || len(encoded) > MaxFrameBytes {
		t.Fatalf("accepted text exceeds frame: %d bytes, %v", len(encoded), err)
	}
	decoded, err := base64.StdEncoding.DecodeString(dto.TextBase64)
	if err != nil || string(decoded) != text {
		t.Fatalf("document text round trip failed: %v", err)
	}
	service, doc, _, _ := preflightFixture(t, validGraphText)
	handlers := NewDocumentHandlers(service.documents, service)
	params, err := json.Marshal(map[string]any{"document_id": doc.DocumentID, "expected_revision": doc.Revision,
		"text_base64": dto.TextBase64})
	if err != nil || len(params) > MaxFrameBytes {
		t.Fatalf("update frame = %d bytes, %v", len(params), err)
	}
	result, e := callDocumentHandler(t, handlers, "documents.update", string(params))
	if e != nil || result.(documentDTO).TextBase64 != dto.TextBase64 {
		t.Fatalf("max text update = %#v, %#v", result, e)
	}
}
