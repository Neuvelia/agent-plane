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
	if got := fmt.Sprintf("%v", env); got != "[secret env: 1 variables]" {
		t.Errorf("%%v = %q", got)
	}
}

func TestEnvRefusesMarshaling(t *testing.T) {
	env := NewEnv(bindings("TOKEN", canary))
	if out, err := json.Marshal(env); err == nil || strings.Contains(string(out), canary) {
		t.Errorf("json.Marshal = %q, %v; want an error", out, err)
	}
}

func TestEnvPickReturnsOnlyRequestedVariablesOnce(t *testing.T) {
	env := NewEnv([]Binding{
		{Variable: "A", Value: NewValue("a-value")},
		{Variable: "B", Value: NewValue("b-value")},
		{Variable: "A", Value: NewValue("duplicate")},
	})
	got := env.Pick(map[string]string{"A": "x", "MISSING": "y"})
	if len(got) != 1 || got[0] != "A=a-value" {
		t.Fatalf("Pick = %v, want only A=a-value", got)
	}
	if len(Env{}.Pick(map[string]string{"A": "x"})) != 0 {
		t.Error("an empty Env produced entries")
	}
}

func TestEnvTargetsNameEachGrantedSecretOnceSorted(t *testing.T) {
	env := NewEnv([]Binding{
		{Variable: "B_TOKEN", Secret: "zeta_token", Value: NewValue("zeta-value")},
		{Variable: "A_TOKEN", Secret: "alpha_token", Value: NewValue("alpha-value")},
		{Variable: "C_TOKEN", Secret: "alpha_token", Value: NewValue("alpha-value")},
		{Variable: "EMPTY", Secret: "empty_token", Value: NewValue("")},
	})
	var names []string
	for _, target := range env.Targets() {
		names = append(names, target.Name())
	}
	if got := strings.Join(names, ","); got != "alpha_token,zeta_token" {
		t.Fatalf("target names = %q, want alpha_token,zeta_token", got)
	}
}

func TestEnvTargetsNeverFormatTheValue(t *testing.T) {
	env := NewEnv([]Binding{{Variable: "A_TOKEN", Secret: "alpha_token", Value: NewValue("alpha-value-canary")}})
	if printed := fmt.Sprintf("%v %+v %#v %s", env.Targets(), env.Targets(), env.Targets(), env.Targets()); strings.Contains(printed, "alpha-value-canary") {
		t.Fatalf("printing targets revealed the value: %s", printed)
	}
}
