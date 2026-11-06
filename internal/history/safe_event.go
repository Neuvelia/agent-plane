package history

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"agentplane/internal/events"
)

// ParseSafeEvent accepts only the closed, typed event schema. Unlike general
// projections, its fixed JSON field names are not user-supplied values.
// Callers must sanitize content first; this constructor checks both raw byte
// fields (before base64) and decoded string values as a final guard.
func ParseSafeEvent(event events.Event, forbidden []string) (SafeJSON, error) {
	if event.IdentityRedacted || event.SchemaVersion < 1 || event.SchemaVersion > events.SchemaVersion || event.Sequence == 0 || event.RunID == "" || event.GraphID == "" || event.GraphVersion == "" || event.Timestamp.IsZero() {
		return SafeJSON{}, errors.New("safe event identity or schema is invalid")
	}
	if typedEventContains(reflect.ValueOf(event), forbidden) {
		return SafeJSON{}, errors.New("safe event contains a configured private value")
	}
	if err := event.Validate(); err != nil {
		return SafeJSON{}, errors.New("safe event is invalid")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return SafeJSON{}, errors.New("safe event cannot be encoded")
	}
	if len(encoded) > maxIndexedProjectionBytes {
		return SafeJSON{}, ErrPageTooLarge
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return SafeJSON{}, errors.New("safe event cannot be decoded")
	}
	for _, value := range forbidden {
		if value != "" && containsEventValue(decoded, value) {
			return SafeJSON{}, errors.New("safe event contains a configured private value")
		}
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return SafeJSON{}, errors.New("safe event cannot be canonicalized")
	}
	if len(canonical) > maxIndexedProjectionBytes {
		return SafeJSON{}, ErrPageTooLarge
	}
	sum := sha256.Sum256(canonical)
	return SafeJSON{data: canonical, digest: hex.EncodeToString(sum[:])}, nil
}

func containsEventValue(value any, forbidden string) bool {
	switch item := value.(type) {
	case string:
		return strings.Contains(item, forbidden)
	case []any:
		for _, child := range item {
			if containsEventValue(child, forbidden) {
				return true
			}
		}
	case map[string]any:
		for _, child := range item {
			if containsEventValue(child, forbidden) {
				return true
			}
		}
	}
	return false
}

func typedEventContains(value reflect.Value, forbidden []string) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		return !value.IsNil() && typedEventContains(value.Elem(), forbidden)
	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return true
		}
		for _, secret := range forbidden {
			if secret != "" && strings.Contains(value.String(), secret) {
				return true
			}
		}
	case reflect.Slice:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			for _, secret := range forbidden {
				if secret != "" && bytes.Contains(value.Bytes(), []byte(secret)) {
					return true
				}
			}
			return false
		}
		fallthrough
	case reflect.Array:
		for i := range value.Len() {
			if typedEventContains(value.Index(i), forbidden) {
				return true
			}
		}
	case reflect.Struct:
		for i := range value.NumField() {
			if value.Type().Field(i).PkgPath == "" && typedEventContains(value.Field(i), forbidden) {
				return true
			}
		}
	case reflect.Map:
		// The event schema has no maps. Reject any later dynamic-key payload
		// until its private-key policy is specified explicitly.
		return true
	}
	return false
}
