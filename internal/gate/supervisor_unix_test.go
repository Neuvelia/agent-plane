//go:build darwin || linux

package gate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/process"
	"agentplane/internal/runtime"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-process-supervisor" {
		os.Exit(process.Supervise(os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func TestSupervisedLegacyDiffPreservesGreenAndRed(t *testing.T) {
	for _, tc := range []struct {
		name, right string
		red         bool
	}{{"green", "same\n", false}, {"red", "different\n", true}} {
		t.Run(tc.name, func(t *testing.T) {
			n := diffNode(t)
			cwd := n.Config["cwd"].(string)
			for name, content := range map[string]string{"left": "same\n", "right": tc.right} {
				if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0400); err != nil {
					t.Fatal(err)
				}
			}
			var check *events.GateCheckPayload
			_, _, err := (Executor{SupervisorBinary: os.Args[0]}).ExecuteNodeObserved(context.Background(), "gate", n, paths(), runtime.ExecutionObservation{Check: func(p events.GateCheckPayload) { check = &p }})
			var red RedError
			if check == nil || check.Validate() != nil || (check.Verdict == "red") != tc.red || errors.As(err, &red) != tc.red {
				t.Fatalf("supervised diff verdict=%+v error=%v", check, err)
			}
			wantCode := 0
			if tc.red {
				wantCode = 1
			}
			if len(check.Evidence) != 1 || check.Evidence[0].ExitCode == nil || *check.Evidence[0].ExitCode != wantCode {
				t.Fatalf("supervised diff evidence=%+v", check.Evidence)
			}
		})
	}
}
