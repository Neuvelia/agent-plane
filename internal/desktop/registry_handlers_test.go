package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func callRegistryHandler(t *testing.T, handlers map[string]Handler, method, params string) (json.RawMessage, *ServiceError) {
	t.Helper()
	result, serviceErr := handlers[method](context.Background(), json.RawMessage(params))
	if serviceErr != nil {
		return nil, serviceErr
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, nil
}

func addProjectViaHandlers(t *testing.T, handlers map[string]Handler, path string) projectDTO {
	t.Helper()
	selected, _ := json.Marshal(struct {
		CanonicalPath string `json:"canonical_path"`
	}{path})
	granted, serviceErr := callRegistryHandler(t, handlers, "projects.grant", string(selected))
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	var grant ProjectGrant
	if err := json.Unmarshal(granted, &grant); err != nil {
		t.Fatal(err)
	}
	added, serviceErr := callRegistryHandler(t, handlers, "projects.add", fmt.Sprintf(`{"grant_token":%q,"path_digest":%q}`, grant.Token, grant.PathDigest))
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	var project projectDTO
	if err := json.Unmarshal(added, &project); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestRegistryHandlersRejectArbitraryPathsAndUnknownFields(t *testing.T) {
	registry, _, projectDir := registryFixture(t)
	handlers, err := NewRegistryHandlers(registry)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{
		fmt.Sprintf(`{"canonical_path":%q}`, projectDir),
		`{"grant_token":"x","path_digest":"y","unexpected":true}`,
		`{"grant_token":null,"path_digest":"y"}`,
	} {
		if _, serviceErr := callRegistryHandler(t, handlers, "projects.add", request); serviceErr == nil || serviceErr.Code != "invalid_request" {
			t.Fatalf("arbitrary add %s = %v", request, serviceErr)
		}
	}
	if _, serviceErr := callRegistryHandler(t, handlers, "projects.grant", `{"canonical_path":"../outside"}`); serviceErr == nil || serviceErr.Code != "invalid_request" {
		t.Fatalf("relative folder grant = %v", serviceErr)
	}
	if _, serviceErr := callRegistryHandler(t, handlers, "settings.get", `{"path":"/tmp"}`); serviceErr == nil || serviceErr.Code != "invalid_request" {
		t.Fatalf("arbitrary settings read = %v", serviceErr)
	}
}

func TestRegistryHandlersPaginationAndStaleCursor(t *testing.T) {
	registry, _, firstDir := registryFixture(t)
	handlers, err := NewRegistryHandlers(registry)
	if err != nil {
		t.Fatal(err)
	}
	_ = addProjectViaHandlers(t, handlers, firstDir)
	secondDir := filepath.Join(t.TempDir(), "second")
	if err := os.Mkdir(secondDir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = addProjectViaHandlers(t, handlers, secondDir)
	page, serviceErr := callRegistryHandler(t, handlers, "projects.list", `{"limit":1}`)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	var first struct {
		Projects   []projectDTO `json:"projects"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.Unmarshal(page, &first); err != nil || len(first.Projects) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %s, %v", page, err)
	}
	query := fmt.Sprintf(`{"limit":1,"cursor":%q}`, first.NextCursor)
	page, serviceErr = callRegistryHandler(t, handlers, "projects.list", query)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	var second struct {
		Projects []projectDTO `json:"projects"`
	}
	if err := json.Unmarshal(page, &second); err != nil || len(second.Projects) != 1 || second.Projects[0].ProjectID == first.Projects[0].ProjectID {
		t.Fatalf("second page = %s, %v", page, err)
	}
	if _, serviceErr := callRegistryHandler(t, handlers, "projects.list", fmt.Sprintf(`{"cursor":%q}`, first.NextCursor+"x")); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("tampered cursor = %v", serviceErr)
	}
	maxConcurrency := 5
	if _, err := registry.UpdateSettings(1, SettingsPatch{MaxConcurrency: &maxConcurrency}); err != nil {
		t.Fatal(err)
	}
	if _, serviceErr := callRegistryHandler(t, handlers, "projects.list", query); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("cursor after settings-only change = %v", serviceErr)
	}
	thirdDir := filepath.Join(t.TempDir(), "third")
	if err := os.Mkdir(thirdDir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = addProjectViaHandlers(t, handlers, thirdDir)
	if _, serviceErr := callRegistryHandler(t, handlers, "projects.list", query); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("stale cursor = %v", serviceErr)
	}
}

func TestRegistryHandlersSettingsAndToolStatus(t *testing.T) {
	registry, _, _ := registryFixture(t)
	handlers, err := NewRegistryHandlers(registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, serviceErr := callRegistryHandler(t, handlers, "settings.update", `{"expected_revision":1,"changes":{"theme":"light"}}`); serviceErr == nil || serviceErr.Code != "invalid_request" {
		t.Fatalf("unsupported theme = %v", serviceErr)
	}
	result, serviceErr := callRegistryHandler(t, handlers, "settings.update", `{"expected_revision":1,"changes":{"git_binary_path":null,"keep_all_history":true}}`)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	var settings LocalSettings
	if err := json.Unmarshal(result, &settings); err != nil || settings.Revision != 2 || settings.HistoryRetentionDays != nil {
		t.Fatalf("settings = %s, %v", result, err)
	}
	if _, serviceErr := callRegistryHandler(t, handlers, "settings.update", `{"expected_revision":2,"changes":{"history_retention_days":10}}`); serviceErr == nil || serviceErr.Code != "invalid_request" {
		t.Fatalf("retention conflict = %v", serviceErr)
	}
	tool, serviceErr := callRegistryHandler(t, handlers, "tools.detect", `{"tool_id":"codex"}`)
	if serviceErr != nil || string(tool) == "" {
		t.Fatalf("tool status = %s, %v", tool, serviceErr)
	}
	var status ToolStatus
	if err := json.Unmarshal(tool, &status); err != nil || status.Status != "unavailable" {
		t.Fatalf("tool status = %s, %v", tool, err)
	}
}
