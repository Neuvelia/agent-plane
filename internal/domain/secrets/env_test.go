package secrets

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func bindings(pairs ...string) []Binding {
	var out []Binding
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Binding{Variable: pairs[i], Value: NewValue(pairs[i+1])})
	}
	return out
}

func TestEnvEnvironIsSortedByUpperCasedVariable(t *testing.T) {
	env := NewEnv(bindings("zeta", "1", "Beta", "2", "ALPHA", "3"))
	want := []string{"ALPHA=3", "Beta=2", "zeta=1"}
	if got := env.Environ(); !reflect.DeepEqual(got, want) {
		t.Errorf("Environ = %v, want %v", got, want)
	}
	if got := env.Names(); !reflect.DeepEqual(got, []string{"ALPHA", "Beta", "zeta"}) {
		t.Errorf("Names = %v", got)
	}
}

func TestEnvDoesNotAliasInput(t *testing.T) {
	input := bindings("A", "one", "B", "two")
	env := NewEnv(input)
	input[0] = Binding{Variable: "Z", Value: NewValue("changed")}
	if got := env.Environ(); !reflect.DeepEqual(got, []string{"A=one", "B=two"}) {
		t.Errorf("Env followed its input slice: %v", got)
	}
}

func TestEnvZeroValueIsEmpty(t *testing.T) {
	var env Env
	if len(env.Environ()) != 0 || len(env.Names()) != 0 {
		t.Errorf("zero Env = %v %v, want empty", env.Environ(), env.Names())
	}
}

func TestEnvNeverFormats(t *testing.T) {
	env := NewEnv(bindings("TOKEN", canary))
	holder := struct{ E Env }{E: env}
	renderings := []string{
		fmt.Sprintf("%v", env), fmt.Sprintf("%+v", env), fmt.Sprintf("%#v", env), fmt.Sprintf("%s", env),
		fmt.Sprintf("%q", env), fmt.Sprintf("%x", env), fmt.Sprintf("%+v", &env),
		fmt.Sprintf("%+v", holder), fmt.Sprintf("%#v", holder),
	}
	for _, got := range renderings {
		if strings.Contains(got, canary) {
			t.Errorf("rendering leaked the value: %q", got)
		}
	}
	if got := fmt.Sprintf("%v", env); got != "[secret env: 1 variable]" {
		t.Errorf("%%v = %q", got)
	}
}

func TestEnvRefusesMarshaling(t *testing.T) {
	env := NewEnv(bindings("TOKEN", canary))
	if out, err := json.Marshal(env); err == nil || strings.Contains(string(out), canary) {
		t.Errorf("json.Marshal = %q, %v; want an error", out, err)
	}
}

func TestEnvFormatAgreesWithCount(t *testing.T) {
	cases := map[string]Env{
		"[secret env: 0 variables]": {},
		"[secret env: 2 variables]": NewEnv(bindings("A", "one", "B", "two")),
	}
	for want, env := range cases {
		if got := fmt.Sprintf("%v", env); got != want {
			t.Errorf("%%v = %q, want %q", got, want)
		}
	}
}
