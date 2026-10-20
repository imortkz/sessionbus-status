package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "state.json")
	empty, err := loadState(path)
	if err != nil || empty.Version != 1 || len(empty.Sessions) != 0 {
		t.Fatalf("missing: %+v %v", empty, err)
	}
	want := State{Version: 1, Sessions: map[string]Record{"a": {Product: "codex-peer", Status: "working", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for p, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("mode %s: %v %v", p, info, err)
		}
	}
	want.Sessions["a"] = Record{Status: "replacement"}
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err = loadState(path)
	if err != nil || got.Sessions["a"].Status != "replacement" {
		t.Fatal("atomic replacement failed", err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(path); err == nil {
		t.Fatal("malformed state accepted")
	}
}

type observedStateFile struct {
	*os.File
	events  *[]string
	syncErr error
}

func (f observedStateFile) Sync() error {
	*f.events = append(*f.events, "sync")
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}
func (f observedStateFile) Close() error {
	*f.events = append(*f.events, "close")
	return f.File.Close()
}
func TestStateSyncBeforeRename(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.CreateTemp(dir, "pending")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.WriteString("new"); err != nil {
				t.Fatal(err)
			}
			var events []string
			var syncErr error
			if fail {
				syncErr = errors.New("sync failed")
			}
			err = commitStateFile(observedStateFile{f, &events, syncErr}, f.Name(), path)
			if !reflect.DeepEqual(events, []string{"sync", "close"}) {
				t.Fatalf("events %v", events)
			}
			got, _ := os.ReadFile(path)
			if fail {
				if !errors.Is(err, syncErr) || string(got) != "old" {
					t.Fatalf("sync failure: %q %v", got, err)
				}
			} else if err != nil || string(got) != "new" {
				t.Fatalf("commit: %q %v", got, err)
			}
		})
	}
}
