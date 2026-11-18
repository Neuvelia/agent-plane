package system

import "testing"

func TestIDsAreUniqueAndUUIDShaped(t *testing.T) {
	a, err := IDs{}.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := IDs{}.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	node, err := IDs{}.NewNodeRunID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || string(a) == string(node) || len(a) != 36 || a[14] != '4' {
		t.Fatalf("IDs: %q %q %q", a, b, node)
	}
}
