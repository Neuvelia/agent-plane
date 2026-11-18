package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agentplane/internal/testenv"
)

func TestStoreJSONIsCreatedLazilyOnFirstWrite(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "runs")}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "store.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Check must not create store.json: %v", err)
	}
	write(t, s, runA, started(runA, time.Now().UTC()))
	data, err := os.ReadFile(filepath.Join(s.Dir, "store.json"))
	if err != nil || strings.TrimSpace(string(data)) != `{"store_format":1}` {
		t.Fatalf("%v %q", err, data)
	}
	if f, err := s.ReadFormat(); err != nil || f.StoreFormat != CurrentFormat {
		t.Fatalf("%v %+v", err, f)
	}
	testenv.RequireUnixPermissions(t)
	if info, err := os.Stat(filepath.Join(s.Dir, "store.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("store.json must be 0600: %v %v", info.Mode().Perm(), err)
	}
}

func TestAbsentStoreJSONIsFormatZeroAndReadable(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA, started(runA, now), finished(runA, 2, "succeeded", now))
	if err := os.Remove(filepath.Join(s.Dir, "store.json")); err != nil {
		t.Fatal(err)
	}
	if f, err := s.ReadFormat(); err != nil || f.StoreFormat != 0 {
		t.Fatalf("absent store.json is format 0: %v %+v", err, f)
	}
	if list, err := s.List(); err != nil || len(list) != 1 || list[0].Status != "succeeded" {
		t.Fatalf("%v %+v", err, list)
	}
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestNewerStoreFormatIsReadOnlyWithAnActionableError(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	now := time.Now().UTC()
	write(t, s, runA, started(runA, now), finished(runA, 2, "succeeded", now))
	newer := fmt.Sprintf(`{"store_format":%d,"added_later":true}`, CurrentFormat+1)
	if err := os.WriteFile(filepath.Join(s.Dir, "store.json"), []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	var fe *FormatError
	if err := s.Check(); !errors.As(err, &fe) || fe.Found != CurrentFormat+1 || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("Check: %v", err)
	}
	if err := s.NewWriter(nil).Append(started(runB, now)); !errors.As(err, &fe) {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, runB)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused write must not create a run directory: %v", err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 {
		t.Fatalf("%v %+v", err, list)
	}
	if rec, err := s.Read(runA); err != nil || rec.Summary.Status != "succeeded" {
		t.Fatalf("%v %+v", err, rec.Summary)
	}
}

func TestInvalidStoreJSONIsRejectedAndRetentionIsParsed(t *testing.T) {
	for name, content := range map[string]string{
		"negative retention": `{"store_format":1,"retention":{"max_runs":-1}}`,
		"not json":           `{`,
	} {
		t.Run(name, func(t *testing.T) {
			s := Store{Dir: t.TempDir()}
			if err := os.WriteFile(filepath.Join(s.Dir, "store.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := s.Check(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	s := Store{Dir: t.TempDir()}
	valid := `{"store_format":1,"retention":{"max_runs":5,"max_age_days":2,"max_total_bytes":9},"future":1}`
	if err := os.WriteFile(filepath.Join(s.Dir, "store.json"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if f, err := s.ReadFormat(); err != nil || f.Retention == nil || f.Retention.MaxRuns != 5 {
		t.Fatalf("%v %+v", err, f)
	}
}

func TestConcurrentFirstWritersCreateStoreJSONOnce(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		run := fmt.Sprintf("%08d-1111-4111-8111-111111111111", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := s.NewWriter(nil)
			if err := w.Append(started(run, time.Now().UTC())); err != nil {
				errs <- err
				return
			}
			errs <- w.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".store-") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
	if f, err := s.ReadFormat(); err != nil || f.StoreFormat != CurrentFormat {
		t.Fatalf("%v %+v", err, f)
	}
	if list, err := s.List(); err != nil || len(list) != writers {
		t.Fatalf("%v %d", err, len(list))
	}
}
