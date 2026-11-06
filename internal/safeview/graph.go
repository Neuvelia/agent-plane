package safeview

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"time"
	"unicode/utf8"

	"agentplane/internal/graph"
	"agentplane/internal/history"
)

// ProjectGraph captures a safe semantic view of a saved graph. The versioned
// positional form is an inspection artifact, never executable YAML. Its hash
// is the frozen semantic version, not the digest of this redacted view.
func (p *Projector) ProjectGraph(definition *graph.Definition, graphHash string) (history.SafeJSON, bool, error) {
	if p == nil || p.redactor == nil || definition == nil || definition.ID == "" || graphHash == "" {
		return history.SafeJSON{}, false, errors.New("safe graph projector is unavailable")
	}
	if p.identity(definition.ID, graphHash) {
		return history.SafeJSON{}, false, errors.New("required graph identity is unsafe")
	}
	budget := &launchBudget{}
	complete := true
	name := ""
	if utf8.ValidString(definition.Name) {
		name = p.content(definition.Name)
		complete = name == definition.Name
	} else {
		complete = false
	}
	metadata, metadataComplete, err := p.graphAny(reflect.ValueOf(definition.Metadata), 0, budget)
	if err != nil {
		return history.SafeJSON{}, false, err
	}
	complete = complete && metadataComplete

	units := make([]any, 0, len(definition.Units))
	for _, id := range definition.Units {
		if p.identity(id) {
			return history.SafeJSON{}, false, errors.New("required unit identity is unsafe")
		}
		units = append(units, id)
	}
	inputs := make([]any, 0, len(definition.Inputs))
	for _, id := range sortedGraphKeys(definition.Inputs) {
		if p.unsafeLaunchKey(id) {
			complete = false
			continue
		}
		input := definition.Inputs[id]
		code, ok := graphValueCode(input.Type)
		if !ok {
			return history.SafeJSON{}, false, errors.New("unsupported graph input type")
		}
		inputs = append(inputs, []any{id, code, input.Required})
	}
	nodes := make([]any, 0, len(definition.Nodes))
	for _, id := range sortedGraphKeys(definition.Nodes) {
		if p.identity(id) {
			return history.SafeJSON{}, false, errors.New("required node identity is unsafe")
		}
		node := definition.Nodes[id]
		nodeType, typeComplete := p.graphNodeType(node.Type)
		complete = complete && typeComplete
		portsIn, inComplete, err := p.graphPorts(node.Inputs)
		if err != nil {
			return history.SafeJSON{}, false, err
		}
		complete = complete && inComplete
		portsOut, outComplete, err := p.graphPorts(node.Outputs)
		if err != nil {
			return history.SafeJSON{}, false, err
		}
		complete = complete && outComplete
		config, configComplete, err := p.graphAny(reflect.ValueOf(node.Config), 0, budget)
		if err != nil {
			return history.SafeJSON{}, false, err
		}
		complete = complete && configComplete
		var loop any
		if node.Loop != nil {
			loopValue, loopComplete, err := p.graphLoop(node.Loop, budget)
			if err != nil {
				return history.SafeJSON{}, false, err
			}
			loop, complete = loopValue, complete && loopComplete
		}
		nodes = append(nodes, []any{id, nodeType, portsIn, config, portsOut, loop})
	}
	edges := make([]any, 0, len(definition.Edges))
	for _, edge := range definition.Edges {
		from, to := string(edge.From), string(edge.To)
		if p.identity(from, to) {
			complete = false
			continue
		}
		edges = append(edges, []any{from, to})
	}
	bindings := make([]any, 0, len(definition.GraphInputBindings))
	for _, binding := range definition.GraphInputBindings {
		from, to := string(binding.From), string(binding.To)
		if p.identity(from, to) {
			complete = false
			continue
		}
		bindings = append(bindings, []any{from, to})
	}
	outputs := make([]any, 0, len(definition.Outputs))
	for _, id := range sortedGraphKeys(definition.Outputs) {
		from := string(definition.Outputs[id].From)
		if p.unsafeLaunchKey(id) || p.identity(from) {
			complete = false
			continue
		}
		outputs = append(outputs, []any{id, from})
	}
	root := []any{1, graphHash, int(definition.SchemaVersion), definition.ID, name, metadata, units, inputs, nodes, edges, bindings, outputs}
	encoded, err := json.Marshal(root)
	if err != nil || len(encoded) > maxLaunchJSONBytes {
		return history.SafeJSON{}, false, errors.New("safe graph projection exceeds limit")
	}
	safe, err := history.ParseSafeJSON(encoded, p.forbidden)
	return safe, complete, err
}

func sortedGraphKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func graphValueCode(value graph.ValueType) (int, bool) {
	switch value {
	case graph.ValueTypeString:
		return 1, true
	case graph.ValueTypeNumber:
		return 2, true
	case graph.ValueTypeBoolean:
		return 3, true
	case graph.ValueTypeJSON:
		return 4, true
	case graph.ValueTypeArtifactRef:
		return 5, true
	default:
		return 0, false
	}
}

