package agent

import "agentplane/internal/fields"

// ConfigFields describes the Agent node configuration parseNode accepts
// (F2.2). parseNode derives its allowed keys from it. The adapter is free
// text here; whether an adapter exists is decided at run preflight.
func ConfigFields() []fields.Field {
	notBlank := func(id string) *fields.Field {
		return &fields.Field{ID: id, Kind: fields.String, Format: fields.FormatNotBlank}
	}
	return []fields.Field{
		{ID: "agent.config.adapter", Key: "adapter", Kind: fields.String, Required: true, Format: fields.FormatNotBlank},
		{ID: "agent.config.workspace", Key: "workspace", Kind: fields.String, Required: true, Format: fields.FormatAbsolutePath},
		{ID: "agent.config.task", Key: "task", Kind: fields.Text, Required: true, Format: fields.FormatNotBlank,
			Excludes: []fields.Rule{{Input: "task"}}},
		{ID: "agent.config.allowed_paths", Key: "allowed_paths", Kind: fields.PathList, Item: notBlank("agent.config.allowed_paths.item")},
		{ID: "agent.config.context_refs", Key: "context_refs", Kind: fields.List, Item: notBlank("agent.config.context_refs.item")},
		{ID: "agent.config.environment_refs", Key: "environment_refs", Kind: fields.List, Item: notBlank("agent.config.environment_refs.item")},
	}
}

// OutputPorts lists the outputs an Agent node may declare.
func OutputPorts() []fields.Port {
	return []fields.Port{
		{Name: "summary", Type: "string"},
		{Name: "changed_files", Type: "json"},
		{Name: "artifacts", Type: "json"},
		{Name: "raw_metadata", Type: "json"},
		{Name: "usage", Type: "json"},
	}
}
