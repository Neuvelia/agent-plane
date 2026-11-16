package runtime

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"context"
	"sync"
	"testing"
)

type correlationProbe struct {
	mu       sync.Mutex
	received map[string]ExecutionCorrelation
	legacy   int
}

func (p *correlationProbe) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	p.legacy++
	return nil, nil
}
func (p *correlationProbe) ExecuteNodeWithCorrelation(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value, ids ExecutionCorrelation) (map[string]graph.Value, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.received[id] = ids
	return nil, nil
}

type combinedProbe struct {
	*correlationProbe
	logs int
}

func (p *combinedProbe) ExecuteNodeWithLogs(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
	p.logs++
	observe("stdout", []byte("log"))
	return nil, nil
}

func TestSchedulerExecutorCorrelationCompatibility(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"one": {Type: "test"}, "two": {Type: "test"}})
	legacyCalls := 0
	legacy := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		legacyCalls++
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, legacy, Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != RunSucceeded || legacyCalls != 2 {
		t.Fatalf("legacy=%+v %v calls=%d", result, err, legacyCalls)
	}
	p := &correlationProbe{received: map[string]ExecutionCorrelation{}}
	result, err = Run(context.Background(), def, nil, p, Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 2})
	if err != nil || result.State != RunSucceeded || p.legacy != 0 {
		t.Fatalf("correlated=%+v %v", result, err)
	}
	for _, n := range result.Nodes {
		ids := p.received[n.NodeID]
		if ids.RunID != result.RunID || ids.NodeRunID != n.NodeRunID || ids.RunID == "" || ids.NodeRunID == "" || ids.AttemptID != "" {
			t.Fatalf("ids=%+v node=%+v", ids, n)
		}
		for _, event := range result.Events {
			if event.NodeID == n.NodeID && (event.RunID != ids.RunID || event.NodeRunID != ids.NodeRunID || event.AttemptID != "") {
				t.Fatalf("event=%+v", event)
			}
		}
	}
	if p.received["one"].NodeRunID == p.received["two"].NodeRunID {
		t.Fatal("node IDs reused")
	}
}
func TestSchedulerExistingLogsTakePrecedenceOverCorrelation(t *testing.T) {
	p := &combinedProbe{correlationProbe: &correlationProbe{received: map[string]ExecutionCorrelation{}}}
	result, err := Run(context.Background(), definition(map[string]graph.NodeDefinition{"one": {Type: "test"}}), nil, p, Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != RunSucceeded || p.logs != 1 || p.legacy != 0 || len(p.received) != 0 {
		t.Fatalf("precedence=%+v %+v %v", p, result, err)
	}
	found := false
	for _, event := range result.Events {
		if event.Type == events.LogChunk {
			found = true
		}
	}
	if !found {
		t.Fatal("log event missing")
	}
}
