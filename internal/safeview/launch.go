package safeview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"agentplane/internal/graph"
	"agentplane/internal/history"
)

const (
	maxLaunchJSONBytes = 8 << 20
	maxLaunchDepth     = 64
	maxLaunchItems     = 100000
)

// CredentialShapedKey is the ADR-0018 classifier for untrusted structured
// object members. It deliberately uses ASCII normalization, not Unicode folds.
func CredentialShapedKey(key string) bool {
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

// ProjectLaunchValues produces a bounded, detached, safe view of typed launch
// inputs. The completeness flag is false when any value is redacted or a key
// or opaque identity is omitted. Callers store it in the run completeness.
func (p *Projector) ProjectLaunchValues(inputs map[string]graph.Value) (history.SafeJSON, bool, error) {
	if p == nil || p.redactor == nil {
		return history.SafeJSON{}, false, errors.New("safe launch projector is unavailable")
	}
	if len(inputs) > maxLaunchItems {
		return history.SafeJSON{}, false, errors.New("launch input item limit exceeded")
	}
	budget := &launchBudget{}
	complete := true
	projected := make([]any, 0, len(inputs))
	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := inputs[key]
		if err := budget.item(); err != nil {
			return history.SafeJSON{}, false, err
		}
		if p.unsafeLaunchKey(key) {
			complete = false
			continue
		}
		item, retained, itemComplete, err := p.projectTypedValue(value, budget)
		if err != nil {
			return history.SafeJSON{}, false, err
		}
		if !itemComplete {
			complete = false
		}
		if retained {
			projected = append(projected, []any{key, item})
		}
	}
	// Positional arrays keep fixed DTO field names from colliding with a
	// short frozen value in the general key-and-value safety parser.
	encoded, err := json.Marshal([]any{1, projected})
	if err != nil || len(encoded) > maxLaunchJSONBytes {
		return history.SafeJSON{}, false, errors.New("safe launch projection exceeds limit")
	}
	safe, err := history.ParseSafeJSON(encoded, p.forbidden)
	return safe, complete, err
}

func (p *Projector) projectTypedValue(value graph.Value, budget *launchBudget) (any, bool, bool, error) {
	switch value.Type() {
	case graph.ValueTypeString:
		text, _ := value.AsString()
		if !utf8.ValidString(text) {
			return nil, false, false, nil
		}
		redacted := p.content(text)
		return []any{1, redacted}, true, redacted == text, nil
	case graph.ValueTypeNumber:
		number, _ := value.AsNumber()
		return []any{2, number}, true, true, nil
	case graph.ValueTypeBoolean:
		boolean, _ := value.AsBoolean()
		return []any{3, boolean}, true, true, nil
	case graph.ValueTypeArtifactRef:
		ref, _ := value.AsArtifactRef()
		if p.identity(ref) {
			return nil, false, false, nil
		}
		return []any{5, ref}, true, true, nil
	case graph.ValueTypeJSON:
		raw, _ := value.AsJSON()
		if len(raw) > maxLaunchJSONBytes || !utf8.Valid(raw) {
			return nil, false, false, errors.New("launch JSON is too large or has invalid UTF-8")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		item, complete, err := p.projectJSONValue(decoder, 0, budget)
		if err != nil {
			return nil, false, false, err
		}
		if _, err := decoder.Token(); err != io.EOF {
			return nil, false, false, errors.New("launch JSON has trailing data")
		}
		return []any{4, item}, true, complete, nil
	default:
		return nil, false, false, errors.New("unsupported typed launch value")
	}
}

type launchBudget struct{ items int }

func (b *launchBudget) item() error {
	b.items++
	if b.items > maxLaunchItems {
		return errors.New("launch JSON item limit exceeded")
	}
	return nil
}

func (p *Projector) unsafeLaunchKey(key string) bool {
	return CredentialShapedKey(key) || p.identity(key)
}

func (p *Projector) projectJSONValue(decoder *json.Decoder, depth int, budget *launchBudget) (any, bool, error) {
	if depth > maxLaunchDepth {
		return nil, false, errors.New("launch JSON nesting limit exceeded")
	}
	if err := budget.item(); err != nil {
		return nil, false, err
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, false, fmt.Errorf("read launch JSON: %w", err)
	}
	switch value := token.(type) {
	case string:
		if !utf8.ValidString(value) {
			return nil, false, errors.New("launch JSON contains invalid UTF-8")
		}
		redacted := p.content(value)
		return redacted, redacted == value, nil
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			seen := make(map[string]bool)
			complete := true
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, false, err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return nil, false, errors.New("launch JSON has duplicate or invalid object key")
				}
				seen[key] = true
				item, itemComplete, err := p.projectJSONValue(decoder, depth+1, budget)
				if err != nil {
					return nil, false, err
				}
				if !itemComplete {
					complete = false
				}
				if p.unsafeLaunchKey(key) {
					complete = false
					continue
				}
				object[key] = item
			}
			if _, err := decoder.Token(); err != nil {
				return nil, false, err
			}
			return object, complete, nil
		case '[':
			items := make([]any, 0)
			complete := true
			for decoder.More() {
				item, itemComplete, err := p.projectJSONValue(decoder, depth+1, budget)
				if err != nil {
					return nil, false, err
				}
				items = append(items, item)
				if !itemComplete {
					complete = false
				}
			}
			if _, err := decoder.Token(); err != nil {
				return nil, false, err
			}
			return items, complete, nil
		}
		return nil, false, errors.New("unexpected launch JSON delimiter")
	default:
		return token, true, nil
	}
}
