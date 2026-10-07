package runtime

import "testing"

func TestNodeTransitionRulesRejectTerminalAndPrematureSuccess(t *testing.T) {
	for _, test := range []struct {
		from NodeState
		to   NodeState
		want bool
	}{
		{NodePending, NodeReady, true},
		{NodePending, NodeFailed, true},
		{NodeReady, NodeRunning, true},
		{NodeRunning, NodeSucceeded, true},
		{NodePending, NodeSucceeded, false},
		{NodeReady, NodeSucceeded, false},
		{NodeSucceeded, NodeRunning, false},
		{NodeFailed, NodeReady, false},
		{NodeCancelled, NodeFailed, false},
		{NodeSkipped, NodeReady, false},
	} {
		if got := validNodeTransition(test.from, test.to); got != test.want {
			t.Errorf("transition %s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestRunTransitionRulesHaveOneTerminalState(t *testing.T) {
	for _, terminal := range []RunState{RunSucceeded, RunFailed, RunCancelled} {
		if !validRunTransition(RunRunning, terminal) {
			t.Errorf("running -> %s rejected", terminal)
		}
		if validRunTransition(terminal, RunRunning) || validRunTransition(terminal, RunFailed) {
			t.Errorf("terminal run state %s accepted another transition", terminal)
		}
	}
}
