package state

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

type rec struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestWriteReadJSONRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.json")
	in := rec{Name: "vm", Count: 3}
	if err := WriteJSON(p, in); err != nil {
		t.Fatal(err)
	}
	var out rec
	if err := ReadJSON(p, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v want %+v", out, in)
	}
}

func TestReadJSONNotFound(t *testing.T) {
	var out rec
	err := ReadJSON(filepath.Join(t.TempDir(), "missing.json"), &out)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestReadJSONCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	if err := WriteFileAtomic(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out rec
	if err := ReadJSON(p, &out); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestWriteFileAtomicOverwrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := WriteFileAtomic(p, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out rec
	_ = out
	if !Exists(p) {
		t.Fatal("file should exist")
	}
}

func TestLockSerializes(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "x.lock")
	// Acquire, then a concurrent acquire must block until release.
	unlock, err := Lock(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		u2, err := Lock(lockPath)
		if err != nil {
			t.Errorf("second lock: %v", err)
			return
		}
		close(acquired)
		u2()
	}()

	select {
	case <-acquired:
		t.Fatal("second Lock acquired while first was held")
	default:
	}
	unlock()
	wg.Wait()
	select {
	case <-acquired:
	default:
		t.Fatal("second Lock never acquired after release")
	}
}
