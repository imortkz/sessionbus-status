package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (Report, State, time.Time) {
	t.Helper()
	data, err := os.ReadFile("testdata/roster.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := decodeReport(data)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 14, 32, 5, 0, time.UTC)
	state := State{Version: 1, Sessions: map[string]Record{
		"master@macbook":     {Model: "claude-opus-5-5", Ask: "одобрить PLAN bus-status", UpdatedAt: now.Add(-3 * time.Minute)},
		"developer@macbook":  {Model: "gpt-6.1-sol", Status: "волна 1: пишу сборщик", UpdatedAt: now.Add(-45 * time.Second)},
		"researcher@macbook": {Model: "claude-sonnet-5-5"},
		"bus-status@macbook": {Product: "bus-status", Ask: "collector must stay hidden"},
	}}
	return report, state, now
}
func TestRenderGolden(t *testing.T) {
	report, state, now := fixture(t)
	want := `bus-status · 14:32:05 · macbook    ⚠ 1 ждёт тебя

⚠ openclaw-peer@macbook    claude · claude-opus-5-5
  │  ЖДЁТ: одобрить PLAN bus-status  3m
  ├─ ● developer-bus-status@macbook    codex · gpt-6.1-sol
  │    волна 1: пишу сборщик  45s
  └─ ○ researcher-bus-status@macbook    claude · claude-sonnet-5-5
       нет статуса
● orphan@macbook    dsh
     нет статуса
── office: недоступен (offline)
`
	if got := Render(report, state, now, false); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	colored := Render(report, state, now, true)
	if strings.Count(colored, "\x1b[31m") != 2 || strings.Count(colored, "\x1b[0m") != 2 {
		t.Fatalf("red node lines: %q", colored)
	}
}
func TestRenderOrphansHostsAndErrors(t *testing.T) {
	report, state, now := fixture(t)
	report.Complete = false
	report.Error = "partial"
	report.Remote = append(report.Remote, Host{Host: "remote", Sessions: []Row{{SessionID: "remote-a", Product: "codex-peer"}}})
	got := Render(report, state, now, false)
	for _, want := range []string{"\n● orphan@macbook", "── office: недоступен (offline)", "── remote\n○ codex · remote-a", "федерация неполная", "partial"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	data, _ := os.ReadFile("testdata/roster.json")
	if got := Frame(data, []byte("command failed"), errors.New("exit 1"), state, now, false); !strings.Contains(got, "openclaw-peer") || strings.Contains(got, "command failed") {
		t.Fatal(got)
	}
	// A waiting grandchild lifts its entire branch above a running root.
	report.Local.Sessions = append(report.Local.Sessions, Row{SessionID: "grandchild", Owner: "researcher@macbook", Product: "codex-peer"})
	state.Sessions["master@macbook"] = Record{}
	state.Sessions["grandchild"] = Record{Ask: "question"}
	got = Render(report, state, now, false)
	if strings.Index(got, "openclaw-peer") > strings.Index(got, "orphan@macbook") || strings.Index(got, "researcher-bus") > strings.Index(got, "developer-bus") || !strings.Contains(got, "⚠ codex · grandchi") {
		t.Fatal(got)
	}
}
func TestBellOnNewAsk(t *testing.T) {
	a := map[string]bool{"a": true}
	ab := map[string]bool{"a": true, "b": true}
	if !newAsks(nil, a) || newAsks(a, a) || !newAsks(a, ab) || newAsks(ab, a) || newAsks(a, nil) {
		t.Fatal("bell transitions incorrect")
	}
}
func TestRenderSanitizesInputs(t *testing.T) {
	report, state, now := fixture(t)
	poison := "\x1b]0;injected\a\x1b[31m\x00\x7f\u009b"
	report.Local.Host += poison
	report.Local.Sessions[0].Name += poison
	report.Local.Sessions[0].Product += poison
	report.Remote[0].Host += poison
	report.Remote[0].Error += poison
	report.Error = poison
	state.Sessions["master@macbook"] = Record{Ask: poison, Model: poison, Status: poison}
	for _, got := range []string{Render(report, state, now, false), Frame([]byte("invalid"), []byte(poison), errors.New("bad"), state, now, false), Frame(nil, nil, errors.New(poison), state, now, false)} {
		for _, r := range got {
			if r != '\n' && (r < 32 || r == 127 || r >= 128 && r <= 159) {
				t.Fatalf("control %U in %q", r, got)
			}
		}
	}
	for _, raw := range []string{"{}", "null", `{"schema":"wrong"}`} {
		if _, err := decodeReport([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
}

func TestRenderStatusOnlySanitization(t *testing.T) {
	report, state, now := fixture(t)
	state.Sessions["developer@macbook"] = Record{Status: "working\x1b[31m\a\x00\u009b"}
	got := Render(report, state, now, false)
	if !strings.Contains(got, "working[31m") {
		t.Fatalf("missing status: %q", got)
	}
	for _, r := range got {
		if r != '\n' && (r < 32 || r == 127 || r >= 128 && r <= 159) {
			t.Fatalf("status leaked %U: %q", r, got)
		}
	}
}
func TestRenderRunningBeforeName(t *testing.T) {
	report, _, now := fixture(t)
	report.Local.Sessions = []Row{
		{SessionID: "idle", Name: "aaa-idle", Product: "codex-peer"},
		{SessionID: "running", Name: "zzz-running", Product: "codex-peer", Running: true},
	}
	got := Render(report, State{}, now, false)
	if a, b := strings.Index(got, "● zzz-running"), strings.Index(got, "○ aaa-idle"); a < 0 || b < 0 || a > b {
		t.Fatalf("running must precede idle regardless of name: %s", got)
	}
}
func TestFrameErrorFallbackText(t *testing.T) {
	_, state, now := fixture(t)
	got := Frame([]byte("invalid"), []byte("  stderr diagnosis\x1b\n"), errors.New("run diagnosis"), state, now, false)
	if !strings.Contains(got, "Ошибка roster: stderr diagnosis\n") || strings.Contains(got, "run diagnosis") {
		t.Fatalf("stderr fallback: %q", got)
	}
	got = Frame([]byte("invalid"), nil, errors.New("run diagnosis"), state, now, false)
	if !strings.Contains(got, "Ошибка roster: run diagnosis\n") {
		t.Fatalf("run error fallback: %q", got)
	}
}

func TestRenderOmitsRemoteCollector(t *testing.T) {
	report, state, now := fixture(t)
	report.Remote = append(report.Remote, Host{Host: "remote", Sessions: []Row{{SessionID: "collector-remote", Name: "hidden-remote", Product: "bus-status"}}})
	state.Sessions["collector-remote"] = Record{Ask: "hidden ask"}
	got := Render(report, state, now, false)
	if strings.Contains(got, "hidden-remote") || strings.Contains(got, "collector must stay hidden") || len(askIDs(report, state)) != 1 {
		t.Fatalf("collector is visible: %s", got)
	}
}

func TestRenderPeerActivity(t *testing.T) {
	r, s, now := fixture(t)
	s.Sessions["master@macbook"] = Record{Status: "old", UpdatedAt: now.Add(-time.Hour)}
	got := Render(r, s, now, false)
	if !strings.Contains(got, "◐ openclaw-peer") || !strings.Contains(got, "● developer-bus") {
		t.Fatal(got)
	}
	r.Local.Sessions[2].Running = false
	got = Render(r, s, now, false)
	if !strings.Contains(got, "○ openclaw-peer") {
		t.Fatal(got)
	}
}
