package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func mustTarget(t *testing.T, name, value string) ScanTarget {
	t.Helper()
	target, err := NewScanTarget(name, NewValue(value))
	if err != nil {
		t.Fatalf("NewScanTarget(%q): %v", name, err)
	}
	return target
}

func TestNewScanTargetRejectsIllegalNameAndEmptyValue(t *testing.T) {
	for name, tc := range map[string]struct{ name, value string }{
		"uppercase name": {"Token", "x"},
		"empty name":     {"", "x"},
		"empty value":    {"token", ""},
	} {
		if _, err := NewScanTarget(tc.name, NewValue(tc.value)); !errors.Is(err, ErrInvalidScanTarget) {
			t.Errorf("%s: err = %v, want ErrInvalidScanTarget", name, err)
		}
	}
}

func TestScanTargetNeverFormatsOrMarshalsValue(t *testing.T) {
	const canary = "scan-canary-value"
	target := mustTarget(t, "gh_token", canary)
	holder := struct{ T ScanTarget }{T: target}
	renderings := []string{
		fmt.Sprintf("%v", target), fmt.Sprintf("%+v", target), fmt.Sprintf("%#v", target),
		fmt.Sprintf("%s", target), fmt.Sprintf("%x", target), fmt.Sprintf("%+v", &target),
		fmt.Sprintf("%+v", holder), fmt.Sprintf("%#v", holder), fmt.Sprint([]ScanTarget{target}),
	}
	for _, got := range renderings {
		if strings.Contains(got, canary) {
			t.Errorf("rendering leaked the value: %q", got)
		}
	}
	if got := fmt.Sprintf("%v", target); got != "ScanTarget(gh_token)" {
		t.Errorf("%%v = %q, want ScanTarget(gh_token)", got)
	}
	if _, err := json.Marshal(target); err == nil {
		t.Error("json.Marshal succeeded; a scan target must not serialize")
	}
	if _, err := target.MarshalText(); err == nil {
		t.Error("MarshalText succeeded; a scan target must not serialize")
	}
}

func TestMatchReportsOnlyContainedSecretsSorted(t *testing.T) {
	targets := []ScanTarget{
		mustTarget(t, "zeta", "zzz-value"), mustTarget(t, "alpha", "aaa-value"),
		mustTarget(t, "absent", "never-there"), mustTarget(t, "alpha", "aaa-value"),
		mustTarget(t, "long", "much longer than the data"),
	}
	data := []byte("prefix aaa-value middle zzz-value suffix")
	got := Match(data, targets)
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Match = %v, want %v", got, want)
	}
}

func TestMatchIsByteExactAndFindsBinary(t *testing.T) {
	target := mustTarget(t, "bin", "\x00\xffsecret\x01")
	if got := Match([]byte("\x10\x00\xffsecret\x01\x20"), []ScanTarget{target}); len(got) != 1 {
		t.Errorf("binary value not found: %v", got)
	}
	if got := Match([]byte("\x00\xffSECRET\x01"), []ScanTarget{target}); len(got) != 0 {
		t.Errorf("match must be case sensitive: %v", got)
	}
	if got := Match(nil, []ScanTarget{target}); len(got) != 0 {
		t.Errorf("empty data matched: %v", got)
	}
}

func TestDefaultScanLimitsAreTheSpecifiedBounds(t *testing.T) {
	want := ScanLimits{Files: 256, FileBytes: 8 << 20, TotalBytes: 32 << 20}
	if got := DefaultScanLimits(); got != want {
		t.Errorf("DefaultScanLimits = %+v, want %+v", got, want)
	}
}

func TestErrUnobservableIsASentinel(t *testing.T) {
	wrapped := fmt.Errorf("scan: %w", ErrUnobservable)
	if !errors.Is(wrapped, ErrUnobservable) {
		t.Error("ErrUnobservable cannot be matched through wrapping")
	}
}
