package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

type orderedProduceAdapter struct {
	order      *[]string
	executeErr error
}

func (a orderedProduceAdapter) ID() AdapterID { return "ordered" }
func (a orderedProduceAdapter) Detect(context.Context) (CapabilityReport, error) {
	*a.order = append(*a.order, "detect")
	return CapabilityReport{Available: true}, nil
}
func (a orderedProduceAdapter) Execute(context.Context, AgentExecutionInput) (AgentExecutionOutput, error) {
	*a.order = append(*a.order, "execute")
	return AgentExecutionOutput{Summary: "done"}, a.executeErr
}

func TestProductionHooksSurroundProviderAfterDetectionEvenOnError(t *testing.T) {
	for _, failed := range []bool{false, true} {
		order := []string{}
		var executeErr error
		if failed {
			executeErr = errors.New("provider failed")
		}
		executor := Executor{Adapters: map[AdapterID]AgentAdapter{"ordered": orderedProduceAdapter{order: &order, executeErr: executeErr}}}
		node := graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"adapter": "ordered", "task": "edit", "workspace": "/tmp", "allowed_paths": []string{"src/"}}, Outputs: map[string]graph.PortDefinition{"summary": {Type: graph.ValueTypeString}}}
		_, _, err := executor.ExecuteNodeObserved(context.Background(), "work", node, map[string]graph.Value{}, runtime.ExecutionObservation{
			BeforeProduce: func(context.Context) { order = append(order, "before") },
			AfterProduce:  func(context.Context) { order = append(order, "after") },
		})
		if failed && err == nil || !failed && err != nil {
			t.Fatalf("failed=%v result=%v", failed, err)
		}
		if !reflect.DeepEqual(order, []string{"detect", "before", "execute", "after"}) {
			t.Fatalf("failed=%v order=%v", failed, order)
		}
	}
}
