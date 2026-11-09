package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"agentplane/internal/desktop"
)

// A graph authored through the desktop document contract remains an ordinary
// schema-0 file that the independent CLI can validate and execute unchanged.
func TestDesktopAuthoredShellGraphRunsWithCLI(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".agents", "graphs"), 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := desktop.NewRegistry(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	store := desktop.NewDocumentStore(registry, stateDir)
	doc, err := store.Create(project.ProjectID, ".agents/graphs/cli-authored.yaml", "cli-authored")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewEdit(doc.DocumentID, doc.Revision, desktop.AuthoringOperation{Kind: "add_node", NodeID: "hello", NodeType: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	previewBytes, err := base64.StdEncoding.DecodeString(preview.TextBase64)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = store.Update(doc.DocumentID, preview.BaseRevision, string(previewBytes))
	if err != nil {
		t.Fatal(err)
	}
	command, args := "/usr/bin/printf", "[desktop-authored]"
	if runtime.GOOS == "windows" {
		command = os.Getenv("ComSpec")
		if command == "" {
			command = `C:\Windows\System32\cmd.exe`
		}
		args = "[/c, echo, desktop-authored]"
	}
	configured := strings.Replace(doc.Text, "config: {}", "config: {command: "+strconv.Quote(command)+", args: "+args+"}", 1)
	if configured == doc.Text {
		t.Fatalf("visual Shell node not found in preview: %q", doc.Text)
	}
	doc, err = store.Update(doc.DocumentID, doc.Revision, configured)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(doc.DocumentID, doc.Revision, doc.DiskDigest, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, ".agents", "graphs", "cli-authored.yaml")
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != configured {
		t.Fatalf("saved bytes changed: %v, %q", err, stored)
	}
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"graph", "validate", path, "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("CLI validation failed: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"run", path, "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("CLI run failed: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Status != "succeeded" {
		t.Fatalf("CLI result = %q, %v", stdout.String(), err)
	}
}
