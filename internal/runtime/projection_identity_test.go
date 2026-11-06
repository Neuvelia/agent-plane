package runtime

import (
	"errors"
	"testing"

	"agentplane/internal/events"
)

func TestGeneratedProjectionIdentitiesRejectKnownValueWithoutEcho(t *testing.T) {
	template, err := newLogRedactor([]string{"private"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		run  events.RunID
		node events.NodeRunID
	}{
		{name: "run", run: "generated-private-id", node: "safe-node"},
		{name: "node", run: "safe-run", node: "generated-private-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			machine := &runMachine{ids: []string{"n"}, nodes: map[string]*NodeResult{"n": {NodeRunID: test.node}}, result: RunResult{RunID: test.run}}
			err := machine.validateGeneratedProjectionIdentities(template)
			if !errors.Is(err, ErrUnsafeProjectionIdentity) || err.Error() != ErrUnsafeProjectionIdentity.Error() {
				t.Fatalf("generated collision error = %v", err)
			}
		})
	}
	if !identityRedactedBy(template, "attempt-private-id") || identityRedactedBy(template, "safe-attempt") {
		t.Fatal("attempt identity collision classification")
	}
}
