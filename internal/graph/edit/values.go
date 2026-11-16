package edit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

// asMapping returns a config mapping value as a map, whichever of the two map
// types it was read or built as.
func asMapping(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case graph.NodeConfig:
		return m, true
	case map[string]any:
		return m, true
	}
	return nil, false
}

// maxValueDepth bounds nesting in a value sent by a client.
const maxValueDepth = 32

// flowItems is the most items a list of scalars has to be written inline.
const flowItems = 8

// decodeValue turns a JSON value into the YAML node to write and the typed
// value graph.Parse will read back from it. Object keys keep the client's
// order; an integer stays an integer and any other number is written so YAML
// reads it as a float.
func decodeValue(raw json.RawMessage) (*yaml.Node, any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	node, typed, err := readValue(dec, 0)
	if err != nil {
		return nil, nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, errors.New("the value must be exactly one JSON value")
	}
	return node, typed, nil
}

func readValue(dec *json.Decoder, depth int) (*yaml.Node, any, error) {
	if depth > maxValueDepth {
		return nil, nil, errors.New("the value is nested too deeply")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, errors.New("the value is not valid JSON: " + err.Error())
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '[' {
			return readList(dec, depth)
		}
		return readObject(dec, depth)
	case string:
		return textNode(t), t, nil
	case json.Number:
		return numberNode(t)
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, t, nil
	default: // nil
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil, nil
	}
}

func readList(dec *json.Decoder, depth int) (*yaml.Node, any, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	items := []any{}
	for dec.More() {
		n, v, err := readValue(dec, depth+1)
		if err != nil {
			return nil, nil, err
		}
		seq.Content = append(seq.Content, n)
		items = append(items, v)
	}
	if _, err := dec.Token(); err != nil { // ]
		return nil, nil, err
	}
	if len(seq.Content) <= flowItems && allScalars(seq.Content) {
		seq.Style = yaml.FlowStyle
	}
	return seq, items, nil
}

func readObject(dec *json.Decoder, depth int) (*yaml.Node, any, error) {
	m := mapNode(false)
	values := graph.NodeConfig{} // graph.Parse reads a nested config mapping as NodeConfig
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key := tok.(string) // the decoder guarantees a string key here
		if _, dup := values[key]; dup {
			return nil, nil, errors.New("the value repeats the key " + strconv.Quote(key))
		}
		n, v, err := readValue(dec, depth+1)
		if err != nil {
			return nil, nil, err
		}
		m.Content = append(m.Content, str(key), n)
		values[key] = v
	}
	if _, err := dec.Token(); err != nil { // }
		return nil, nil, err
	}
	if len(m.Content) == 0 {
		m.Style = yaml.FlowStyle
	}
	return m, values, nil
}

func numberNode(n json.Number) (*yaml.Node, any, error) {
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return intNode(int(i)), int(i), nil
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, nil, errors.New("the number " + string(n) + " cannot be stored")
	}
	text := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(text, ".e") {
		text += ".0" // YAML would read "2" as an integer
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: text}, f, nil
}

// textNode is a string scalar; the encoder quotes it when its plain form
// would read as another type, and a multi-line string is written literally.
func textNode(s string) *yaml.Node {
	n := str(s)
	if strings.Contains(s, "\n") {
		n.Style = yaml.LiteralStyle
	}
	return n
}

func allScalars(nodes []*yaml.Node) bool {
	for _, n := range nodes {
		if n.Kind != yaml.ScalarNode || n.Style == yaml.LiteralStyle {
			return false
		}
	}
	return true
}

// adoptValue makes a replacement node keep what belonged to the old one: its
// comments, and flow style when both are collections.
func adoptValue(old, repl *yaml.Node) {
	repl.HeadComment, repl.LineComment, repl.FootComment = old.HeadComment, old.LineComment, old.FootComment
	collection := func(n *yaml.Node) bool { return n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode }
	if collection(old) && collection(repl) && old.Style&yaml.FlowStyle != 0 {
		repl.Style |= yaml.FlowStyle
	}
}
