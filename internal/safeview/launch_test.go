package safeview

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestProjectLaunchValuesRedactsNestedStringsAndOmitsCredentialKeys(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := graph.NewJSONValue(json.RawMessage(`{"name":"public-private","nested":{"api_key":"private","safe":"pri\u0076ate"},"items":["private",17]}`))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]graph.Value{"payload": value, "label": graph.NewStringValue("private"), "api_token": graph.NewStringValue("private")}
	projection, complete, err := projector.ProjectLaunchValues(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Fatal("omitted keys must mark launch incomplete")
	}
	got := projection.Bytes()
	if bytes.Contains(got, []byte("private")) || bytes.Contains(got, []byte("api_key")) || bytes.Contains(got, []byte("api_token")) {
		t.Fatalf("unsafe launch projection: %s", got)
	}
	var decoded []any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0] != float64(1) {
		t.Fatalf("expected versioned projection: %s", got)
	}
	if !bytes.Contains(got, []byte(`"items"`)) {
		t.Fatalf("unexpected projection: %s", got)
	}
	if raw, _ := value.AsJSON(); !bytes.Contains(raw, []byte("private")) {
		t.Fatal("input was mutated")
	}
}

func TestProjectLaunchValuesHasNoFixedKeyCollision(t *testing.T) {
	projector, err := NewProjector([]string{"status"})
	if err != nil {
		t.Fatal(err)
	}
	projection, complete, err := projector.ProjectLaunchValues(map[string]graph.Value{"message": graph.NewStringValue("status")})
	if err != nil || complete {
		t.Fatalf("projection=%s complete=%v err=%v", projection.Bytes(), complete, err)
	}
	if bytes.Contains(projection.Bytes(), []byte("status")) {
		t.Fatalf("private value leaked: %s", projection.Bytes())
	}
}

func TestProjectLaunchValuesMarksNestedRedactionIncomplete(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := graph.NewJSONValue(json.RawMessage(`{"safe":"private"}`))
	if err != nil {
		t.Fatal(err)
	}
	projection, complete, err := projector.ProjectLaunchValues(map[string]graph.Value{"payload": value})
	if err != nil || complete {
		t.Fatalf("projection=%s complete=%v err=%v", projection.Bytes(), complete, err)
	}
}

func TestProjectLaunchValuesRejectsUnrepresentableJSON(t *testing.T) {
	projector, err := NewProjector([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"duplicate": `{"name":1,"name":2}`,
		"too_deep":  strings.Repeat("[", 65) + "1" + strings.Repeat("]", 65),
	} {
		t.Run(name, func(t *testing.T) {
			value, err := graph.NewJSONValue(json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := projector.ProjectLaunchValues(map[string]graph.Value{"value": value}); err == nil {
				t.Fatal("expected bounded parse failure")
			}
		})
	}
}

func TestCredentialShapedKeyNormalization(t *testing.T) {
	for _, key := range []string{"api_key", "PRIVATE-KEY", "Auth_Token", "credentials"} {
		if !CredentialShapedKey(key) {
			t.Fatalf("missed %q", key)
		}
	}
	if CredentialShapedKey("ordinary_name") {
		t.Fatal("ordinary key classified as credential")
	}
}
