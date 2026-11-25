package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/testenv"
)

// A secret that contains "=" can span the key and the value of one --input
// item; neither half holds it, only the whole item does.
func TestInputItemEqualToSecretWithEqualsSignIsRejected(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, "ab=cd1234")
	marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
	path := writeGraph(t, guardGraphText("{ab: {type: string}}", guardNode(marker, "")))
	exit, out, errout := cli(t, "run", path, "--store-dir", store, "--record-values", "--input", "ab=cd1234")
	if exit != 3 || !strings.Contains(out+errout, "secret_value_in_input") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a node ran")
	}
	if entries, _ := os.ReadDir(store); len(entries) != 0 {
		t.Errorf("a refused run left store entries: %v", entries)
	}
}

func TestStoredMaterialFindsASecretSpanningKeyAndValueOfOneInput(t *testing.T) {
	material := storedMaterial{flags: inputFlags{"ab=cd1234"}}
	got := material.Locations(func(text string) bool { return strings.Contains(text, "ab=cd1234") })
	if strings.Join(got, ",") != "inputs" {
		t.Errorf("locations = %v, want [inputs]", got)
	}
}

// An input name equal to the secret is reported as plain "inputs", never with
// the name, and the value appears nowhere in the output.
func TestInputNameEqualToResolvedValueIsReportedWithoutTheName(t *testing.T) {
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, "abc-secret1")
	for _, extra := range [][]string{nil, {"--json"}} {
		marker, store := filepath.Join(t.TempDir(), "ran"), t.TempDir()
		path := writeGraph(t, guardGraphText("{abc-secret1: {type: string}}", guardNode(marker, "")))
		args := append([]string{"run", path, "--store-dir", store, "--input", "abc-secret1=x"}, extra...)
		exit, out, errout := cli(t, args...)
		shown := out + errout
		if exit != 3 || !strings.Contains(shown, "secret_value_in_input") {
			t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, errout)
		}
		if !strings.Contains(shown, "at inputs:") && !strings.Contains(shown, `"path":"inputs"`) {
			t.Errorf("location must be plain inputs: %s", shown)
		}
		if strings.Contains(shown, "abc-secret1") {
			t.Errorf("output holds the value: %s", shown)
		}
	}
}
