package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"regexp"
)

func stripANSI(s string) string { return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(s, "") }

func testModel(t *testing.T) (*Model, Report, State, time.Time) {
	t.Helper()
	r, s, now := fixture(t)
	m := newModel(tuiConfig{ctx: context.Background(), statePath: t.TempDir() + "/missing", home: t.TempDir(), groups: "bus-status,test", interval: time.Second, timeout: time.Second, sound: "test.aiff", muted: true})
	m.now = func() time.Time { return now }
	m.collectorDecided = true
	m.collectorMode = "внешний"
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(stateMsg{state: s})
	m.Update(rosterMsg{report: r})
	return m, r, s, now
}
func press(m *Model, key string) tea.Cmd {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	}
	_, cmd := m.Update(msg)
	return cmd
}
func TestKeysAndLayout(t *testing.T) {
	m, r, _, _ := testModel(t)
	if m.selected != "master@macbook" {
		t.Fatal(m.selected)
	}
	press(m, "down")
	if m.selected != "developer@macbook" {
		t.Fatal(m.selected)
	}
	m.Update(rosterMsg{report: r})
	if m.selected != "developer@macbook" {
		t.Fatal("selection reset")
	}
	press(m, "up")
	press(m, "left")
	if len(m.visible()) != 2 {
		t.Fatal("collapse", m.visible())
	}
	press(m, "right")
	if len(m.visible()) != 4 {
		t.Fatal("expand")
	}
	press(m, "l")
	if m.pane != "log" {
		t.Fatal(m.pane)
	}
	press(m, "L")
	if m.pane != "logFull" {
		t.Fatal(m.pane)
	}
	press(m, "esc")
	if m.pane != "history" {
		t.Fatal(m.pane)
	}
	press(m, "s")
	if m.muted {
		t.Fatal("mute toggle")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 60, Height: 20}, {Width: 100, Height: 30}} {
		m.Update(size)
		v := stripANSI(m.View())
		if len(strings.Split(v, "\n")) > size.Height {
			t.Fatalf("too tall: %s", v)
		}
		for _, line := range strings.Split(v, "\n") {
			if lipgloss.Width(line) > size.Width {
				t.Fatalf("too wide %d: %q", lipgloss.Width(line), line)
			}
		}
		for _, want := range []string{"bus-status", "сборщик:", "↑↓", "←→", "l ", "L ", "f ", "s ", "q "} {
			if !strings.Contains(v, want) {
				t.Fatalf("missing %q: %s", want, v)
			}
		}
	}
	// The tree follows a selection below its visible area.
	for i := 0; i < 50; i++ {
		r.Local.Sessions = append(r.Local.Sessions, Row{SessionID: fmt.Sprintf("z%02d", i), Name: fmt.Sprintf("zz%02d", i), Product: "codex-peer"})
	}
	m.Update(rosterMsg{report: r})
	for i := 0; i < 54; i++ {
		press(m, "down")
	}
	if !strings.Contains(stripANSI(m.View()), "zz49") {
		t.Fatal("selection is offscreen", m.View())
	}
	if cmd := press(m, "q"); cmd == nil {
		t.Fatal("quit missing")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
}
func TestVisibleOutput(t *testing.T) {
	m, r, s, now := testModel(t)
	s.Sessions["developer@macbook"] = Record{Status: "working", History: []Entry{{At: now, Kind: "status", Text: "history\x1b\u202e\u200b"}}}
	s.Sessions["researcher@macbook"] = Record{Status: "old", UpdatedAt: now.Add(-11 * time.Minute)}
	m.Update(stateMsg{state: s})
	press(m, "down")
	v := stripANSI(m.View())
	if !strings.Contains(v, "history") || !strings.Contains(v, "история: developer") {
		t.Fatal(v)
	}
	press(m, "l")
	m.Update(logMsg{gen: m.generation, lines: []LogLine{{Kind: "TEXT", Text: "log\x1b\u202e\u200b"}}})
	v = stripANSI(m.View())
	if !strings.Contains(v, "лог: developer") || !strings.Contains(v, "TEXT  log") {
		t.Fatal(v)
	}
	for _, r := range v {
		if r == '\u202e' || r == '\u200b' || r == '\x1b' {
			t.Fatal("unsafe output", v)
		}
	}
	press(m, "down")
	if len(m.logLines) != 0 || strings.Contains(stripANSI(m.View()), "TEXT  log") {
		t.Fatal("log retained on selection change")
	}
	// Empty leaf disappears; parent with lanes remains.
	r.Local.Sessions = append(r.Local.Sessions, Row{SessionID: "empty", Name: "empty-peer", Product: "codex-peer"})
	m.Update(rosterMsg{report: r})
	press(m, "f")
	v = stripANSI(m.View())
	if strings.Contains(v, "empty-peer") || !strings.Contains(v, "openclaw-peer") || !strings.Contains(v, "фильтр") {
		t.Fatal(v)
	}
	// Stale rendering has a real ANSI faint sequence, independently of color profile.
	if !strings.Contains(m.View(), "\x1b[2m") {
		t.Fatal("stale not faint", m.View())
	}
	press(m, "L")
	v = stripANSI(m.View())
	if !strings.Contains(v, "лог:") || !strings.Contains(v, "q выход") {
		t.Fatal(v)
	}
}

