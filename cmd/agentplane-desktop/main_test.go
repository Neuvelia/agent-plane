package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"agentplane/internal/desktop"
)

func TestSidecarStdioHandshake(t *testing.T) {
	name := "agentplane-desktop"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sidecar: %v: %s", err, output)
	}
	var input bytes.Buffer
	for _, request := range []json.RawMessage{
		json.RawMessage(`{"protocol_version":1,"request_id":"wrong","method":"hello","params":{"client_protocol_version":2}}`),
		json.RawMessage(`{"protocol_version":1,"request_id":"ready","method":"hello","params":{"client_protocol_version":1}}`),
	} {
		if err := desktop.WriteFrame(&input, request); err != nil {
			t.Fatal(err)
		}
	}
	process := exec.Command(binary)
	process.Stdin = &input
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	if err := process.Run(); err != nil {
		t.Fatalf("sidecar: %v: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", stderr.String())
	}
	var responses []desktop.Response
	for i := 0; i < 2; i++ {
		frame, err := desktop.ReadFrame(&stdout)
		if err != nil {
			t.Fatal(err)
		}
		var response desktop.Response
		if err := json.Unmarshal(frame, &response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if responses[0].RequestID != "wrong" || responses[0].Error == nil || responses[0].Error.Code != "version_mismatch" {
		t.Fatalf("version response = %#v", responses[0])
	}
	if responses[1].RequestID != "ready" || responses[1].Error != nil || !bytes.Contains(responses[1].Result, []byte(`"methods":["hello"]`)) {
		t.Fatalf("hello response = %#v", responses[1])
	}
	if _, err := desktop.ReadFrame(&stdout); err != io.EOF {
		t.Fatalf("stdout contains non-protocol data: %v", err)
	}
}
