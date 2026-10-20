package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type State struct {
	Version  int               `json:"version"`
	Sessions map[string]Record `json:"sessions"`
}

func loadState(path string) (State, error) {
	state := State{Version: 1, Sessions: make(map[string]Record)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	if state.Sessions == nil {
		state.Sessions = make(map[string]Record)
	}
	return state, err
}
func saveState(path string, state State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	return commitStateFile(f, f.Name(), path)
}
func defaultStatePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "bus-status", "state.json")
}

// Sync and close the completed temporary file before publishing it atomically.
func commitStateFile(f interface {
	Sync() error
	Close() error
}, temp, path string) error {
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
