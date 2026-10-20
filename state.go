package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	state.Version = 2
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
func stateHome() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return dir
}
func defaultStatePath() string {
	return filepath.Join(stateHome(), "sessionbus-status", "state.json")
}
func legacyStatePath() string {
	return filepath.Join(stateHome(), "bus-status", "state.json")
}

// migrateLegacyState copies the pre-rename state file once. It never
// overwrites an existing new file and never removes the old one.
func migrateLegacyState(newPath, oldPath string) error {
	if _, err := os.Lstat(newPath); err == nil || !os.IsNotExist(err) {
		return err
	}
	info, err := os.Stat(oldPath)
	if os.IsNotExist(err) || (err == nil && !info.Mode().IsRegular()) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(newPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
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
	return commitStateFile(f, f.Name(), newPath)
}

// migrateDefaultState runs the legacy copy only when -state was not given.
func migrateDefaultState(flags *flag.FlagSet, path string, warnings io.Writer) {
	explicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "state" {
			explicit = true
		}
	})
	if explicit {
		return
	}
	if err := migrateLegacyState(path, legacyStatePath()); err != nil {
		fmt.Fprintln(warnings, uiAppName+": warning: cannot migrate legacy state:", sanitize(err.Error()))
	}
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