type fakeNotifier struct {
	sounds []string
	bodies []string
}

func (f *fakeNotifier) Sound(p string)            { f.sounds = append(f.sounds, p) }
func (f *fakeNotifier) Notify(title, body string) { f.bodies = append(f.bodies, title+": "+body) }

// Execute command trees as Tea does, without running timers or external I/O.
func runActions(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runActions(c)
		}
	}
}
func TestAlertDispatch(t *testing.T) {
	m, r, s, now := testModel(t)
	n := &fakeNotifier{}
	m.notifier = n
	m.muted = false
	m.alerts = Alerts{}
	m.alerts.Tick(now, nil)
	_, cmd := m.Update(rosterMsg{report: r})
	runActions(cmd)
	if len(n.sounds) != 1 || n.sounds[0] != "test.aiff" || len(n.bodies) != 1 || !strings.Contains(n.bodies[0], "одобрить PLAN") {
		t.Fatal(n)
	}
	now = now.Add(30 * time.Second)
	m.now = func() time.Time { return now }
	_, cmd = m.Update(stateMsg{state: s})
	runActions(cmd)
	if len(n.sounds) != 1 {
		t.Fatal(n)
	}
	now = now.Add(30 * time.Second)
	_, cmd = m.Update(stateMsg{state: s})
	runActions(cmd)
	if len(n.sounds) != 2 {
		t.Fatal(n)
	}
	press(m, "s")
	now = now.Add(time.Minute)
	_, cmd = m.Update(stateMsg{state: s})
	runActions(cmd)
	if len(n.sounds) != 2 {
		t.Fatal(n)
	}
}
func TestLogReadGeneration(t *testing.T) {
	m, _, _, _ := testModel(t)
	calls := 0
	m.readLog = func(reader *LogReader, row visibleNode) (*LogReader, []LogLine, error) {
		calls++
		return &LogReader{}, []LogLine{{Kind: "TEXT", Text: "fresh"}}, nil
	}
	press(m, "l")
	_, cmd := m.Update(logTick{})
	if calls != 0 || !m.inFlight || cmd == nil {
		t.Fatal("I/O ran in Update")
	}
	_, again := m.Update(logTick{})
	if again != nil {
		t.Fatal("overlapping read")
	}
	msg := cmd()
	if calls != 1 {
		t.Fatal(calls)
	}
	oldGen := m.generation
	press(m, "down")
	if m.generation == oldGen {
		t.Fatal("no generation change")
	}
	m.Update(msg)
	if len(m.logLines) != 0 || m.inFlight {
		t.Fatal("stale result applied")
	}
	_, cmd = m.Update(logTick{})
	m.Update(cmd())
	if len(m.logLines) != 1 || m.logLines[0].Text != "fresh" || m.inFlight {
		t.Fatal(m.logLines)
	}
	// Keep only 200 tail lines.
	lines := make([]LogLine, 210)
	for i := range lines {
		lines[i] = LogLine{Kind: "TEXT", Text: fmt.Sprint(i)}
	}
	m.Update(logMsg{gen: m.generation, lines: lines})
	if len(m.logLines) != 200 || m.logLines[0].Text != "10" {
		t.Fatal("tail cap")
	}
}
func TestCollectorDecision(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprint(external), func(t *testing.T) {
			called := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m := newModel(tuiConfig{ctx: ctx, groups: "bus-status,project", statePath: "test-state", muted: true, interval: time.Second})
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m.startCollector = func(got context.Context, p, g string) error {
				called++
				if got != ctx || p != "test-state" || g != "bus-status,project" {
					t.Fatal("collector configuration")
				}
				return nil
			}
			r := Report{Local: Host{Host: "local"}}
			if external {
				r.Local.Sessions = []Row{{Product: "bus-status", Connected: true}}
			} else {
				r.Remote = []Host{{Host: "remote", Sessions: []Row{{Product: "bus-status", Connected: true}}}}
			}
			_, cmd := m.Update(rosterMsg{report: r})
			runActions(cmd)
			want := "свой"
			if external {
				want = "внешний"
			}
			if m.collectorMode != want || called != map[bool]int{false: 1, true: 0}[external] || !strings.Contains(stripANSI(m.View()), "сборщик: "+want) {
				t.Fatal(m.collectorMode, called, m.View())
			}
			m.Update(rosterMsg{report: r})
			if called > 1 {
				t.Fatal("collector restarted")
			}
			if external {
				r.Local.Sessions = nil
				m.Update(rosterMsg{report: r})
				if !strings.Contains(stripANSI(m.View()), "сборщик: не найден") {
					t.Fatal(m.View())
				}
			}
		})
	}
}

