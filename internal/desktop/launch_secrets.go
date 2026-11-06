package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"agentplane/internal/graph"
)

var errEmbeddedSecret = errors.New("launch contains a credential-shaped key")

func credentialShapedKey(key string) bool {
	normalized := make([]byte, 0, len(key))
	for i := 0; i < len(key); i++ {
		letter := key[i]
		if letter == '_' || letter == '-' {
			continue
		}
		if letter >= 'A' && letter <= 'Z' {
			letter += 'a' - 'A'
		}
		normalized = append(normalized, letter)
	}
	value := string(normalized)
	for _, word := range []string{"password", "secret", "token", "credential", "apikey", "privatekey"} {
		if strings.Contains(value, word) {
			return true
		}
	}
	return false
}

// inputStringLeaves visits the original JSON token stream so duplicate object
// members cannot hide a credential key or a value from the redaction set.
func inputStringLeaves(inputs map[string]graph.Value) ([]string, error) {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	var leaves []string
	seen := make(map[string]bool)
	usedBytes := 0
	add := func(text string) error {
		if text == "" || seen[text] {
			return nil
		}
		if len(leaves) >= maxLaunchRedactionValues || usedBytes+len(text) > maxLaunchRedactionBytes {
			return errLaunchRedactions
		}
		seen[text] = true
		leaves = append(leaves, text)
		usedBytes += len(text)
		return nil
	}
	for _, name := range names {
		value := inputs[name]
		if text, ok := value.AsString(); ok {
			if err := add(text); err != nil {
				return nil, err
			}
			continue
		}
		raw, ok := value.AsJSON()
		if !ok {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := walkInputJSON(decoder, add); err != nil {
			if errors.Is(err, errLaunchRedactions) {
				return nil, err
			}
			return nil, errEmbeddedSecret
		}
	}
	return leaves, nil
}

func walkInputJSON(decoder *json.Decoder, visit func(string) error) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if text, ok := token.(string); ok {
		return visit(text)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || credentialShapedKey(key) {
				return errEmbeddedSecret
			}
			if err := walkInputJSON(decoder, visit); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkInputJSON(decoder, visit); err != nil {
				return err
			}
		}
	default:
		return errEmbeddedSecret
	}
	_, err = decoder.Token()
	return err
}

func validateCredentialKeys(definition *graph.Definition, inputs map[string]graph.Value) (string, []string, error) {
	ids := make([]string, 0, len(definition.Nodes))
	for id := range definition.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := definition.Nodes[id]
		if node.Type != "shell" {
			continue
		}
		raw, present := node.Config["env"]
		if !present {
			continue
		}
		if credentialShellEnvKey(raw) {
			return "nodes." + id + ".config.env", nil, errEmbeddedSecret
		}
	}
	leaves, err := inputStringLeaves(inputs)
	if err != nil {
		return "inputs", nil, err
	}
	return "", leaves, nil
}

func credentialShellEnvKey(raw any) bool {
	switch entries := raw.(type) {
	case map[string]string:
		for name := range entries {
			if credentialShapedKey(name) {
				return true
			}
		}
	case map[string]any:
		for name := range entries {
			if credentialShapedKey(name) {
				return true
			}
		}
	case graph.NodeConfig:
		for name := range entries {
			if credentialShapedKey(name) {
				return true
			}
		}
	}
	return false
}
