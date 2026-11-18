package yamlgraph

import (
	"fmt"
	"strconv"

	"go.yaml.in/yaml/v3"

	"agentplane/internal/domain/graph"
)

// TimeoutInvalidError reports a timeout field that is not an integer. The typed
// decode would only say "cannot unmarshal", so the parser names the field.
type TimeoutInvalidError struct {
	Path    string
	Message string
}

func (e *TimeoutInvalidError) Error() string {
	return fmt.Sprintf("parse graph definition: E_TIMEOUT_INVALID at %s: %s; use whole seconds from 1 to 86400, or omit the field", e.Path, e.Message)
}

// checkTimeouts runs before the strict decode so a float, string, boolean or
// null timeout is reported with its path. Range is the validator's concern.
func checkTimeouts(data []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	if err := checkTimeoutScalar(field(field(root, "limits"), "run_timeout_seconds"), "limits.run_timeout_seconds"); err != nil {
		return err
	}
	nodes := field(root, "nodes")
	if nodes == nil || nodes.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(nodes.Content); i += 2 {
		id := nodes.Content[i].Value
		path := "nodes." + id + ".timeout_seconds"
		if !graph.ValidIdentifier(id) {
			path = "nodes[" + strconv.Quote(id) + "].timeout_seconds"
		}
		if err := checkTimeoutScalar(field(nodes.Content[i+1], "timeout_seconds"), path); err != nil {
			return err
		}
	}
	return nil
}

func checkTimeoutScalar(value *yaml.Node, path string) error {
	if value == nil {
		return nil
	}
	var seconds int
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!int" || value.Decode(&seconds) != nil {
		return &TimeoutInvalidError{Path: path, Message: "timeout must be an integer number of seconds"}
	}
	return nil
}