func TestNarrowHeader(t *testing.T) {
	m, _, _, _ := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	press(m, "f")
	header := strings.Split(stripANSI(m.View()), "\n")[1]
	for _, want := range []string{"bus-status", "сборщик: внешний", "🔇", "фильтр", "⚠ 1"} {
		if !strings.Contains(header, want) {
			t.Fatalf("missing %q: %s", want, header)
		}
	}
}

func TestLocalLogCommand(t *testing.T) {
	home := t.TempDir()
	data, err := os.ReadFile("testdata/logs/claude.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	putLog(t, filepath.Join(home, ".claude/projects/project/abc.jsonl"), data)
	m := newModel(tuiConfig{home: home, muted: true, interval: time.Second})
	m.collectorDecided = true
	m.Update(rosterMsg{report: Report{Local: Host{Host: "local-alias", Sessions: []Row{{SessionID: "abc@local-alias", Product: "claude-peer"}}}}})
	press(m, "l")
	_, cmd := m.Update(logTick{})
	m.Update(cmd())
	if m.logErr != "" || len(m.logLines) != 5 {
		t.Fatal(m.logErr, m.logLines)
	}
	r := Report{Local: Host{Host: "local-alias"}, Remote: []Host{{Host: "remote", Sessions: []Row{{SessionID: "abc@remote", Product: "claude-peer"}}}}}
	m.Update(rosterMsg{report: r})
	_, cmd = m.Update(logTick{})
	m.Update(cmd())
	if !strings.Contains(m.logErr, "remote-host") || len(m.logLines) != 0 {
		t.Fatal(m.logErr, m.logLines)
	}
}

func TestPeerActivityRefresh(t *testing.T) {
	m, r, s, now := testModel(t)
	s.Sessions["master@macbook"] = Record{Status: "old", UpdatedAt: now.Add(-time.Hour)}
	m.Update(stateMsg{state: s})
	v := stripANSI(m.View())
	if !strings.Contains(v, "◐ openclaw-peer") || !strings.Contains(v, "◐ работают lanes") {
		t.Fatal(v)
	}
	// Stat work is deferred until the command runs, then refreshes activity without opening the log pane.
	m.report = r
	m.config.home = t.TempDir()
	p := filepath.Join(m.config.home, ".claude/projects/p/master.jsonl")
	cmd := m.pollState()
	putLog(t, p, nil)
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
	msg := cmd().(stateMsg)
	msg.state = s
	if !msg.activity["master@macbook"].Equal(now) {
		t.Fatal(msg.activity)
	}
	m.Update(msg)
	if !strings.Contains(stripANSI(m.View()), "● openclaw-peer") {
		t.Fatal(m.View())
	}
	// Missing/old logs fall back to running lanes; no real homes are inspected.
	if err := os.Chtimes(p, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	msg = m.pollState()().(stateMsg)
	msg.state = s
	m.Update(msg)
	if !strings.Contains(stripANSI(m.View()), "◐ openclaw-peer") {
		t.Fatal(m.View())
	}
	for i := range r.Local.Sessions {
		r.Local.Sessions[i].Running = false
	}
	m.Update(rosterMsg{report: r})
	if !strings.Contains(stripANSI(m.View()), "○ openclaw-peer") {
		t.Fatal(m.View())
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if !strings.Contains(stripANSI(m.View()), "◐ работают lanes") {
		t.Fatal(m.View())
	}
}

func TestInitialWindowSize(t *testing.T) {
	m := newModel(tuiConfig{muted: true})
	if got := m.View(); got != "" {
		t.Fatalf("frame before size: %q", got)
	}
	if m.collectorMode != "проверяю…" {
		t.Fatal(m.collectorMode)
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if got := len(strings.Split(m.View(), "\n")); got != 20 {
		t.Fatal("frame height", got)
	}
}

func TestCollectorFailureRetry(t *testing.T) {
	m, _, _, now := testModel(t)
	m.collectorDecided = false
	m.ownCollector = false
	m.report.Local.Sessions = nil
	calls := 0
	m.startCollector = func(context.Context, string, string) error {
		calls++
		return fmt.Errorf("offline\x1b\u202e%s", strings.Repeat("x", 250))
	}
	cmd := m.collectorDecision()
	m.Update(cmd())
	if m.collectorDecided || m.ownCollector || !strings.Contains(stripANSI(m.View()), "Ошибка сборщика: offline") {
		t.Fatal(m.View())
	}
	if len([]rune(m.collectorErr)) != 200 || strings.ContainsAny(m.collectorErr, "\x1b\u202e") {
		t.Fatal(m.collectorErr)
	}
	now = now.Add(9 * time.Second)
	m.now = func() time.Time { return now }
	if cmd = m.collectorDecision(); cmd != nil {
		t.Fatal("early retry")
	}
	now = now.Add(time.Second)
	cmd = m.collectorDecision()
	if cmd == nil {
		t.Fatal("retry missing")
	}
	m.Update(cmd())
	if calls != 2 {
		t.Fatal(calls)
	}
	m.report.Local.Sessions = []Row{{Product: "bus-status", Connected: true}}
	m.collectorDecision()
	if m.collectorMode != "внешний" || m.collectorErr != "" {
		t.Fatal(m.collectorMode, m.collectorErr)
	}
}

func TestCollectorShutdownWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	m := newModel(tuiConfig{ctx: ctx, cancel: cancel, collectorWG: wg})
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.startCollector = func(ctx context.Context, _, _ string) error { close(started); <-ctx.Done(); <-release; return nil }
	cmd := m.collectorDecision()
	go func() { cmd(); close(finished) }()
	<-started
	press(m, "q")
	if waitCollector(wg, 10*time.Millisecond) {
		t.Fatal("quit did not wait for collector")
	}
	close(release)
	if !waitCollector(wg, time.Second) {
		t.Fatal("collector did not finish")
	}
	<-finished
}

func TestAlertRosterFlap(t *testing.T) {
	m, r, s, now := testModel(t)
	n := &fakeNotifier{}
	m.notifier = n
	m.muted = false
	m.alerts = Alerts{}
	runActions(m.alertCmd())
	if len(n.sounds) != 0 {
		t.Fatal("startup burst")
	}
	now = now.Add(10 * time.Second)
	m.now = func() time.Time { return now }
	m.report.Local.Sessions = nil
	m.rebuild()
	runActions(m.alertCmd())
	m.report = r
	m.rebuild()
	runActions(m.alertCmd())
	if len(n.sounds) != 0 {
		t.Fatal("roster flap re-fired")
	}
	now = now.Add(50 * time.Second)
	runActions(m.alertCmd())
	if len(n.sounds) != 1 {
		t.Fatal("reminder lost", n)
	}
	rec := s.Sessions["master@macbook"]
	rec.Ask = ""
	s.Sessions["master@macbook"] = rec
	_, cmd := m.Update(stateMsg{state: s})
	runActions(cmd)
	rec.Ask = "new question"
	s.Sessions["master@macbook"] = rec
	_, cmd = m.Update(stateMsg{state: s})
	runActions(cmd)
	if len(n.sounds) != 2 {
		t.Fatal("state clear not observed", n)
	}
}

func TestAlertsWaitForInitialState(t *testing.T) {
	r, s, _ := fixture(t)
	m := newModel(tuiConfig{muted: false})
	m.collectorDecided = true
	n := &fakeNotifier{}
	m.notifier = n
	m.report = r
	m.rebuild()
	runActions(m.alertCmd())
	_, cmd := m.Update(stateMsg{state: s})
	runActions(cmd)
	if len(n.sounds) != 0 {
		t.Fatal("initial state asks fired", n)
	}
}
