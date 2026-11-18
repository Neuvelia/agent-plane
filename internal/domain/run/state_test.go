package run_test

import (
	"agentplane/internal/domain/run"
	"testing"
)

func TestNodeTransitionRulesRejectTerminalAndPrematureSuccess(t *testing.T) {
	for _, test := range []struct {
		from run.NodeState
		to   run.NodeState
		want bool
	}{
		{run.NodePending, run.NodeReady, true},
		{run.NodePending, run.NodeFailed, true},
		{run.NodeReady, run.NodeRunning, true},
		{run.NodeRunning, run.NodeSucceeded, true},
		{run.NodePending, run.NodeSucceeded, false},
		{run.NodeReady, run.NodeSucceeded, false},
		{run.NodeSucceeded, run.NodeRunning, false},
		{run.NodeFailed, run.NodeReady, false},
		{run.NodeCancelled, run.NodeFailed, false},
		{run.NodeSkipped, run.NodeReady, false},
	} {
		if got := run.ValidNodeTransition(test.from, test.to); got != test.want {
			t.Errorf("transition %s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestRunTransitionRulesHaveOneTerminalState(t *testing.T) {
	for _, terminal := range []run.State{run.RunSucceeded, run.RunFailed, run.RunCancelled} {
		if !run.ValidRunTransition(run.RunRunning, terminal) {
			t.Errorf("running -> %s rejected", terminal)
		}
		if run.ValidRunTransition(terminal, run.RunRunning) || run.ValidRunTransition(terminal, run.RunFailed) {
			t.Errorf("terminal run state %s accepted another transition", terminal)
		}
	}
}
