package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSettingsRevisionPathsAndRestart(t *testing.T) {
	registry, stateDir, _ := registryFixture(t)
	initial, err := registry.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if initial.Revision != 1 || initial.Theme != "dark" || initial.MaxConcurrency != 4 || initial.HistoryRetentionDays == nil || *initial.HistoryRetentionDays != 30 {
		t.Fatalf("defaults = %#v", initial)
	}
	tool := filepath.Join(t.TempDir(), "tool with spaces")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(tool, []byte("tool"), 0700); err != nil {
		t.Fatal(err)
	}
	updated, err := registry.UpdateSettings(1, SettingsPatch{GitBinaryPath: OptionalPath{Set: true, Value: &tool}})
	if err != nil || updated.Revision != 2 || updated.GitBinaryPath == nil {
		t.Fatalf("updated settings = %#v, %v", updated, err)
	}
	if _, err := registry.UpdateSettings(1, SettingsPatch{}); !errors.Is(err, ErrRegistryConflict) {
		t.Fatalf("stale revision = %v", err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.GitBinaryPath == nil || *got.GitBinaryPath != updatedPath(updated.GitBinaryPath) {
		t.Fatalf("settings after restart = %#v", got)
	}
	if err := os.Remove(tool); err != nil {
		t.Fatal(err)
	}
	status, err := reopened.DetectTool("git")
	if err != nil || status.Status != "unavailable" {
		t.Fatalf("missing tool status = %#v, %v", status, err)
	}
	cleared, err := reopened.UpdateSettings(2, SettingsPatch{GitBinaryPath: OptionalPath{Set: true}})
	if err != nil || cleared.GitBinaryPath != nil || cleared.Revision != 3 {
		t.Fatalf("clear path = %#v, %v", cleared, err)
	}
}

func updatedPath(value *string) string { return *value }

func TestSettingsRetentionMutualExclusion(t *testing.T) {
	registry, _, _ := registryFixture(t)
	keep := true
	days := 10
	if _, err := registry.UpdateSettings(1, SettingsPatch{KeepAllHistory: &keep, HistoryRetentionDays: &days}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("conflicting retention = %v", err)
	}
	all, err := registry.UpdateSettings(1, SettingsPatch{KeepAllHistory: &keep})
	if err != nil || !all.KeepAllHistory || all.HistoryRetentionDays != nil {
		t.Fatalf("keep all = %#v, %v", all, err)
	}
	if _, err := registry.UpdateSettings(2, SettingsPatch{HistoryRetentionDays: &days}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("days during keep all = %v", err)
	}
	keep = false
	limited, err := registry.UpdateSettings(2, SettingsPatch{KeepAllHistory: &keep, HistoryRetentionDays: &days})
	if err != nil || limited.KeepAllHistory || limited.HistoryRetentionDays == nil || *limited.HistoryRetentionDays != days {
		t.Fatalf("limited = %#v, %v", limited, err)
	}
}

func TestSettingsToolPathCannotClaimCapability(t *testing.T) {
	registry, _, _ := registryFixture(t)
	unconfigured, err := registry.DetectTool("codex")
	if err != nil || unconfigured.Status != "unavailable" {
		t.Fatalf("unconfigured = %#v, %v", unconfigured, err)
	}
	tool := filepath.Join(t.TempDir(), "codex")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(tool, []byte("not actually codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.UpdateSettings(1, SettingsPatch{CodexBinaryPath: OptionalPath{Set: true, Value: &tool}}); err != nil {
		t.Fatal(err)
	}
	status, err := registry.DetectTool("codex")
	if err != nil || status.Status != "unverified" {
		t.Fatalf("path incorrectly marked capable: %#v, %v", status, err)
	}
}

func TestSettingsRejectsToolWithoutEffectiveExecutePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable permissions")
	}
	registry, _, _ := registryFixture(t)
	tool := filepath.Join(t.TempDir(), "not-executable-by-owner")
	if err := os.WriteFile(tool, []byte("tool"), 0001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tool, 0001); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.UpdateSettings(1, SettingsPatch{GitBinaryPath: OptionalPath{Set: true, Value: &tool}}); !errors.Is(err, ErrToolUnavailable) {
		t.Fatalf("owner cannot execute, update error = %v", err)
	}
}

func TestSettingsNonWritableStoreFailsWithoutAdvancingRevision(t *testing.T) {
	registry, stateDir, _ := registryFixture(t)
	if err := os.Chmod(stateDir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(stateDir, 0700)
	max := 5
	if _, err := registry.UpdateSettings(1, SettingsPatch{MaxConcurrency: &max}); err == nil {
		t.Fatal("settings update succeeded in non-writable directory")
	}
	settings, err := registry.Settings()
	if err != nil || settings.Revision != 1 || settings.MaxConcurrency != 4 {
		t.Fatalf("settings changed after failed write: %#v, %v", settings, err)
	}
}

func TestSettingsRevisionCannotWrap(t *testing.T) {
	current := defaultSettings()
	current.Revision = ^uint64(0)
	max := 5
	if _, err := applySettingsPatch(current, SettingsPatch{MaxConcurrency: &max}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("maximum revision update = %v", err)
	}
}
