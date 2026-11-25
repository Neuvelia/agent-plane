package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckValueRejectsEmptyAndNUL(t *testing.T) {
	cases := []struct {
		name  string
		value Value
		want  error
	}{
		{"empty", NewValue(""), ErrEmptyValue},
		{"zero value", Value{}, ErrEmptyValue},
		{"NUL inside", NewValue("ab\x00cd"), ErrNULValue},
		{"plain", NewValue("ghp_ok"), nil},
		{"only whitespace is a value", NewValue(" "), nil},
	}
	for _, tc := range cases {
		got := CheckValue(tc.value)
		if tc.want == nil && got != nil || tc.want != nil && !errors.Is(got, tc.want) {
			t.Errorf("%s: CheckValue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCheckUTF8RejectsInvalid(t *testing.T) {
	if err := CheckUTF8(NewValue("zażółć")); err != nil {
		t.Errorf("valid UTF-8 rejected: %v", err)
	}
	if err := CheckUTF8(NewValue("ab\xffcd")); !errors.Is(err, ErrNotUTF8) {
		t.Errorf("CheckUTF8 = %v, want ErrNotUTF8", err)
	}
}

func TestCheckErrorsNeverContainValue(t *testing.T) {
	for _, err := range []error{CheckValue(NewValue("a\x00" + canary)), CheckUTF8(NewValue(canary + "\xff"))} {
		if err == nil || strings.Contains(err.Error(), canary) {
			t.Errorf("error %v must exist and not hold the value", err)
		}
	}
}
