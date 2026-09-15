package version

import "testing"

func TestStringIsNotEmpty(t *testing.T) {
	if String() == "" {
		t.Fatal("version must not be empty")
	}
}
