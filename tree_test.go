package main

import (
	"errors"
	"fmt"
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
		"master@laptop":     {Model: "claude-opus-5-5", Ask: "одобрить PLAN bus-status", UpdatedAt: now.Add(-3 * time.Minute)},
		"developer@laptop":  {Model: "gpt-6.1-sol", Status: "волна 1: пишу сборщик", UpdatedAt: now.Add(-45 * time.Second)},
		"researcher@laptop": {Model: "claude-sonnet-5-5"},
		"bus-status@laptop": {Product: "bus-status", Ask: "collector must stay hidden"},
	}}
	return report, state, now
}
func TestRenderGolden(t *testing.T) {
	report, state, now := fixture(t)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		os.WriteFile("testdata/text-table.golden", []byte(Render(report, state, now, false)), 0600)
	}
	data, err := os.ReadFile("testdata/text-table.golden")
	if err != nil {
		t.Fatal(err)
	}
	want := string(data)
	if got := Render(report, state, now, false); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	colored := Render(report, state, now, true)
	if strings.Count(colored, "\x1b[31m") != 1 || strings.Count(colored, "\x1b[0m") != 1 {
		t.Fatalf("red node lines: %q", colored)
	}
}
func TestRenderOrphansHostsAndErrors(t *testing.T) {
	report, state, now := fixture(t)
	report.Complete = false
	report.Error = "partial"
	report.Remote = append(report.Remote, Host{Host: "remote", Sessions: []Row{{SessionID: "remote-a", Product: "codex-peer"}}})
	got := Render(report, state, now, false)
	for _, want := range []string{"\n● orphan", "── server" + uiUnavailable + "offline)", "── remote", "○ codex · remote-a", uiIncomplete, "partial"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	data, _ := os.ReadFile("testdata/roster.json")
	if got := Frame(data, []byte("command failed"), errors.New("exit 1"), state, now, false, nil); !strings.Contains(got, "demo-peer") || strings.Contains(got, "command failed") {
		t.Fatal(got)
	}
	// A waiting grandchild lifts its entire branch above a running root.
	report.Local.Sessions = append(report.Local.Sessions, Row{SessionID: "grandchild", Owner: "researcher@laptop", Product: "codex-peer"})
	state.Sessions["master@laptop"] = Record{}
	state.Sessions["grandchild"] = Record{Ask: "question"}
	got = Render(report, state, now, false)
	if strings.Index(got, "demo-peer") > strings.Index(got, "orphan") || strings.Index(got, "researcher-bus") > strings.Index(got, "developer-bus") || !strings.Contains(got, "codex · grandchi") {
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
	state.Sessions["master@laptop"] = Record{Ask: poison, Model: poison, Status: poison}
	for _, got := range []string{Render(report, state, now, false), Frame([]byte("invalid"), []byte(poison), errors.New("bad"), state, now, false, nil), Frame(nil, nil, errors.New(poison), state, now, false, nil)} {
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
	state.Sessions["developer@laptop"] = Record{Status: "working\x1b[31m\a\x00\u009b"}
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
	got := Frame([]byte("invalid"), []byte("  stderr diagnosis\x1b\n"), errors.New("run diagnosis"), state, now, false, nil)
	if !strings.Contains(got, uiRosterError+"stderr diagnosis\n") || strings.Contains(got, "run diagnosis") {
		t.Fatalf("stderr fallback: %q", got)
	}
	got = Frame([]byte("invalid"), nil, errors.New("run diagnosis"), state, now, false, nil)
	if !strings.Contains(got, uiRosterError+"run diagnosis\n") {
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
	s.Sessions["master@laptop"] = Record{Status: "old", UpdatedAt: now.Add(-time.Hour)}
	got := Render(r, s, now, false)
	if !strings.Contains(got, "◐ ▾ demo-peer") || !strings.Contains(got, "●   ├ developer-bus") {
		t.Fatal(got)
	}
	r.Local.Sessions[2].Running = false
	got = Render(r, s, now, false)
	if !strings.Contains(got, "○ ▾ demo-peer") {
		t.Fatal(got)
	}
}

func TestNeutralFixtureNames(t *testing.T) {
	report, _, _ := fixture(t)
	if report.Local.Host != "laptop" || len(report.Remote) != 1 || report.Remote[0].Host != "server" {
		t.Fatalf("fixture hosts: %+v", report)
	}
	if report.Local.Sessions[0].Name != "demo-peer@laptop" {
		t.Fatalf("fixture name: %q", report.Local.Sessions[0].Name)
	}
}
func TestFilterReportIgnore(t *testing.T) {
	report := Report{Local: Host{Host: "laptop", Sessions: []Row{
		{SessionID: "keep@laptop", Name: "keep@laptop", Kind: "peer"},
		{SessionID: "semi@laptop", Name: "semiurg@laptop", Kind: "peer"},
		{SessionID: "lane@laptop", Name: "semiurg/dev@laptop", Kind: "lane", Owner: "semi@laptop"},
		{SessionID: "sub@laptop", Name: "dev-sub@laptop", Kind: "lane", Owner: "lane@laptop"},
		{SessionID: "orphan@laptop", Name: "semiurg/old@laptop", Kind: "lane", Owner: "gone@laptop"},
		{SessionID: "other@laptop", Name: "innerconnect/rev@laptop", Kind: "lane", Owner: "keep@laptop"},
		{SessionID: "prefixpeer@laptop", Name: "semiurg/x@laptop", Kind: "peer"},
	}}, Remote: []Host{{Host: "office", Sessions: []Row{
		{SessionID: "semi@office", Name: "semiurg@office", Kind: "peer"},
		{SessionID: "lane@office", Name: "innerconnect_tests/dev@office", Kind: "lane", Owner: "semi@office"},
		{SessionID: "semiurg2@office", Name: "semiurg2@office", Kind: "peer"},
	}}}}
	ids := func(r Report) string {
		var out []string
		for _, h := range append([]Host{r.Local}, r.Remote...) {
			for _, row := range h.Sessions {
				out = append(out, row.SessionID)
			}
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		ignore []string
		want   string
	}{
		{nil, ids(report)},
		{[]string{"semiurg"}, "keep@laptop other@laptop prefixpeer@laptop semiurg2@office"},
		{[]string{"semiurg@office"}, "keep@laptop semi@laptop lane@laptop sub@laptop orphan@laptop other@laptop prefixpeer@laptop semiurg2@office"},
		{[]string{"semiurg@laptop", "keep"}, "prefixpeer@laptop semi@office lane@office semiurg2@office"},
	} {
		if got := ids(filterReport(report, tc.ignore)); got != tc.want {
			t.Fatalf("ignore %v: got %q want %q", tc.ignore, got, tc.want)
		}
	}
	if len(report.Local.Sessions) != 7 || len(report.Remote[0].Sessions) != 3 {
		t.Fatal("filterReport mutated its input")
	}
}
func TestIgnoreFlagHidesPeerAndAsk(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	saved := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = saved }()
	statePath := t.TempDir() + "/state.json"
	if err := saveState(statePath, State{Sessions: map[string]Record{"master@laptop": {Ask: "одобрить PLAN"}}}); err != nil {
		t.Fatal(err)
	}
	code := runWatch([]string{"-once", "-roster", "cat testdata/roster.json", "-state", statePath, "--ignore", "nobody", "--ignore", "demo-peer,orphan"})
	data, _ := os.ReadFile(out.Name())
	got := string(data)
	if code != 0 || strings.Contains(got, "demo-peer") || strings.Contains(got, "researcher-bus-status") || strings.Contains(got, "orphan") || strings.Contains(got, "одобрить") || strings.Contains(got, fmt.Sprintf(uiWaitingCount, 1)) {
		t.Fatalf("code %d output:\n%s", code, got)
	}
}
