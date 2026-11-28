package cliproc_test

import (
	"io"
	"strings"
	"testing"

	"agentplane/internal/agent/cliproc"
)

func TestCaptureLimitCannotBeBypassedByIOCopy(t *testing.T) {
	b := cliproc.NewLimitedBuffer(3)
	if _, e := io.Copy(b, io.LimitReader(strings.NewReader("12345"), 5)); e != nil {
		t.Fatal(e)
	}
	if b.Len() != 3 || !b.Overflow() || b.String() != "123" {
		t.Fatalf("capture length=%d overflow=%v value=%q", b.Len(), b.Overflow(), b.String())
	}
}

func TestLimitedBufferWithinLimitDoesNotOverflow(t *testing.T) {
	b := cliproc.NewLimitedBuffer(5)
	if n, e := b.Write([]byte("12345")); n != 5 || e != nil || b.Overflow() || string(b.Bytes()) != "12345" {
		t.Fatalf("n=%d err=%v overflow=%v", n, e, b.Overflow())
	}
}

func TestUniqueJSONAcceptsAndRejects(t *testing.T) {
	deep := strings.Repeat("[", 70) + strings.Repeat("]", 70)
	cases := map[string]bool{
		`{"a":1,"b":[true,null,{"c":"d"}]}`: true,
		`"scalar"`:                          true,
		`{"a":1,"a":2}`:                     false,
		`{"a":{"b":1,"b":2}}`:               false,
		"\"\xff\"":                          false,
		`{"a":1} {"b":2}`:                   false,
		`{"a":`:                             false,
		deep:                                false,
	}
	for input, ok := range cases {
		if err := cliproc.UniqueJSON([]byte(input)); (err == nil) != ok {
			t.Errorf("%.40q: err=%v, want ok=%v", input, err, ok)
		}
	}
}
