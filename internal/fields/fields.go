// Package fields describes node configuration for editors (F2.2, ADR-0021).
//
// A descriptor is data owned by the package that validates the field: the
// Shell, Gate and Agent executors and the graph package (human node, loop)
// each export theirs and derive their own allowed-key checks from it, so a
// form generated from descriptors cannot offer a key the engine rejects, and
// a key the engine accepts cannot lack a descriptor. Descriptors carry no
// behavior; the owning validator stays the authority on values.
package fields

import "reflect"

// Kind is the editing control a field needs.
type Kind string

const (
	String    Kind = "string"     // single-line text
	Text      Kind = "text"       // multi-line text
	Bool      Kind = "bool"       // true or false
	Int       Kind = "int"        // whole number within Min and Max
	Enum      Kind = "enum"       // one of Enum
	List      Kind = "list"       // a list of Item
	Map       Kind = "map"        // string keys to string values
	InputName Kind = "input_name" // the name of a declared input port of the node
	PathList  Kind = "path_list"  // a list of relative workspace paths
	Scalar    Kind = "scalar"     // a string, boolean, integer or null
	Group     Kind = "group"      // nested Fields
)

// Format is a constraint on a string beyond its kind.
type Format string

const (
	FormatNonEmpty          Format = "nonempty"            // not the empty string
	FormatNotBlank          Format = "not_blank"           // contains a non-space character
	FormatAbsolutePath      Format = "absolute_path"       // an absolute local path
	FormatCleanAbsolutePath Format = "clean_absolute_path" // absolute and already clean
	// FormatEnvName is a variable name: not empty, without "=" or NUL, and not
	// the runtime's reserved AGENTPLANE_ATTEMPT in any letter case.
	FormatEnvName      Format = "env_name"
	FormatRelativePath Format = "relative_path" // a portable path inside the workspace
	FormatJSONScalar   Format = "json_scalar"   // the text of one JSON string, number, boolean or null
	// FormatSecretName is the name of a secret declared in the graph's secrets
	// section: [a-z][a-z0-9_]{0,63}. The owner of the field enforces the grammar.
	FormatSecretName Format = "secret_name"
)

// Rule is a condition on the node being edited.
type Rule struct {
	Key    string `json:"key,omitempty"`    // a key of the same config mapping
	Equals string `json:"equals,omitempty"` // that key's string value, or "true"/"false"; empty means "is present"
	Input  string `json:"input,omitempty"`  // a declared input port of the node
}

// Locked marks a field whose value is fixed by the engine.
type Locked struct {
	Value  any    `json:"value"`
	Reason string `json:"reason"`
}

// Field describes one configuration key, or a group of keys.
//
// Requires lists conditions that must all hold for the field to be accepted.
// Excludes lists conditions of which none may hold. A Required field with
// Excludes is satisfied by exactly one of itself and its excluded
// alternatives ("exactly one task source").
//
// A Group with an empty Key holds keys that sit in the parent mapping; it is
// how the keys of one Gate kind are offered only when that kind is selected.
type Field struct {
	ID            string   `json:"id"`
	Key           string   `json:"key,omitempty"`
	Kind          Kind     `json:"kind"`
	Required      bool     `json:"required,omitempty"`
	Default       any      `json:"default,omitempty"`
	Enum          []string `json:"enum,omitempty"`
	Min           *int     `json:"min,omitempty"`
	Max           *int     `json:"max,omitempty"`
	MaxBytes      int      `json:"max_bytes,omitempty"`
	MaxItems      int      `json:"max_items,omitempty"`
	Format        Format   `json:"format,omitempty"`
	Item          *Field   `json:"item,omitempty"`
	InputTypes    []string `json:"input_types,omitempty"`
	InputRequired bool     `json:"input_required,omitempty"`
	Fields        []Field  `json:"fields,omitempty"`
	Requires      []Rule   `json:"requires,omitempty"`
	Excludes      []Rule   `json:"excludes,omitempty"`
	Locked        *Locked  `json:"locked,omitempty"`
}

// Port describes an output a node type may declare.
type Port struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Requires []Rule `json:"requires,omitempty"`
}

// AllowedKeys returns the config keys fs accepts for config: every top-level
// key, plus the keys of each keyless group whose Requires hold for config.
// Rules on input ports are not consulted; key acceptance depends on config
// alone.
func AllowedKeys(fs []Field, config map[string]any) map[string]bool {
	allowed := map[string]bool{}
	for _, f := range Active(fs, config) {
		allowed[f.Key] = true
	}
	return allowed
}

