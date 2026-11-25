package main

import (
	"encoding/json"
	"reflect"
	"strings"

	"agentplane/internal/domain/graph"
)

// storedMaterial is everything a run may copy or record besides its events: the
// exact graph bytes the store snapshots, the parsed definition and the graph
// inputs as supplied and as parsed. It implements runtime.StoredMaterial so
// preflight can refuse a run whose material holds a resolved secret value
// without that value leaving the runtime (F3.8 requirement 12, ADR-0029).
type storedMaterial struct {
	graphBytes []byte
	def        *graph.Definition
	flags      inputFlags
	values     map[string]graph.Value
}

// Locations reports "graph", "inputs.<key>" or "inputs" (an input name held the
// text) for every place holds is true. Only raw bytes are compared; encoded
// forms are the redactor's concern, not the guard's.
func (m storedMaterial) Locations(holds func(text string) bool) []string {
	var found []string
	if holds(string(m.graphBytes)) || definitionHolds(m.def, holds) {
		found = append(found, "graph")
	}
	for _, item := range m.flags {
		key, raw, _ := strings.Cut(item, "=")
		switch {
		case holds(key):
			found = append(found, "inputs")
		case holds(raw) || anyHolds(valueStrings(m.values[key]), holds):
			found = append(found, "inputs."+key)
		case holds(item):
			// A value containing "=" can span key and value; the key may be part of it.
			found = append(found, "inputs")
		}
	}
	return found
}

func anyHolds(texts []string, holds func(string) bool) bool {
	for _, text := range texts {
		if holds(text) {
			return true
		}
	}
	return false
}

// valueStrings lists the texts a graph input value carries. graph.Value has
// only unexported fields, so a reflection walk would see nothing: the typed
// accessors are the only way in. JSON is also decoded, because an escaped
// string differs from its raw bytes.
func valueStrings(value graph.Value) []string {
	var texts []string
	if text, ok := value.AsString(); ok {
		texts = append(texts, text)
	}
	if number, ok := value.AsNumber(); ok {
		texts = append(texts, number.String())
	}
	if ref, ok := value.AsArtifactRef(); ok {
		texts = append(texts, ref)
	}
	if raw, ok := value.AsJSON(); ok {
		texts = append(texts, string(raw))
		var decoded any
		if json.Unmarshal(raw, &decoded) == nil {
			texts = appendJSONTexts(texts, decoded)
		}
	}
	return texts
}

// appendJSONTexts collects every string leaf and object key of a decoded value.
func appendJSONTexts(texts []string, decoded any) []string {
	switch typed := decoded.(type) {
	case string:
		return append(texts, typed)
	case []any:
		for _, item := range typed {
			texts = appendJSONTexts(texts, item)
		}
	case map[string]any:
		for key, item := range typed {
			texts = appendJSONTexts(append(texts, key), item)
		}
	}
	return texts
}

// definitionHolds reports whether holds is true for any string in the parsed
// definition, map keys included. It walks the whole structure by reflection so
// no field can be forgotten, and it compares parsed values, so YAML escapes,
// folding and unicode escapes cannot hide a secret from the byte-level check on
// the stored snapshot. Unexported fields are skipped.
func definitionHolds(def *graph.Definition, holds func(text string) bool) bool {
	return def != nil && valueHolds(reflect.ValueOf(def), holds)
}

func valueHolds(v reflect.Value, holds func(text string) bool) bool {
	switch v.Kind() {
	case reflect.String:
		return holds(v.String())
	case reflect.Interface, reflect.Pointer:
		return !v.IsNil() && valueHolds(v.Elem(), holds)
	case reflect.Slice, reflect.Array:
		return elementsHold(v, holds)
	case reflect.Map:
		return entriesHold(v, holds)
	case reflect.Struct:
		return fieldsHold(v, holds)
	}
	return false
}

func elementsHold(v reflect.Value, holds func(text string) bool) bool {
	for i := 0; i < v.Len(); i++ {
		if valueHolds(v.Index(i), holds) {
			return true
		}
	}
	return false
}

func entriesHold(v reflect.Value, holds func(text string) bool) bool {
	for _, key := range v.MapKeys() {
		if valueHolds(key, holds) || valueHolds(v.MapIndex(key), holds) {
			return true
		}
	}
	return false
}

func fieldsHold(v reflect.Value, holds func(text string) bool) bool {
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).IsExported() && valueHolds(v.Field(i), holds) {
			return true
		}
	}
	return false
}
