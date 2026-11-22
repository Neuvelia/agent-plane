package secrets

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const canary = "s3cr3t-canary-value"

func TestValueNeverFormats(t *testing.T) {
	v := NewValue(canary)
	holder := struct{ V Value }{V: v}
	renderings := map[string]string{
		"%v": fmt.Sprintf("%v", v), "%+v": fmt.Sprintf("%+v", v), "%#v": fmt.Sprintf("%#v", v),
		"%s": fmt.Sprintf("%s", v), "%q": fmt.Sprintf("%q", v), "%x": fmt.Sprintf("%x", v), "%d": fmt.Sprintf("%d", v),
		"Sprint": fmt.Sprint(v), "pointer": fmt.Sprintf("%+v", &v),
		"struct": fmt.Sprintf("%+v", holder), "struct %#v": fmt.Sprintf("%#v", holder),
	}
	for name, got := range renderings {
		if strings.Contains(got, canary) {
			t.Errorf("%s leaked the value: %q", name, got)
		}
	}
	if got := fmt.Sprintf("%v", v); got != "[secret]" {
		t.Errorf("%%v = %q, want [secret]", got)
	}
}

func TestValueRefusesMarshaling(t *testing.T) {
	v := NewValue(canary)
	if out, err := json.Marshal(v); err == nil || strings.Contains(string(out), canary) {
		t.Errorf("json.Marshal = %q, %v; want an error", out, err)
	}
	if out, err := json.Marshal(struct{ V Value }{v}); err == nil || strings.Contains(string(out), canary) {
		t.Errorf("json.Marshal(struct) = %q, %v; want an error", out, err)
	}
	if out, err := v.MarshalText(); err == nil || len(out) != 0 {
		t.Errorf("MarshalText = %q, %v; want an error", out, err)
	}
}

func TestValueRevealReturnsOriginal(t *testing.T) {
	if got := NewValue(canary).Reveal(); got != canary {
		t.Errorf("Reveal = %q, want %q", got, canary)
	}
	if got := (Value{}).Reveal(); got != "" {
		t.Errorf("zero Value Reveal = %q, want empty", got)
	}
}
