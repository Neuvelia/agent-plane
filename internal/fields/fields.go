// Package fields describes node configuration for editors (F2.2, ADR-0021).
//
// A descriptor is data owned by the package that validates the field: the
// Shell, Gate and Agent executors and the graph package (human node, loop)
// each export theirs and derive their own allowed-key checks from it, so a
// form generated from descriptors cannot offer a key the engine rejects, and
// a key the engine accepts cannot lack a descriptor. Descriptors carry no
// behavior; the owning validator stays the authority on values.
package fields

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
	var walk func([]Field)
	walk = func(fs []Field) {
		for _, f := range fs {
			if f.Kind == Group && f.Key == "" {
				if holds(f.Requires, config) {
					walk(f.Fields)
				}
				continue
			}
			allowed[f.Key] = true
		}
	}
	walk(fs)
	return allowed
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

// IntPtr returns a pointer to v, for Min and Max.
func IntPtr(v int) *int { return &v }
