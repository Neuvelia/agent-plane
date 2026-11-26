package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/agent/adaptertest"
)

// roadmap: F1.1-AC1, F3.10-AC1
func TestMockAdapterContract(t *testing.T) {
	adaptertest.RunContract(t, func(_ *testing.T, scenario adaptertest.Scenario) adaptertest.ContractFixture {
		input := agent.AgentExecutionInput{RunID: "run-test", NodeRunID: "node-test", Task: "unit task", Workspace: "/tmp/unit-work", AllowedPaths: []string{"src/"}, ContextRefs: []string{"docs/spec.md"}, EnvironmentRefs: []string{"TOOL_HOME"}}
		output := agent.AgentExecutionOutput{Summary: "finished", Artifacts: []agent.ArtifactReference{{Ref: "artifact:report", Name: "report"}}, ChangedFiles: []agent.ChangedFile{{Path: "src/b.go", Provenance: agent.ProvenanceReported}}, RawMetadata: json.RawMessage(`{"tool":true}`)}
		count := int64(17)
		model, cost, currency := "model", "0.002", "USD"
		output.Usage = &agent.UsageMetadata{InputTokens: &count, OutputTokens: &count, ModelName: &model, ProviderCost: &cost, Currency: &currency}
		yes, no, version := true, false, "1.0"
		m := &adaptertest.MockAdapter{AdapterID: "mock", Report: agent.CapabilityReport{Available: true, DisplayName: "Mock", Version: &version, Cancellation: &yes, Streaming: &no, UsageReporting: &yes, Workspace: &yes}, Output: output}
		cause := errors.New("controlled provider failure")
		switch scenario {
		case adaptertest.UsageAbsent:
			m.Output.Usage = nil
		case adaptertest.Unavailable:
			m.Report.Available = false
		case adaptertest.DetectionFailure:
			m.DetectErr = cause
		case adaptertest.StartFailure:
			m.ExecuteErr = &agent.ExecutionError{Kind: agent.ErrorStart, Cause: cause}
		case adaptertest.ExecutionFailure:
			m.ExecuteErr = cause
		case adaptertest.UsagePartial:
			model := "test-model"
			m.Output.Usage = &agent.UsageMetadata{ModelName: &model}
		case adaptertest.UsageZero:
			zero := int64(0)
			m.Output.Usage = &agent.UsageMetadata{OutputTokens: &zero}
		case adaptertest.InFlightCancellation:
			m.WaitForCancel = true
			m.Started = make(chan struct{})
		}
		wantErr := m.ExecuteErr
		if m.DetectErr != nil {
			wantErr = cause
		}
		return adaptertest.ContractFixture{Adapter: m, Input: input, WantOutput: m.Output, WantReport: m.Report, WantError: wantErr, Started: m.Started, ReceivedInput: m.LastInput}
	})
}

func TestMockAdapterControlledOutcomes(t *testing.T) {
	m := &adaptertest.MockAdapter{AdapterID: "mock", Report: agent.CapabilityReport{Available: false}}
	report, err := m.Detect(context.Background())
	if err != nil || report.Available {
		t.Fatalf("unavailable = %+v, %v", report, err)
	}
	want := errors.New("detection failed")
	m.DetectErr = want
	if _, err := m.Detect(context.Background()); !errors.Is(err, want) {
		t.Fatalf("detection error = %v", err)
	}
	m.DetectErr = nil
	m.Report.Available = true
	m.ExecuteErr = want
	if _, err := m.Execute(context.Background(), agent.AgentExecutionInput{}); !errors.Is(err, want) {
		t.Fatalf("execution error = %v", err)
	}
	m.ExecuteErr = nil
	m.Output = agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"provider_field":7}`), Usage: &agent.UsageMetadata{}}
	out, err := m.Execute(context.Background(), agent.AgentExecutionInput{})
	if err != nil || string(out.RawMetadata) != `{"provider_field":7}` || out.Usage == nil || out.Usage.InputTokens != nil {
		t.Fatalf("metadata/unknown usage = %+v, %v", out, err)
	}
}

func TestCapabilityUnknownDistinctFromFalse(t *testing.T) {
	no := false
	report := agent.CapabilityReport{Available: true, Workspace: &no}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded agent.CapabilityReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Workspace == nil || *decoded.Workspace || decoded.Streaming != nil {
		t.Fatalf("round trip = %+v", decoded)
	}
}

func TestOutputMetadataAndUsageRoundTrip(t *testing.T) {
	zero := int64(0)
	out := agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"arbitrary":{"provider":true}}`), Usage: &agent.UsageMetadata{OutputTokens: &zero}, ChangedFiles: []agent.ChangedFile{{Path: "src/a.go", Provenance: agent.ProvenanceObserved}}, Artifacts: []agent.ArtifactReference{{Ref: "artifact:one"}}}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded agent.AgentExecutionOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Usage == nil || decoded.Usage.OutputTokens == nil || *decoded.Usage.OutputTokens != 0 || decoded.Usage.InputTokens != nil || string(decoded.RawMetadata) != string(out.RawMetadata) || decoded.ChangedFiles[0].Provenance != agent.ProvenanceObserved || decoded.Artifacts[0].Ref != "artifact:one" {
		t.Fatalf("round trip = %+v", decoded)
	}
}

func TestMockStartSignalOnlyClosesOnce(t *testing.T) {
	m := &adaptertest.MockAdapter{AdapterID: "mock", Started: make(chan struct{})}
	for range 2 {
		if _, err := m.Execute(context.Background(), agent.AgentExecutionInput{}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-m.Started:
	default:
		t.Fatal("start signal was not sent")
	}
}
