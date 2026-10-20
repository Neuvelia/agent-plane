package desktop

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"agentplane/internal/graph"
)

var ErrInvalidTypedInputs = errors.New("invalid typed graph inputs")

// decodeTypedInputs rejects ambiguous wrapper objects while leaving graph
// declaration checks to BindingPlan.ValidateGraphInputs. JSON payloads stay
// opaque, including their exact accepted bytes and object key order.
func decodeTypedInputs(raw json.RawMessage) (map[string]graph.Value, error) {
	if len(raw) > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	if !utf8.Valid(raw) {
		return nil, ErrInvalidTypedInputs
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidTypedInputs
	}
	values := make(map[string]graph.Value)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidTypedInputs
		}
		name, ok := token.(string)
		if !ok || name == "" {
			return nil, ErrInvalidTypedInputs
		}
		if _, duplicate := values[name]; duplicate {
			return nil, ErrInvalidTypedInputs
		}
		var record json.RawMessage
		if decoder.Decode(&record) != nil {
			return nil, ErrInvalidTypedInputs
		}
		value, err := decodeTypedInput(record)
		if err != nil {
			return nil, ErrInvalidTypedInputs
		}
		values[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalidTypedInputs
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidTypedInputs
	}
	return values, nil
}

func decodeTypedInput(raw json.RawMessage) (graph.Value, error) {
	fields, err := strictObject(raw, "type", "value")
	if err != nil || string(fields["type"]) == "null" || len(fields["value"]) == 0 {
		return graph.Value{}, ErrInvalidTypedInputs
	}
	var kind graph.ValueType
	if json.Unmarshal(fields["type"], &kind) != nil {
		return graph.Value{}, ErrInvalidTypedInputs
	}
	value := fields["value"]
	switch kind {
	case graph.ValueTypeString:
		var text string
		if string(value) == "null" || json.Unmarshal(value, &text) != nil {
			return graph.Value{}, ErrInvalidTypedInputs
		}
		return graph.NewStringValue(text), nil
	case graph.ValueTypeNumber:
		var text string
		if string(value) == "null" || json.Unmarshal(value, &text) != nil {
			return graph.Value{}, ErrInvalidTypedInputs
		}
		return graph.NewNumberValue(json.Number(text))
	case graph.ValueTypeBoolean:
		var boolean bool
		if string(value) == "null" || json.Unmarshal(value, &boolean) != nil {
			return graph.Value{}, ErrInvalidTypedInputs
		}
		return graph.NewBooleanValue(boolean), nil
	case graph.ValueTypeJSON:
		return graph.NewJSONValue(value)
	case graph.ValueTypeArtifactRef:
		var reference string
		if string(value) == "null" || json.Unmarshal(value, &reference) != nil {
			return graph.Value{}, ErrInvalidTypedInputs
		}
		return graph.NewArtifactRefValue(reference)
	default:
		return graph.Value{}, ErrInvalidTypedInputs
	}
}

// encodeTypedInputsExact is an injective, versioned byte encoding for the
// supported graph values. Map iteration and wrapper JSON field order do not
// affect it; JSON value bytes and number spellings do.
func encodeTypedInputsExact(values map[string]graph.Value) ([]byte, error) {
	if len(values) > MaxFrameBytes/4 {
		return nil, ErrFrameTooLarge
	}
	names := make([]string, 0, len(values))
	for name := range values {
		if name == "" || !utf8.ValidString(name) {
			return nil, ErrInvalidTypedInputs
		}
		names = append(names, name)
	}
	sort.Strings(names)
	encoded := []byte("agentplane.inputs.v1\x00")
	encoded = binary.BigEndian.AppendUint32(encoded, uint32(len(names)))
	for _, name := range names {
		value := values[name]
		var payload []byte
		switch value.Type() {
		case graph.ValueTypeString:
			text, _ := value.AsString()
			if !utf8.ValidString(text) {
				return nil, ErrInvalidTypedInputs
			}
			payload = []byte(text)
		case graph.ValueTypeNumber:
			number, _ := value.AsNumber()
			if _, err := graph.NewNumberValue(number); err != nil {
				return nil, ErrInvalidTypedInputs
			}
			payload = []byte(number)
		case graph.ValueTypeBoolean:
			boolean, _ := value.AsBoolean()
			if boolean {
				payload = []byte("true")
			} else {
				payload = []byte("false")
			}
		case graph.ValueTypeJSON:
			payload, _ = value.AsJSON()
			if !json.Valid(payload) {
				return nil, ErrInvalidTypedInputs
			}
		case graph.ValueTypeArtifactRef:
			reference, _ := value.AsArtifactRef()
			if reference == "" || !utf8.ValidString(reference) {
				return nil, ErrInvalidTypedInputs
			}
			payload = []byte(reference)
		default:
			return nil, ErrInvalidTypedInputs
		}
		var err error
		encoded, err = appendInputField(encoded, []byte(name))
		if err == nil {
			encoded, err = appendInputField(encoded, []byte(value.Type()))
		}
		if err == nil {
			encoded, err = appendInputField(encoded, payload)
		}
		if err != nil {
			return nil, err
		}
	}
	return encoded, nil
}

func appendInputField(encoded, field []byte) ([]byte, error) {
	if len(field) > MaxFrameBytes || len(encoded) > MaxFrameBytes-4-len(field) {
		return nil, ErrFrameTooLarge
	}
	encoded = binary.BigEndian.AppendUint32(encoded, uint32(len(field)))
	encoded = append(encoded, field...)
	return encoded, nil
}
