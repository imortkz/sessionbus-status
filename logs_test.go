package main

import (
	"github.com/klauspost/compress/zstd"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putLog(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLocate(t *testing.T) {
	home := t.TempDir()
	for _, tt := range []struct{ id, product, path, kind string }{
		{"abc", "claude-peer", ".claude/projects/p/abc.jsonl", "claude"},
		{"def", "codex-peer", ".codex/sessions/2026/10/20/rollout-t-def.jsonl", "codex"},
		{"ghi", "codex-peer", ".codex/archived_sessions/rollout-t-ghi.jsonl", "codex"},
		{"session-123", "sessionbus-dsh", ".dsh/sessions/p/session-123/session.v3.jsonl.zstd", "dsh"},
	} {
		p := filepath.Join(home, tt.path)
		putLog(t, p, nil)
		got, kind, err := LocateLog(home, tt.id, tt.product)
		if err != nil || got != p || kind != tt.kind {
			t.Fatal(got, kind, err)
		}
		got, _, err = LocateLog(home, tt.id, "unknown")
		if err != nil || got != p {
			t.Fatal(got, err)
		}
	}
	if _, _, err := LocateLog(home, "missing", "unknown"); err == nil {
		t.Fatal("missing accepted")
	}
	if _, _, err := LocateLog(home, "abc@remote", "claude-peer"); err == nil {
		t.Fatal("remote accepted")
	}
}
func TestParseIncremental(t *testing.T) {
	dsh := `{"type":"assistant/message","data":{"message":{"content":[{"type":"text","text":"hello"},{"type":"reasoning","text":"thought"},{"type":"tool-call","name":"shell","arguments":{"command":"pwd"}}]}}}
{"type":"tool/result","data":{"message":{"content":[{"type":"tool-result","content":[{"type":"text","text":"result"}]}]}}}
{"type":"user/message","data":{"content":[{"type":"text","text":"prompt"}]}}
`
	for _, kind := range []string{"claude", "codex", "dsh"} {
		t.Run(kind, func(t *testing.T) {
			data := []byte(dsh)
			if kind != "dsh" {
				var err error
				data, err = os.ReadFile("testdata/logs/" + kind + ".jsonl")
				if err != nil {
					t.Fatal(err)
				}
			}
			enc, _ := zstd.NewWriter(nil)
			defer enc.Close()
			encode := func(b []byte) []byte {
				if kind == "dsh" {
					return enc.EncodeAll(b, nil)
				}
				return b
			}
			p := filepath.Join(t.TempDir(), "log")
			putLog(t, p, encode(data))
			r := LogReader{path: p, kind: kind}
			lines, err := r.Read()
			if err != nil || len(lines) != 5 {
				t.Fatalf("%+v %v", lines, err)
			}
			for i, k := range []string{"TEXT", "THINK", "TOOL", "RESULT", "SYS"} {
				if lines[i].Kind != k {
					t.Fatal(lines)
				}
				if strings.ContainsAny(lines[i].Text, "\x1b\a\u202e") {
					t.Fatal(lines)
				}
			}
			if lines, err = r.Read(); err != nil || len(lines) != 0 {
				t.Fatal(lines, err)
			}
			putLog(t, p, encode(append(append([]byte{}, data...), data...)))
			if lines, err = r.Read(); err != nil || len(lines) != 5 {
				t.Fatal(lines, err)
			}
			putLog(t, p, encode(data))
			if lines, err = r.Read(); err != nil || len(lines) != 5 {
				t.Fatal("shrink", lines, err)
			}
			if kind != "dsh" {
				putLog(t, p, append(data, []byte(`{"type":`)...))
				r = LogReader{path: p, kind: kind}
				if lines, err = r.Read(); err != nil || len(lines) != 5 {
					t.Fatal(lines, err)
				}
				f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
				f.WriteString("\"event_msg\",\"payload\":{\"type\":\"agent_message\",\"message\":\"tail\"}}\n")
				f.Close()
				lines, err = r.Read()
				if err != nil || (kind == "codex" && (len(lines) != 1 || lines[0].Text != "tail")) {
					t.Fatal(lines, err)
				}
			} else {
				first := enc.EncodeAll(data, nil)
				tail := enc.EncodeAll(data, nil)
				putLog(t, p, append(first, tail[:len(tail)/2]...))
				r = LogReader{path: p, kind: kind}
				if lines, err = r.Read(); err != nil || len(lines) < 5 {
					t.Fatal("partial zstd", lines, err)
				}
				putLog(t, p, append(first, tail...))
				if lines, err = r.Read(); err != nil || len(lines) != 5 {
					t.Fatal("completed zstd", lines, err)
				}
			}
		})
	}
}
func TestParseLimits(t *testing.T) {
	for _, tt := range []struct {
		kind  string
		limit int
	}{{"TEXT", 300}, {"TOOL", 120}, {"RESULT", 140}, {"SYS", 200}, {"THINK", 200}} {
		if got := logLine(tt.kind, strings.Repeat("я", 400), ""); len([]rune(got.Text)) != tt.limit {
			t.Fatal(tt, got)
		}
	}
}