func (p *Projector) graphNodeType(value string) (any, bool) {
	switch value {
	case "shell":
		return 1, true
	case "agent":
		return 2, true
	case "gate":
		return 3, true
	case "human":
		return 4, true
	default:
		if !utf8.ValidString(value) {
			return "", false
		}
		redacted := p.content(value)
		return redacted, redacted == value
	}
}

func (p *Projector) graphPorts(ports map[string]graph.PortDefinition) ([]any, bool, error) {
	projected := make([]any, 0, len(ports))
	complete := true
	for _, id := range sortedGraphKeys(ports) {
		if p.unsafeLaunchKey(id) {
			complete = false
			continue
		}
		port := ports[id]
		code, ok := graphValueCode(port.Type)
		if !ok {
			return nil, false, errors.New("unsupported graph port type")
		}
		projected = append(projected, []any{id, code, port.Required})
	}
	return projected, complete, nil
}

func (p *Projector) graphLoop(loop *graph.LoopDefinition, budget *launchBudget) (any, bool, error) {
	inputs, complete, err := p.graphPorts(loop.Gate.Inputs)
	if err != nil {
		return nil, false, err
	}
	config, configComplete, err := p.graphAny(reflect.ValueOf(loop.Gate.Config), 0, budget)
	if err != nil {
		return nil, false, err
	}
	complete = complete && configComplete
	attempts, attemptsComplete, err := p.graphScalar(loop.MaxAttempts)
	if err != nil {
		return nil, false, err
	}
	complete = complete && attemptsComplete
	bindings := make([]any, 0, len(loop.Bindings))
	for _, binding := range loop.Bindings {
		if p.identity(binding.From, binding.To) {
			complete = false
			continue
		}
		bindings = append(bindings, []any{binding.From, binding.To})
	}
	return []any{attempts, inputs, config, bindings}, complete, nil
}

// graphScalar checks the exact encoding that will be persisted, including
// json.Marshal's float normalization (which differs from fmt.Sprint).
func (p *Projector) graphScalar(value any) (any, bool, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false, errors.New("graph projection scalar cannot be encoded")
	}
	if p.identity(string(encoded)) {
		return nil, false, nil
	}
	return value, true, nil
}

// graphAny walks decoded YAML config/metadata without serializing the source.
// Unsupported subtrees and unsafe members are omitted and mark a partial view.
func (p *Projector) graphAny(value reflect.Value, depth int, budget *launchBudget) (any, bool, error) {
	if depth > maxLaunchDepth {
		return nil, false, errors.New("graph projection nesting limit exceeded")
	}
	if err := budget.item(); err != nil {
		return nil, false, err
	}
	if !value.IsValid() {
		return nil, true, nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, true, nil
		}
		depth++
		if depth > maxLaunchDepth {
			return nil, false, errors.New("graph projection pointer depth exceeded")
		}
		value = value.Elem()
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		encoded, err := json.Marshal(value.Interface())
		if err != nil {
			return nil, false, errors.New("graph timestamp cannot be encoded")
		}
		var timestamp string
		if err := json.Unmarshal(encoded, &timestamp); err != nil {
			return nil, false, errors.New("graph timestamp cannot be decoded")
		}
		return []any{6, p.content(timestamp)}, false, nil
	}
	if value.Type() == reflect.TypeOf(json.Number("")) {
		number := value.Interface().(json.Number)
		return p.graphScalar(number)
	}
	switch value.Kind() {
	case reflect.String:
		text := value.String()
		if !utf8.ValidString(text) {
			return nil, false, errors.New("graph projection contains invalid UTF-8")
		}
		redacted := p.content(text)
		return redacted, redacted == text, nil
	case reflect.Bool:
		return p.graphScalar(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return p.graphScalar(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return p.graphScalar(value.Uint())
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, false, errors.New("graph projection contains a nonfinite number")
		}
		return p.graphScalar(number)
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return nil, false, nil
		}
		keys := value.MapKeys()
		if len(keys) > maxLaunchItems {
			return nil, false, errors.New("graph projection item limit exceeded")
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		object := make(map[string]any, len(keys))
		complete := true
		for _, key := range keys {
			if err := budget.item(); err != nil {
				return nil, false, err
			}
			name := key.String()
			if p.unsafeLaunchKey(name) {
				complete = false
				continue
			}
			item, itemComplete, err := p.graphAny(value.MapIndex(key), depth+1, budget)
			if err != nil {
				return nil, false, err
			}
			if item != nil || itemComplete {
				object[name] = item
			}
			complete = complete && itemComplete
		}
		return object, complete, nil
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil, false, nil
		}
		if value.Len() > maxLaunchItems {
			return nil, false, errors.New("graph projection item limit exceeded")
		}
		items := make([]any, 0, value.Len())
		complete := true
		for i := 0; i < value.Len(); i++ {
			item, itemComplete, err := p.graphAny(value.Index(i), depth+1, budget)
			if err != nil {
				return nil, false, err
			}
			if item != nil || itemComplete {
				items = append(items, item)
			}
			complete = complete && itemComplete
		}
		return items, complete, nil
	default:
		return nil, false, nil
	}
}
