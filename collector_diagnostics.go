package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The collector writes only here in TUI mode. The UI samples the latest line
// through a command, so a collector callback never mutates the model.
type collectorDiagnostics struct {
	path          string
	mu            sync.Mutex
	pending, last string
	dirty         bool
}

const collectorLogLimit = 1 << 20

func (d *collectorDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending += string(p)
	for {
		line, rest, ok := strings.Cut(d.pending, "\n")
		if !ok {
			break
		}
		d.pending = rest
		line = cleanText(line, 200)
		if line != "" {
			d.last = strings.TrimPrefix(line, uiAppName+": ")
			d.dirty = true
		}
		if err := d.appendLine(line + "\n"); err != nil {
			d.last = cleanText(uiDiagnosticLogError+err.Error(), 200)
			d.dirty = true
			return 0, err
		}
	}
	return len(p), nil
}

// Rotate before appending a complete cleaned line, retaining just one generation.
func (d *collectorDiagnostics) appendLine(line string) error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0700); err != nil {
		return err
	}
	info, err := os.Stat(d.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && info.Size()+int64(len(line)) > collectorLogLimit {
		if err = os.Rename(d.path, d.path+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(d.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (d *collectorDiagnostics) take() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.dirty {
		return ""
	}
	d.dirty = false
	return d.last
}

type collectorDiagnosticMsg struct {
	source *collectorDiagnostics
	line   string
}

func (m *Model) pollCollectorDiagnostics() tea.Cmd {
	d := m.diagnostics
	if d == nil {
		return nil
	}
	return func() tea.Msg { return collectorDiagnosticMsg{d, d.take()} }
}
