package main

import (
	"os"
	"testing"
)

func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	saved := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = saved }()
	code := fn()
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(data)
}

func TestVersionOutput(t *testing.T) {
	for _, arg := range []string{"--version", "-version", "version"} {
		t.Run(arg, func(t *testing.T) {
			code, out := captureStdout(t, func() int { return run([]string{arg}) })
			if code != 0 || out != "sessionbus-status development\n" {
				t.Fatalf("exit=%d output=%q", code, out)
			}
		})
	}
}

func TestVersionLine(t *testing.T) {
	savedRelease, savedRevision := release, revision
	defer func() { release, revision = savedRelease, savedRevision }()
	release, revision = "v0.1.0-alpha.1", "abc123"
	if got := versionLine(); got != "sessionbus-status v0.1.0-alpha.1 (abc123)" {
		t.Fatalf("got %q", got)
	}
	revision = ""
	if got := versionLine(); got != "sessionbus-status v0.1.0-alpha.1" {
		t.Fatalf("got %q", got)
	}
}
