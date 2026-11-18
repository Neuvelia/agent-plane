package gate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
)

type config struct {
	CheckID      string          `json:"check_id"`
	Kind         string          `json:"kind"`
	Cwd          string          `json:"cwd"`
	Command      string          `json:"command"`
	Left         string          `json:"left_input"`
	Right        string          `json:"right_input"`
	Input        string          `json:"input"`
	Field        []string        `json:"field"`
	Operator     string          `json:"operator"`
	ExpectedJSON *string         `json:"expected_json"`
	Expected     json.RawMessage `json:"expected"`
	Allowed      []string        `json:"allowed_paths"`
	Mode         string          `json:"mode"`
	Workspace    string          `json:"workspace"`
}

func parse(n graph.NodeDefinition) (config, error) {
	var c config
	if n.Type != "gate" {
		return c, fmt.Errorf("expected gate node")
	}
	b, err := json.Marshal(n.Config)
	if err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if strings.TrimSpace(c.CheckID) == "" || len(c.CheckID) > 256 {
		return c, fmt.Errorf("check_id must be nonempty and at most 256 bytes")
	}
	port := func(name string, t graph.ValueType) error {
		p, ok := n.Inputs[name]
		if name == "" || !ok || p.Type != t || !p.Required {
			return fmt.Errorf("input %q must be a required %s port", name, t)
		}
		return nil
	}
	switch c.Kind {
	case "command":
		if c.Command != diffCommand || !filepath.IsAbs(c.Cwd) || filepath.Clean(c.Cwd) != c.Cwd {
			return c, fmt.Errorf("command requires /usr/bin/diff and canonical absolute cwd")
		}
		if err = port(c.Left, graph.ValueTypeString); err == nil {
			err = port(c.Right, graph.ValueTypeString)
		}
	case "json_predicate":
		_, hasExpected := n.Config["expected"]
		_, hasLiteral := n.Config["expected_json"]
		if hasExpected == hasLiteral {
			return c, fmt.Errorf("predicate requires exactly one of expected or expected_json")
		}
		if hasLiteral {
			if c.ExpectedJSON == nil {
				return c, fmt.Errorf("expected_json must be a JSON scalar/null literal string")
			}
			c.Expected = json.RawMessage(*c.ExpectedJSON)
		} else {
			switch n.Config["expected"].(type) {
			case float32, float64:
				return c, fmt.Errorf("floating-point expected may lose precision; use expected_json with an exact JSON literal")
			}
		}
		if len(c.Field) == 0 || (c.Operator != "eq" && c.Operator != "ne") || len(c.Expected) == 0 {
			return c, fmt.Errorf("predicate requires field, eq/ne operator and expected scalar/null")
		}
		for _, f := range c.Field {
			if f == "" {
				return c, fmt.Errorf("field keys must be nonempty")
			}
		}
		if !json.Valid(c.Expected) {
			return c, fmt.Errorf("expected_json must contain exactly one valid JSON scalar/null")
		}
		var v any
		d := json.NewDecoder(bytes.NewReader(c.Expected))
		d.UseNumber()
		if e := d.Decode(&v); e != nil {
			return c, e
		}
		switch v.(type) {
		case nil, string, bool, json.Number:
		default:
			return c, fmt.Errorf("expected must be scalar/null")
		}
		err = port(c.Input, graph.ValueTypeJSON)
	case "reported_file_scope":
		if _, ok := n.Config["allowed_paths"]; !ok || c.Allowed == nil {
			return c, fmt.Errorf("allowed_paths must be a list")
		}
		for i, p := range c.Allowed {
			q, e := normalize(p)
			if e != nil {
				return c, e
			}
			if strings.HasSuffix(p, "/") {
				q += "/"
			}
			c.Allowed[i] = q
		}
		err = port(c.Input, graph.ValueTypeJSON)
	case "changed_files_scope":
		if c.Mode != "warn" && c.Mode != "deny" {
			return c, fmt.Errorf("changed_files_scope mode must be warn or deny")
		}
		if !filepath.IsAbs(c.Workspace) || filepath.Clean(c.Workspace) != c.Workspace {
			return c, fmt.Errorf("changed_files_scope workspace must be an absolute clean path")
		}
		if _, ok := n.Config["allowed_paths"]; !ok || c.Allowed == nil {
			return c, fmt.Errorf("changed_files_scope allowed_paths must be a list")
		}
		c.Allowed, err = normalizeScopeRules(c.Allowed)
		if err != nil {
			return c, err
		}
		candidate, ok := n.Inputs[c.Input]
		if c.Input == "" || !ok || !candidate.Required || (candidate.Type != graph.ValueTypeString && candidate.Type != graph.ValueTypeJSON) {
			return c, fmt.Errorf("changed_files_scope input %q must be a required string or JSON port", c.Input)
		}
	default:
		return c, fmt.Errorf("unsupported gate kind %q", c.Kind)
	}
	keys := make([]string, 0, len(n.Config))
	for k := range n.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	allowed := fields.AllowedKeys(ConfigFields(), n.Config)
	for _, k := range keys {
		if !allowed[k] {
			return c, fmt.Errorf("config field %q is unsupported for %s", k, c.Kind)
		}
	}
	if err != nil {
		return c, err
	}
	keys = keys[:0]
	for k := range n.Outputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k != "verdict" || n.Outputs[k].Type != graph.ValueTypeString {
			return c, fmt.Errorf("only verdict:string output is supported")
		}
	}
	return c, nil
}
func normalize(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\\") || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("invalid portable relative path")
	}
	// Drive prefixes are forbidden even on Unix.
	if len(p) >= 2 && p[1] == ':' {
		return "", fmt.Errorf("invalid portable relative path")
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." {
			return "", fmt.Errorf("parent traversal is forbidden")
		}
	}
	q := path.Clean(p)
	if q == "." {
		return "", fmt.Errorf("path must identify a file/directory")
	}
	return q, nil
}

// ValidateNodeConfig checks Gate configuration without evaluating inputs.
func (Executor) ValidateNodeConfig(_ string, n graph.NodeDefinition) error {
	_, err := parse(n)
	return err
}
