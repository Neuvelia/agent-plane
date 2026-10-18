package graph

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// SemanticVersion identifies the execution-relevant schema-v0 definition.
// Name and Metadata are presentation fields and do not affect the version.
func SemanticVersion(definition *Definition) (string, error) {
	if definition == nil {
		return "", fmt.Errorf("semantic graph version: definition is nil")
	}
	semantic := *definition
	semantic.Name = ""
	semantic.Metadata = nil
	semantic.Edges = append([]EdgeDefinition(nil), definition.Edges...)
	sort.Slice(semantic.Edges, func(i, j int) bool {
		if semantic.Edges[i].From != semantic.Edges[j].From {
			return semantic.Edges[i].From < semantic.Edges[j].From
		}
		return semantic.Edges[i].To < semantic.Edges[j].To
	})
	semantic.GraphInputBindings = append([]GraphInputBindingDefinition(nil), definition.GraphInputBindings...)
	sort.Slice(semantic.GraphInputBindings, func(i, j int) bool {
		if semantic.GraphInputBindings[i].From != semantic.GraphInputBindings[j].From {
			return semantic.GraphInputBindings[i].From < semantic.GraphInputBindings[j].From
		}
		return semantic.GraphInputBindings[i].To < semantic.GraphInputBindings[j].To
	})
	data, err := Marshal(&semantic)
	if err != nil {
		return "", fmt.Errorf("semantic graph version: %w", err)
	}
	return fmt.Sprintf("sha256-v0:%x", sha256.Sum256(data)), nil
}