// Active returns the keyed fields that apply to config, in descriptor order:
// every top-level keyed field, plus the fields of each keyless group whose
// Requires hold for config (the same selection as AllowedKeys).
func Active(fs []Field, config map[string]any) []Field {
	var out []Field
	var walk func([]Field)
	walk = func(fs []Field) {
		for _, f := range fs {
			if f.Kind == Group && f.Key == "" {
				if holds(f.Requires, config) {
					walk(f.Fields)
				}
				continue
			}
			out = append(out, f)
		}
	}
	walk(fs)
	return out
}

// Keys returns every key fs can accept for some config, in descriptor order.
func Keys(fs []Field) []string {
	var keys []string
	seen := map[string]bool{}
	var walk func([]Field)
	walk = func(fs []Field) {
		for _, f := range fs {
			if f.Kind == Group && f.Key == "" {
				walk(f.Fields)
				continue
			}
			if !seen[f.Key] {
				seen[f.Key] = true
				keys = append(keys, f.Key)
			}
		}
	}
	walk(fs)
	return keys
}

func holds(rules []Rule, config map[string]any) bool {
	for _, r := range rules {
		if r.Key == "" {
			continue
		}
		v, present := config[r.Key]
		if !present {
			return false
		}
		if r.Equals != "" && !equals(v, r.Equals) {
			return false
		}
	}
	return true
}

// equals compares a scalar config value with a rule's text: a string by its
// text, a boolean as "true" or "false". Other values never match.
func equals(v any, text string) bool {
	switch v := v.(type) {
	case string:
		return v == text
	case bool:
		return (v && text == "true") || (!v && text == "false")
	}
	return false
}

// NamedInputs returns the input ports of a node that the fields active for
// config name: the value of an InputName field, each item of a list of
// InputName, each key of a map whose keys are inputs (InputTypes set), and an
// input a rule in Excludes offers as the field's alternative source (an Agent
// task). It is how a validator learns from descriptors alone which inputs a
// node reads (W_EDGE_INPUT_UNUSED, #180). Values of an unexpected shape name
// nothing; the owning validator reports them.
func NamedInputs(fs []Field, config map[string]any) map[string]bool {
	named := map[string]bool{}
	for _, f := range Active(fs, config) {
		for _, r := range f.Excludes {
			if r.Input != "" {
				named[r.Input] = true
			}
		}
		for _, name := range inputsIn(f, config[f.Key]) {
			named[name] = true
		}
	}
	return named
}

// InputKeys returns the keys of fs whose values or keys can name an input, in
// descriptor order. Rules are not consulted.
func InputKeys(fs []Field) []string {
	var keys []string
	seen := map[string]bool{}
	var walk func([]Field)
	walk = func(fs []Field) {
		for _, f := range fs {
			if f.Kind == Group && f.Key == "" {
				walk(f.Fields)
				continue
			}
			if namesInputs(f) && !seen[f.Key] {
				seen[f.Key] = true
				keys = append(keys, f.Key)
			}
		}
	}
	walk(fs)
	return keys
}

func namesInputs(f Field) bool {
	switch f.Kind {
	case InputName:
		return true
	case List:
		return f.Item != nil && f.Item.Kind == InputName
	case Map:
		return len(f.InputTypes) > 0
	}
	return false
}

// inputsIn returns the input names value holds for f, or nil when f names no
// input or value has another shape.
func inputsIn(f Field, value any) []string {
	if !namesInputs(f) {
		return nil
	}
	switch f.Kind {
	case InputName:
		if name, ok := value.(string); ok {
			return []string{name}
		}
	case List:
		return listStrings(value)
	case Map:
		return mapKeys(value)
	}
	return nil
}

// listStrings returns the string items of a list. Config decoded from YAML
// holds []any; programmatic config may hold []string.
func listStrings(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		var names []string
		for _, item := range items {
			if name, ok := item.(string); ok {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

// mapKeys returns the keys of a map with string keys. Reflection is needed
// because a nested mapping decodes to the graph package's NodeConfig type,
// which this package cannot import (graph imports fields).
func mapKeys(value any) []string {
	entries := reflect.ValueOf(value)
	if entries.Kind() != reflect.Map || entries.Type().Key().Kind() != reflect.String {
		return nil
	}
	var names []string
	for _, key := range entries.MapKeys() {
		names = append(names, key.String())
	}
	return names
}

// IntPtr returns a pointer to v, for Min and Max.
func IntPtr(v int) *int { return &v }
