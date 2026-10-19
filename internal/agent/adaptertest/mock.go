// Package adaptertest provides deterministic AgentAdapter test doubles and conformance checks.
// It is intended only for tests.
package adaptertest

import (
	"context"
	"sync"

	"agentplane/internal/agent"
)

type MockAdapter struct {
	AdapterID     agent.AdapterID
	Report        agent.CapabilityReport
	DetectErr     error
	Output        agent.AgentExecutionOutput
	ExecuteErr    error
	WaitForCancel bool
	Started       chan struct{}

	mu        sync.Mutex
	startOnce sync.Once
	Inputs    []agent.AgentExecutionInput
}

var _ agent.AgentAdapter = (*MockAdapter)(nil)

func (m *MockAdapter) ID() agent.AdapterID { return m.AdapterID }
func (m *MockAdapter) Detect(context.Context) (agent.CapabilityReport, error) {
	return m.Report, m.DetectErr
}
func (m *MockAdapter) Execute(ctx context.Context, input agent.AgentExecutionInput) (agent.AgentExecutionOutput, error) {
	if err := ctx.Err(); err != nil {
		return agent.AgentExecutionOutput{}, err
	}
	m.mu.Lock()
	m.Inputs = append(m.Inputs, input)
	m.mu.Unlock()
	if m.Started != nil {
		m.startOnce.Do(func() { close(m.Started) })
	}
	if m.WaitForCancel {
		<-ctx.Done()
		return agent.AgentExecutionOutput{}, ctx.Err()
	}
	return m.Output, m.ExecuteErr
}

func (m *MockAdapter) LastInput() (agent.AgentExecutionInput, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Inputs) == 0 {
		return agent.AgentExecutionInput{}, false
	}
	return m.Inputs[len(m.Inputs)-1], true
}
