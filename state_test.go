package main

import (
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
