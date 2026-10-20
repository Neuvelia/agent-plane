package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

var ErrInvalidSettings = errors.New("invalid local settings")
var ErrToolUnavailable = errors.New("configured tool unavailable")

type LocalSettings struct {
	Revision             uint64  `json:"revision"`
	Theme                string  `json:"theme"`
	CodexBinaryPath      *string `json:"codex_binary_path,omitempty"`
	GitBinaryPath        *string `json:"git_binary_path,omitempty"`
	MaxConcurrency       int     `json:"max_concurrency"`
	HistoryRetentionDays *int    `json:"history_retention_days,omitempty"`
	KeepAllHistory       bool    `json:"keep_all_history"`
}

type OptionalPath struct {
	Set   bool
	Value *string
}

type SettingsPatch struct {
	Theme                *string
	CodexBinaryPath      OptionalPath
	GitBinaryPath        OptionalPath
	MaxConcurrency       *int
	HistoryRetentionDays *int
	KeepAllHistory       *bool
}

func defaultSettings() LocalSettings {
	days := 30
	return LocalSettings{Revision: 1, Theme: "dark", MaxConcurrency: 4, HistoryRetentionDays: &days}
}

func cloneSettings(settings LocalSettings) LocalSettings {
	if settings.CodexBinaryPath != nil {
		value := *settings.CodexBinaryPath
		settings.CodexBinaryPath = &value
	}
	if settings.GitBinaryPath != nil {
		value := *settings.GitBinaryPath
		settings.GitBinaryPath = &value
	}
	if settings.HistoryRetentionDays != nil {
		value := *settings.HistoryRetentionDays
		settings.HistoryRetentionDays = &value
	}
	return settings
}

func applySettingsPatch(current LocalSettings, patch SettingsPatch) (LocalSettings, error) {
	if current.Revision == ^uint64(0) {
		return LocalSettings{}, ErrInvalidSettings
	}
	next := cloneSettings(current)
	if patch.Theme != nil {
		next.Theme = *patch.Theme
	}
	if patch.CodexBinaryPath.Set {
		if patch.CodexBinaryPath.Value != nil {
			canonical, err := validateToolPath(*patch.CodexBinaryPath.Value)
			if err != nil {
				return LocalSettings{}, err
			}
			next.CodexBinaryPath = &canonical
		} else {
			next.CodexBinaryPath = nil
		}
	}
	if patch.GitBinaryPath.Set {
		if patch.GitBinaryPath.Value != nil {
			canonical, err := validateToolPath(*patch.GitBinaryPath.Value)
			if err != nil {
				return LocalSettings{}, err
			}
			next.GitBinaryPath = &canonical
		} else {
			next.GitBinaryPath = nil
		}
	}
	if patch.MaxConcurrency != nil {
		next.MaxConcurrency = *patch.MaxConcurrency
	}
	if patch.KeepAllHistory != nil && *patch.KeepAllHistory && patch.HistoryRetentionDays != nil {
		return LocalSettings{}, ErrInvalidSettings
	}
	if patch.HistoryRetentionDays != nil {
		if next.KeepAllHistory && (patch.KeepAllHistory == nil || *patch.KeepAllHistory) {
			return LocalSettings{}, ErrInvalidSettings
		}
		value := *patch.HistoryRetentionDays
		next.HistoryRetentionDays = &value
	}
	if patch.KeepAllHistory != nil {
		next.KeepAllHistory = *patch.KeepAllHistory
		if next.KeepAllHistory {
			next.HistoryRetentionDays = nil
		} else if next.HistoryRetentionDays == nil {
			days := 30
			next.HistoryRetentionDays = &days
		}
	}
	if err := validateSettings(&next); err != nil {
		return LocalSettings{}, err
	}
	next.Revision++
	return next, nil
}

func validateSettings(settings *LocalSettings) error {
	if settings.Revision == 0 || settings.Theme != "dark" || settings.MaxConcurrency < 1 || settings.MaxConcurrency > 32 ||
		settings.KeepAllHistory == (settings.HistoryRetentionDays != nil) {
		return ErrInvalidSettings
	}
	if settings.HistoryRetentionDays != nil && (*settings.HistoryRetentionDays < 1 || *settings.HistoryRetentionDays > 3650) {
		return ErrInvalidSettings
	}
	for _, selected := range []*string{settings.CodexBinaryPath, settings.GitBinaryPath} {
		if selected != nil && (*selected == "" || len(*selected) > MaxRelativePathBytes ||
			!utf8.ValidString(*selected) || strings.ContainsAny(*selected, "\x00\n\r") ||
			!filepath.IsAbs(*selected) || filepath.Clean(*selected) != *selected) {
			return ErrInvalidSettings
		}
	}
	return nil
}

func validateToolPath(selected string) (string, error) {
	if selected == "" || len(selected) > MaxRelativePathBytes || !utf8.ValidString(selected) ||
		strings.ContainsAny(selected, "\x00\n\r") || !filepath.IsAbs(selected) {
		return "", ErrInvalidSettings
	}
	canonical, err := filepath.EvalSymlinks(selected)
	if err != nil {
		return "", ErrToolUnavailable
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrToolUnavailable
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(filepath.Ext(canonical), ".exe") {
			return "", ErrToolUnavailable
		}
	} else if !toolIsExecutable(canonical) {
		return "", ErrToolUnavailable
	}
	return canonical, nil
}

type ToolStatus struct {
	ToolID     string `json:"tool_id"`
	Status     string `json:"status"`
	Path       string `json:"path,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

func detectToolPath(settings LocalSettings, toolID string) (ToolStatus, error) {
	var selected *string
	switch toolID {
	case "git":
		selected = settings.GitBinaryPath
	case "codex":
		selected = settings.CodexBinaryPath
	default:
		return ToolStatus{}, ErrInvalidSettings
	}
	if selected == nil {
		return ToolStatus{ToolID: toolID, Status: "unavailable", Diagnostic: "No executable configured"}, nil
	}
	canonical, err := validateToolPath(*selected)
	if err != nil {
		return ToolStatus{ToolID: toolID, Status: "unavailable", Diagnostic: "Configured executable is unavailable"}, nil
	}
	return ToolStatus{ToolID: toolID, Status: "unverified", Path: canonical,
		Diagnostic: "Executable version and capability have not been checked"}, nil
}
