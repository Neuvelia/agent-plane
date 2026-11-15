package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/api"
	"agentplane/internal/fields"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
	"agentplane/internal/shell"
)

func catalogueByType(t *testing.T) map[string]api.NodeType {
	t.Helper()
	out := map[string]api.NodeType{}
	for _, nt := range nodeCatalogue() {
		if _, dup := out[nt.Type]; dup {
			t.Fatalf("type %q listed twice", nt.Type)
		}
		out[nt.Type] = nt
	}
	return out
}

func kindEnum(t *testing.T, fs []fields.Field) []string {
	t.Helper()
	for _, f := range fs {
		if f.Key == "kind" {
			return f.Enum
		}
	}
	t.Fatal("no kind field")
	return nil
}

func groupIDs(fs []fields.Field) []string {
	var ids []string
	for _, f := range fs {
		if f.Kind == fields.Group {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

func TestCatalogueComposesTheValidatorsDescriptors(t *testing.T) {
	c := catalogueByType(t)
	if len(c) != 4 {
		t.Fatalf("types: %v", c)
	}
	if !reflect.DeepEqual(c["shell"].Config, shell.ConfigFields()) || !reflect.DeepEqual(c["shell"].Outputs, shell.OutputPorts()) {
		t.Error("shell descriptors are not the executor's")
	}
	if !reflect.DeepEqual(c["agent"].Config, agent.ConfigFields()) || !reflect.DeepEqual(c["agent"].Outputs, agent.OutputPorts()) {
		t.Error("agent descriptors are not the executor's")
	}
	if !reflect.DeepEqual(c["human"].Config, graph.HumanConfigFields()) || !reflect.DeepEqual(c["human"].Outputs, graph.HumanOutputPorts()) {
		t.Error("human descriptors are not the graph package's")
	}
	if !reflect.DeepEqual(c["gate"].Outputs, gate.OutputPorts()) {
		t.Error("gate outputs are not the executor's")
	}
	for name, nt := range c {
		if nt.OutputsLocked != (name == "human") {
			t.Errorf("%s: outputs locked = %v", name, nt.OutputsLocked)
		}
		if (nt.Loop != nil) != graph.LoopProducer(name) {
			t.Errorf("%s: loop offered = %v", name, nt.Loop != nil)
		}
		if nt.Loop != nil {
			if !reflect.DeepEqual(nt.Loop.GateKinds, graph.LoopGateKinds(name)) || !reflect.DeepEqual(kindEnum(t, nt.Loop.Gate), graph.LoopGateKinds(name)) {
				t.Errorf("%s: loop gate kinds %v / enum %v", name, nt.Loop.GateKinds, kindEnum(t, nt.Loop.Gate))
			}
			if !reflect.DeepEqual(nt.Loop.Fields, graph.LoopFields()) {
				t.Errorf("%s: loop fields are not the graph package's", name)
			}
		}
	}
}

func TestStandaloneGateOffersThreeKinds(t *testing.T) {
	g := catalogueByType(t)["gate"]
	want := []string{"command", "json_predicate", "reported_file_scope"}
	if got := kindEnum(t, g.Config); !reflect.DeepEqual(got, want) {
		t.Errorf("standalone kinds %v", got)
	}
	if got := groupIDs(g.Config); !reflect.DeepEqual(got, []string{"gate.config.command", "gate.config.json_predicate", "gate.config.reported_file_scope"}) {
		t.Errorf("standalone groups %v", got)
	}
	if got := groupIDs(catalogueByType(t)["agent"].Loop.Gate); len(got) != 4 {
		t.Errorf("agent loop groups %v", got)
	}
	// Narrowing changes nothing but the kind enum and the dropped group.
	byID := map[string]fields.Field{}
	for _, f := range gate.ConfigFields() {
		byID[f.ID] = f
	}
	for _, f := range g.Config {
		if f.Key == "kind" {
			continue
		}
		if !reflect.DeepEqual(f, byID[f.ID]) {
			t.Errorf("%s changed by narrowing", f.ID)
		}
	}
}

func TestServeAnswersWithTheCatalogue(t *testing.T) {
	s := startServe(t)
	path := filepath.Join(s.dir, "g.yaml")
	text := "schema_version: 0\nid: g\ninputs: {}\nnodes:\n  a:\n    type: shell\n    config: {command: /usr/bin/true, mystery: {x: 1}}\nedges: []\noutputs: {}\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	status, body := s.call(t, "POST", "/graphs/grants", `{"path":`+jsonString(path)+`,"mode":"open"}`)
	if status != 201 {
		t.Fatalf("grant: %d %s", status, body)
	}
	var grant struct{ Grant string }
	_ = json.Unmarshal([]byte(body), &grant)
	for _, all := range []bool{false, true} {
		req, _ := json.Marshal(map[string]any{"grant": grant.Grant, "all_node_types": all})
		status, body = s.call(t, "POST", "/graphs/open", string(req))
		var doc struct {
			NodeTypes map[string]json.RawMessage `json:"node_types"`
			NodeInfo  map[string]struct {
				Advanced []struct{ Key, YAML, Reason string }
			} `json:"node_info"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil || status != 200 {
			t.Fatalf("open: %d %s", status, body)
		}
		want := 1
		if all {
			want = 4
		}
		if len(doc.NodeTypes) != want {
			t.Errorf("all=%v: %d node types", all, len(doc.NodeTypes))
		}
		adv := doc.NodeInfo["a"].Advanced
		if len(adv) != 1 || adv[0].Key != "mystery" || adv[0].YAML != "{x: 1}" || adv[0].Reason != "unknown" {
			t.Errorf("advanced: %+v", adv)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
