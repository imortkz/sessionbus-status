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
		if line = cleanText(line, 200); line != "" {
			d.last = strings.TrimPrefix(line, uiAppName+": ")
			d.dirty = true
		}
	}
	n := 0
	err := os.MkdirAll(filepath.Dir(d.path), 0700)
	if err == nil {
		var f *os.File
		f, err = os.OpenFile(d.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			n, err = f.Write(p)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		d.last = cleanText(uiDiagnosticLogError+err.Error(), 200)
		d.dirty = true
		return n, err
	}
	return len(p), nil
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
