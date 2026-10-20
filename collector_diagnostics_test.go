package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsCleanAndRotate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "collector.log")
	d := &collectorDiagnostics{path: p}
	if _, err := d.Write([]byte("part\x1b")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write([]byte("\u202ewhole\tline\r\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "partwhole line\n" {
		t.Fatal("raw diagnostic bytes", string(data), err)
	}
	const limit = 1 << 20
	for _, letter := range []string{"x", "y"} {
		fill := strings.Repeat(letter+"\n", limit/2-1)
		if err := os.WriteFile(p, []byte(fill), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Write([]byte("new line\x1b\u200b\n")); err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(p)
		if err != nil || string(current) != "new line\n" || len(current) > limit {
			t.Fatal(string(current), err)
		}
		previous, err := os.ReadFile(p + ".1")
		if err != nil || string(previous) != fill || len(previous) > limit {
			t.Fatal("bad rotation", len(previous), err)
		}
	}
	files, err := filepath.Glob(p + ".*")
	if err != nil || len(files) != 1 {
		t.Fatal("more than one generation", files, err)
	}
}
