package desktop

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type projectDTO struct {
	ProjectID     string       `json:"project_id"`
	DisplayName   string       `json:"display_name"`
	CanonicalPath string       `json:"canonical_path"`
	Availability  Availability `json:"availability"`
}

type workspaceDTO struct {
	WorkspaceID  string       `json:"workspace_id"`
	ProjectID    string       `json:"project_id"`
	RelativePath string       `json:"relative_path"`
	Availability Availability `json:"availability"`
}

type listCursor struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Digest  string `json:"digest"`
	Offset  int    `json:"offset"`
}

type registryHandlerSet struct {
	registry *Registry
	secret   [32]byte
}

func NewRegistryHandlers(registry *Registry) (map[string]Handler, error) {
	if registry == nil {
		return nil, ErrRegistryUnavailable
	}
	set := &registryHandlerSet{registry: registry}
	if _, err := rand.Read(set.secret[:]); err != nil {
		return nil, err
	}
	return map[string]Handler{
		"projects.grant":  set.projectsGrant,
		"projects.add":    set.projectsAdd,
		"projects.list":   set.projectsList,
		"workspaces.add":  set.workspacesAdd,
		"workspaces.list": set.workspacesList,
		"settings.get":    set.settingsGet,
		"settings.update": set.settingsUpdate,
		"tools.detect":    set.toolsDetect,
	}, nil
}

func strictParams(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, *ServiceError) {
	fields, err := strictObject(raw, allowed...)
	if err != nil {
		return nil, protocolError("invalid_request", "Invalid method parameters")
	}
	return fields, nil
}

func registryError(err error) *ServiceError {
	switch {
	case errors.Is(err, ErrRegistryConflict), errors.Is(err, ErrDocumentConflict):
		return protocolError("conflict", "Registry state changed or grant is invalid")
	case errors.Is(err, ErrRegistryNotFound):
		return protocolError("not_found", "Registered item not found")
	case errors.Is(err, ErrRegistryBusy):
		return protocolError("busy", "Local registry is in use")
	case errors.Is(err, ErrInvalidDocumentPath), errors.Is(err, ErrInvalidSettings):
		return protocolError("invalid_request", "Invalid local path or setting")
	case errors.Is(err, ErrToolUnavailable):
		return protocolError("unavailable", "Configured executable is unavailable")
	case errors.Is(err, ErrRegistryInDoubt):
		return protocolError("unavailable", "Registry commit requires service restart")
	default:
		return protocolError("unavailable", "Local registry is unavailable")
	}
}

func (h *registryHandlerSet) projectsGrant(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "canonical_path")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var selected string
	if requiredField(fields, "canonical_path", &selected) != nil || !validProjectPath(selected) {
		return nil, protocolError("invalid_request", "Invalid selected folder")
	}
	grant, err := h.registry.GrantProjectFolder(selected)
	if err != nil {
		return nil, registryError(err)
	}
	return grant, nil
}

func (h *registryHandlerSet) projectsAdd(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "grant_token", "path_digest")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var token, digest string
	if requiredField(fields, "grant_token", &token) != nil || requiredField(fields, "path_digest", &digest) != nil ||
		len(token) != 64 || !validDigest(digest) {
		return nil, protocolError("invalid_request", "Invalid folder grant")
	}
	project, err := h.registry.AddProject(token, digest)
	if err != nil {
		return nil, registryError(err)
	}
	return toProjectDTO(project), nil
}

func toProjectDTO(project ProjectRecord) projectDTO {
	return projectDTO{project.ProjectID, project.DisplayName, project.CanonicalPath, project.Availability}
}

func toWorkspaceDTO(workspace WorkspaceRecord) workspaceDTO {
	return workspaceDTO{workspace.WorkspaceID, workspace.ProjectID, workspace.RelativePath, workspace.Availability}
}

func listArgs(fields map[string]json.RawMessage) (int, string, *ServiceError) {
	limit := 100
	if raw, ok := fields["limit"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &limit) != nil || limit < 1 || limit > 100 {
			return 0, "", protocolError("invalid_request", "Invalid list limit")
		}
	}
	var cursor string
	if raw, ok := fields["cursor"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &cursor) != nil || cursor == "" || len(cursor) > 512 {
			return 0, "", protocolError("invalid_request", "Invalid list cursor")
		}
	}
	return limit, cursor, nil
}

func (h *registryHandlerSet) projectsList(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "limit", "cursor")
	if serviceErr != nil {
		return nil, serviceErr
	}
	limit, cursor, serviceErr := listArgs(fields)
	if serviceErr != nil {
		return nil, serviceErr
	}
	projects, err := h.registry.ListProjects()
	if err != nil {
		return nil, registryError(err)
	}
	items := make([]projectDTO, len(projects))
	for i, project := range projects {
		items[i] = toProjectDTO(project)
	}
	page, next, serviceErr := pageRegistryItems(h, "projects", "", items, limit, cursor)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return struct {
		Projects   []projectDTO `json:"projects"`
		NextCursor string       `json:"next_cursor,omitempty"`
	}{page, next}, nil
}

func (h *registryHandlerSet) workspacesAdd(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "project_id", "relative_path")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var projectID, relative string
	if requiredField(fields, "project_id", &projectID) != nil || !validSaveID(projectID) ||
		requiredField(fields, "relative_path", &relative) != nil || !validWorkspaceRelative(relative) {
		return nil, protocolError("invalid_request", "Invalid workspace registration")
	}
	workspace, err := h.registry.AddWorkspace(projectID, relative)
	if err != nil {
		return nil, registryError(err)
	}
	return toWorkspaceDTO(workspace), nil
}

