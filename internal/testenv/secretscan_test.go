package testenv

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder is a hand-written testing.TB fake that only counts errors.
type scanRecorder struct {
	testing.TB
	errors int
}

func (*scanRecorder) Helper()                 {}
func (r *scanRecorder) Errorf(string, ...any) { r.errors++ }
func (r *scanRecorder) Fatalf(string, ...any) { r.errors++ }

const scanCanary = "ghp_Scan0Oracle3Canary9xyz"

func leaks(t *testing.T, content string) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &scanRecorder{TB: t}
	AssertNoSecretBytes(r, dir, scanCanary)
	return r.errors > 0
}

func TestAssertNoSecretBytesFindsEveryForm(t *testing.T) {
	forms := map[string]string{
		"raw":     "x " + scanCanary + " y",
		"hex":     hex.EncodeToString([]byte(scanCanary)),
		"percent": url.QueryEscape(scanCanary + "/ "),
		"base64":  base64.StdEncoding.EncodeToString([]byte(scanCanary)),
		"json":    `{"data":"` + base64.StdEncoding.EncodeToString([]byte("log "+scanCanary)) + `"}`,
	}
	for k := 0; k < 3; k++ {
		framed := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", k) + scanCanary + "tail"))
		forms[fmt.Sprintf("base64 alignment %d", k)] = "[" + framed + "]"
	}
	for name, content := range forms {
		t.Run(name, func(t *testing.T) {
			if !leaks(t, content) {
				t.Errorf("the scan missed %q", content)
			}
		})
	}
}

func TestAssertNoSecretBytesAcceptsCleanContent(t *testing.T) {
	if leaks(t, `{"data":"`+base64.StdEncoding.EncodeToString([]byte("nothing here"))+"\"}\nplain text\n") {
		t.Error("clean content was reported as a leak")
	}
}

func TestAssertNoSecretTextFindsLeakInBody(t *testing.T) {
	r := &scanRecorder{TB: t}
	AssertNoSecretText(r, "body", "error: "+scanCanary, scanCanary)
	if r.errors != 1 {
		t.Errorf("errors = %d, want 1", r.errors)
	}
}
