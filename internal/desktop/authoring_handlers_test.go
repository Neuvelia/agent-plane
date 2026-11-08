package desktop

import (
	"encoding/json"
	"testing"
)

func TestAuthoringHandlersStrictRequestsAndRevision(t *testing.T) {
	registry, stateDir, projectDir := registryFixture(t)
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	store := NewDocumentStore(registry, stateDir)
	handlers := NewDocumentHandlers(store, NewPreflightService(registry, store, nil))
	created, e := callDocumentHandler(t, handlers, "documents.create", `{"project_id":"`+project.ProjectID+`","relative_path":"new.yaml","graph_id":"new-graph"}`)
	if e != nil {
		t.Fatal(e)
	}
	view := created.(documentDTO)
	if view.Revision != 1 || view.Validity != DocumentValid {
		t.Fatalf("created = %#v", view)
	}
	for _, call := range []struct{ name, params string }{
		{"documents.create", `{"project_id":"project-1","relative_path":".agents/graphs/other.yaml","graph_id":"new","extra":1}`},
		{"documents.reload", `{"document_id":"` + view.DocumentID + `","expected_revision":1}`},
		{"documents.inspect", `{"document_id":"` + view.DocumentID + `","revision":0}`},
		{"documents.preview_edit", `{"document_id":"` + view.DocumentID + `","revision":1,"operation":{"kind":"add_node","node_id":"first","node_type":"shell","extra":1}}`},
	} {
		if _, e := callDocumentHandler(t, handlers, call.name, call.params); e == nil || e.Code != "invalid_request" {
			t.Fatalf("%s accepted invalid params: %#v", call.name, e)
		}
	}
	inspection, e := callDocumentHandler(t, handlers, "documents.inspect", `{"document_id":"`+view.DocumentID+`","revision":1}`)
	if e != nil || inspection.(AuthoringInspection).Graph == nil {
		t.Fatalf("inspection = %#v, %#v", inspection, e)
	}
	preview, e := callDocumentHandler(t, handlers, "documents.preview_edit", `{"document_id":"`+view.DocumentID+`","revision":1,"operation":{"kind":"add_node","node_id":"first","node_type":"shell"}}`)
	if e != nil || preview.(AuthoringPreview).TextBase64 == "" {
		t.Fatalf("preview = %#v, %#v", preview, e)
	}
	encoded, _ := json.Marshal(preview)
	if len(encoded) > maxAuthoringResultBytes {
		t.Fatalf("preview size = %d", len(encoded))
	}
}