func (h *registryHandlerSet) workspacesList(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "project_id", "limit", "cursor")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var projectID string
	if requiredField(fields, "project_id", &projectID) != nil || !validSaveID(projectID) {
		return nil, protocolError("invalid_request", "Invalid project identity")
	}
	limit, cursor, serviceErr := listArgs(fields)
	if serviceErr != nil {
		return nil, serviceErr
	}
	workspaces, err := h.registry.ListWorkspaces(projectID)
	if err != nil {
		return nil, registryError(err)
	}
	items := make([]workspaceDTO, len(workspaces))
	for i, workspace := range workspaces {
		items[i] = toWorkspaceDTO(workspace)
	}
	page, next, serviceErr := pageRegistryItems(h, "workspaces", projectID, items, limit, cursor)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return struct {
		Workspaces []workspaceDTO `json:"workspaces"`
		NextCursor string         `json:"next_cursor,omitempty"`
	}{page, next}, nil
}

func (h *registryHandlerSet) settingsGet(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if _, serviceErr := strictParams(raw); serviceErr != nil {
		return nil, serviceErr
	}
	settings, err := h.registry.Settings()
	if err != nil {
		return nil, registryError(err)
	}
	return settings, nil
}

func (h *registryHandlerSet) settingsUpdate(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "expected_revision", "changes")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var revision uint64
	if requiredField(fields, "expected_revision", &revision) != nil || revision == 0 {
		return nil, protocolError("invalid_request", "Invalid settings revision")
	}
	changes, err := strictObject(fields["changes"], "theme", "codex_binary_path", "git_binary_path", "max_concurrency", "history_retention_days", "keep_all_history")
	if err != nil || len(changes) == 0 {
		return nil, protocolError("invalid_request", "Invalid settings changes")
	}
	patch, err := decodeSettingsPatch(changes)
	if err != nil {
		return nil, protocolError("invalid_request", "Invalid settings changes")
	}
	updated, err := h.registry.UpdateSettings(revision, patch)
	if err != nil {
		return nil, registryError(err)
	}
	return updated, nil
}

func decodeSettingsPatch(fields map[string]json.RawMessage) (SettingsPatch, error) {
	var patch SettingsPatch
	if raw, ok := fields["theme"]; ok {
		var value string
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return patch, ErrInvalidSettings
		}
		patch.Theme = &value
	}
	for _, name := range []string{"codex_binary_path", "git_binary_path"} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		optional := OptionalPath{Set: true}
		if string(raw) != "null" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return patch, ErrInvalidSettings
			}
			optional.Value = &value
		}
		if name == "codex_binary_path" {
			patch.CodexBinaryPath = optional
		} else {
			patch.GitBinaryPath = optional
		}
	}
	if raw, ok := fields["max_concurrency"]; ok {
		var value int
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return patch, ErrInvalidSettings
		}
		patch.MaxConcurrency = &value
	}
	if raw, ok := fields["history_retention_days"]; ok {
		var value int
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return patch, ErrInvalidSettings
		}
		patch.HistoryRetentionDays = &value
	}
	if raw, ok := fields["keep_all_history"]; ok {
		var value bool
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return patch, ErrInvalidSettings
		}
		patch.KeepAllHistory = &value
	}
	return patch, nil
}

func (h *registryHandlerSet) toolsDetect(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	fields, serviceErr := strictParams(raw, "tool_id")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var toolID string
	if requiredField(fields, "tool_id", &toolID) != nil || toolID != "git" && toolID != "codex" {
		return nil, protocolError("invalid_request", "Unknown local tool")
	}
	status, err := h.registry.DetectTool(toolID)
	if err != nil {
		return nil, registryError(err)
	}
	return status, nil
}

func pageRegistryItems[T any](h *registryHandlerSet, kind, project string, items []T, limit int, token string) ([]T, string, *ServiceError) {
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, "", protocolError("internal", "Could not encode list")
	}
	stateDigest, err := h.registry.StateDigest()
	if err != nil {
		return nil, "", registryError(err)
	}
	digest := digestBytes(append(append(encoded, 0), stateDigest...))
	offset := 0
	if token != "" {
		cursor, err := h.decodeCursor(token)
		if err != nil || cursor.Kind != kind || cursor.Project != project || cursor.Digest != digest ||
			cursor.Offset < 1 || cursor.Offset >= len(items) {
			return nil, "", protocolError("conflict", "List cursor is invalid or stale")
		}
		offset = cursor.Offset
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	page := append([]T(nil), items[offset:end]...)
	if page == nil {
		page = []T{}
	}
	if end == len(items) {
		return page, "", nil
	}
	next, err := h.encodeCursor(listCursor{Kind: kind, Project: project, Digest: digest, Offset: end})
	if err != nil {
		return nil, "", protocolError("internal", "Could not encode cursor")
	}
	return page, next, nil
}

func (h *registryHandlerSet) encodeCursor(cursor listCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, h.secret[:])
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (h *registryHandlerSet) decodeCursor(token string) (listCursor, error) {
	var cursor listCursor
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 512 {
		return cursor, ErrRegistryConflict
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(data) > 256 {
		return cursor, ErrRegistryConflict
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return cursor, ErrRegistryConflict
	}
	mac := hmac.New(sha256.New, h.secret[:])
	_, _ = mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return cursor, ErrRegistryConflict
	}
	fields, err := strictObject(data, "kind", "project", "digest", "offset")
	if err != nil || requiredField(fields, "kind", &cursor.Kind) != nil ||
		requiredField(fields, "digest", &cursor.Digest) != nil || requiredField(fields, "offset", &cursor.Offset) != nil {
		return cursor, ErrRegistryConflict
	}
	if raw, ok := fields["project"]; ok && json.Unmarshal(raw, &cursor.Project) != nil {
		return cursor, ErrRegistryConflict
	}
	return cursor, nil
}
