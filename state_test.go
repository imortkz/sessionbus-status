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
	want := State{Version: 2, Sessions: map[string]Record{"a": {Product: "codex-peer", Status: "working", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}}
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

func TestHistoryCapAndV1Compat(t *testing.T) {
	now := time.Now().UTC()
	rec := Record{}
	for i := 0; i < 25; i++ {
		rec = Apply(rec, []Update{{"status", fmt.Sprint(i)}}, now.Add(time.Duration(i)*time.Second))
	}
	if len(rec.History) != 20 || rec.History[0].Text != "5" || rec.History[19].Text != "24" || rec.History[0].Kind != "status" {
		t.Fatalf("history: %+v", rec.History)
	}
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte(`{"version":1,"sessions":{"old":{"status":"old"}}}`), 0600)
	s, err := loadState(p)
	if err != nil || len(s.Sessions["old"].History) != 0 {
		t.Fatal(s, err)
	}
	s.Sessions["new"] = rec
	if err = saveState(p, s); err != nil {
		t.Fatal(err)
	}
	s, err = loadState(p)
	if err != nil || s.Version != 2 || len(s.Sessions["new"].History) != 20 {
		t.Fatal(s, err)
	}
}
