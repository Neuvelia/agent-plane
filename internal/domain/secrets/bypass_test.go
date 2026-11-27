package secrets

import (
	"reflect"
	"testing"
)

func TestAmbientBypassesFindOnlyUnassignedNodesNamingTheBackingVariable(t *testing.T) {
	refs := map[string]Ref{
		"gh_token": {Provider: "env", Name: "GITHUB_TOKEN"},
		"later":    {Provider: "local", Name: "GITHUB_TOKEN"}, // not env: no backing variable
	}
	nodes := []NodeChannels{
		{NodeID: "assigned", Assigned: []string{"gh_token"}, Names: []string{"github_token"}},
		{NodeID: "thief", Names: []string{"PATH", "github_token"}},
		{NodeID: "clean", Names: []string{"HOME"}},
		{NodeID: "other_secret", Assigned: []string{"later"}, Names: []string{"GITHUB_TOKEN"}},
	}
	got := AmbientBypasses(refs, nodes)
	want := []Bypass{
		{NodeID: "other_secret", Secret: "gh_token", Variable: "GITHUB_TOKEN"},
		{NodeID: "thief", Secret: "gh_token", Variable: "github_token"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bypasses = %+v, want %+v", got, want)
	}
}

func TestAmbientBypassesAreDeterministicAndEmptyWithoutSecrets(t *testing.T) {
	if got := AmbientBypasses(nil, []NodeChannels{{NodeID: "n", Names: []string{"X"}}}); got != nil {
		t.Fatalf("no secrets, got %+v", got)
	}
	refs := map[string]Ref{"b": {Provider: "env", Name: "TOK"}, "a": {Provider: "env", Name: "TOK"}}
	nodes := []NodeChannels{{NodeID: "z", Names: []string{"TOK"}}, {NodeID: "y", Names: []string{"tok"}}}
	first := AmbientBypasses(refs, nodes)
	for i := 0; i < 20; i++ {
		if again := AmbientBypasses(refs, nodes); !reflect.DeepEqual(first, again) {
			t.Fatalf("order changed: %+v vs %+v", first, again)
		}
	}
	if len(first) != 4 || first[0].NodeID != "y" || first[0].Secret != "a" || first[3].NodeID != "z" || first[3].Secret != "b" {
		t.Fatalf("unexpected order: %+v", first)
	}
}

func TestAmbientBypassesReportAVariableASectionNamesTwiceOnce(t *testing.T) {
	refs := map[string]Ref{"tok": {Provider: ProviderEnv, Name: "TOK"}}
	nodes := []NodeChannels{{NodeID: "n", Section: "config", Names: []string{"TOK", "TOK"}}}
	if got := AmbientBypasses(refs, nodes); len(got) != 1 || got[0].Section != "config" {
		t.Fatalf("got %+v", got)
	}
}
