package adaptertest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"agentplane/internal/agent"
)

// ContractFixture supplies deterministic expectations for one adapter run.
// ReceivedInput is optional when an adapter has no test observation hook.
type ContractFixture struct {
	Adapter       agent.AgentAdapter
	Input         agent.AgentExecutionInput
	WantOutput    agent.AgentExecutionOutput
	WantReport    agent.CapabilityReport
	WantError     error
	Started       <-chan struct{}
	ReceivedInput func() (agent.AgentExecutionInput, bool)
}

// Scenario selects a controlled outcome; each factory invocation must return an
// independent fixture. Cancellation must block until the context is cancelled.
type Scenario string

const (
	Success              Scenario = "success"
	Unavailable          Scenario = "unavailable"
	DetectionFailure     Scenario = "detection failure"
	StartFailure         Scenario = "start failure"
	ExecutionFailure     Scenario = "execution failure"
	UsageAbsent          Scenario = "usage absent"
	UsagePartial         Scenario = "usage partial"
	UsageZero            Scenario = "usage zero"
	InFlightCancellation Scenario = "in-flight cancellation"
)

// RunContract checks the public adapter semantics. A fresh fixture per case
// lets later adapters supply their own workspace, task, and expected result.
func RunContract(t *testing.T, factory func(*testing.T, Scenario) ContractFixture) {
	t.Helper()
	t.Run("identity and detection", func(t *testing.T) {
		fixture := factory(t, Success)
		a := fixture.Adapter
		if a.ID() == "" || a.ID() != a.ID() {
			t.Fatal("empty adapter ID")
		}
		report, err := a.Detect(context.Background())
		if err != nil || !report.Available || !reflect.DeepEqual(report, fixture.WantReport) {
			t.Fatalf("Detect = %+v, %v", report, err)
		}
	})
	t.Run("input and output", func(t *testing.T) {
		fixture := factory(t, Success)
		output, err := fixture.Adapter.Execute(context.Background(), fixture.Input)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(output, fixture.WantOutput) {
			t.Fatalf("output = %+v; want %+v", output, fixture.WantOutput)
		}
		if fixture.ReceivedInput != nil {
			input, ok := fixture.ReceivedInput()
			if !ok || !reflect.DeepEqual(input, fixture.Input) {
				t.Fatalf("received input = %+v, present=%v; want %+v", input, ok, fixture.Input)
			}
		}
	})
	for _, scenario := range []Scenario{Unavailable, DetectionFailure, StartFailure, ExecutionFailure, UsageAbsent, UsagePartial, UsageZero} {
		t.Run(string(scenario), func(t *testing.T) {
			f := factory(t, scenario)
			if scenario == Unavailable || scenario == DetectionFailure {
				report, err := f.Adapter.Detect(context.Background())
				if !reflect.DeepEqual(report, f.WantReport) || !errors.Is(err, f.WantError) {
					t.Fatalf("Detect = %+v, %v; want %+v, %v", report, err, f.WantReport, f.WantError)
				}
				return
			}
			output, err := f.Adapter.Execute(context.Background(), f.Input)
			if !errors.Is(err, f.WantError) {
				t.Fatalf("error = %v; want %v", err, f.WantError)
			}
			if f.WantError == nil {
				if !reflect.DeepEqual(output, f.WantOutput) {
					t.Fatalf("output = %+v; want %+v", output, f.WantOutput)
				}
				encoded, err := json.Marshal(output)
				if err != nil {
					t.Fatal(err)
				}
				var decoded agent.AgentExecutionOutput
				if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, output) {
					t.Fatalf("round trip = %+v, %v", decoded, err)
				}
			}
		})
	}
	t.Run("in-flight cancellation", func(t *testing.T) {
		f := factory(t, InFlightCancellation)
		if f.Started == nil {
			t.Fatal("cancellation fixture needs a started barrier")
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := f.Adapter.Execute(ctx, f.Input); done <- err }()
		<-f.Started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		fixture := factory(t, Success)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := fixture.Adapter.Execute(ctx, fixture.Input)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	})
}
