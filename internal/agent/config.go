package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
)

type nodeConfig struct {
	adapter         AdapterID
	task            string
	workspace       string
	allowedPaths    []string
	contextRefs     []string
	environmentRefs []string
}

func parseNode(node graph.NodeDefinition, inputs map[string]graph.Value) (nodeConfig, error) {
	var cfg nodeConfig
	if node.Type != "agent" {
		return cfg, fmt.Errorf("expected agent node, got %q", node.Type)
	}
	keys := make([]string, 0, len(node.Config))
	for key := range node.Config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	allowed := fields.AllowedKeys(ConfigFields(), node.Config)
	for _, key := range keys {
		if !allowed[key] {
			return cfg, fmt.Errorf("unknown agent config field %q", key)
		}
	}
	adapter, ok := node.Config["adapter"].(string)
	if !ok || strings.TrimSpace(adapter) == "" {
		return cfg, errors.New("config.adapter must be a nonempty string")
	}
	cfg.adapter = AdapterID(adapter)
	workspace, ok := node.Config["workspace"].(string)
	if !ok || !filepath.IsAbs(workspace) {
		return cfg, errors.New("config.workspace must be an absolute local path")
	}
	cfg.workspace = workspace
	if raw, present := node.Config["isolation"]; present {
		if mode, ok := raw.(string); !ok || (mode != "none" && mode != "worktree") {
			return cfg, errors.New(`config.isolation must be "none" or "worktree"`)
		}
	}
	if literal, present := node.Config["task"]; present {
		value, ok := literal.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return cfg, errors.New("config.task must be a nonempty string")
		}
		if _, declared := node.Inputs["task"]; declared {
			return cfg, errors.New("agent task must have exactly one source")
		}
		cfg.task = value
	} else {
		declared, ok := node.Inputs["task"]
		if !ok || declared.Type != graph.ValueTypeString {
			return cfg, errors.New("agent requires a string task input or config.task")
		}
		value, present := inputs["task"]
		if !present {
			return cfg, errors.New("task input is not resolved")
		}
		task, ok := value.AsString()
		if !ok || strings.TrimSpace(task) == "" {
			return cfg, errors.New("resolved task must be a nonempty string")
		}
		cfg.task = task
	}
	var err error
	if cfg.allowedPaths, err = configStrings(node.Config, "allowed_paths"); err != nil {
		return cfg, err
	}
	if cfg.contextRefs, err = configStrings(node.Config, "context_refs"); err != nil {
		return cfg, err
	}
	if cfg.environmentRefs, err = configStrings(node.Config, "environment_refs"); err != nil {
		return cfg, err
	}
	portTypes := map[string]graph.ValueType{"summary": graph.ValueTypeString, "changed_files": graph.ValueTypeJSON, "artifacts": graph.ValueTypeJSON, "raw_metadata": graph.ValueTypeJSON, "usage": graph.ValueTypeJSON}
	ports := make([]string, 0, len(node.Outputs))
	for name := range node.Outputs {
		ports = append(ports, name)
	}
	sort.Strings(ports)
	for _, name := range ports {
		port := node.Outputs[name]
		want, ok := portTypes[name]
		if !ok {
			return cfg, fmt.Errorf("unsupported agent output %q", name)
		}
		if port.Type != want {
			return cfg, fmt.Errorf("agent output %q must have type %s", name, want)
		}
	}
	return cfg, nil
}

func configStrings(config graph.NodeConfig, key string) ([]string, error) {
	raw, present := config[key]
	if !present {
		return nil, nil
	}
	var values []string
	switch list := raw.(type) {
	case []string:
		values = append(values, list...)
	case []any:
		for _, item := range list {
			value, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("config.%s must be a list of nonempty strings", key)
			}
			values = append(values, value)
		}
	default:
		return nil, fmt.Errorf("config.%s must be a list of nonempty strings", key)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("config.%s must be a list of nonempty strings", key)
		}
	}
	return values, nil
}
