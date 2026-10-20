package main

import (
	"context"
	"fmt"
	"github.com/muesli/termenv"
	"io"
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
	m.collectorMode = uiCollectorExternal
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
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
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
	if m.pane != "log" || m.focus != "tree" {
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
		for _, want := range []string{"bus-status", uiCollector, "↑↓", "←→", "l ", "L ", "f ", "s ", "q "} {
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
	if !strings.Contains(v, "history") || !strings.Contains(v, uiHistory+"developer") {
		t.Fatal(v)
	}
	press(m, "l")
	m.Update(logMsg{gen: m.generation, lines: []LogLine{{Kind: "TEXT", Text: "log\x1b\u202e\u200b"}}})
	v = stripANSI(m.View())
	if !strings.Contains(v, uiLog+"developer") || !strings.Contains(v, "TEXT     log") {
		t.Fatal(v)
	}
	for _, r := range v {
		if r == '\u202e' || r == '\u200b' || r == '\x1b' {
			t.Fatal("unsafe output", v)
		}
	}
	press(m, "down")
	if len(m.logLines) != 0 || strings.Contains(stripANSI(m.View()), "TEXT     log") {
		t.Fatal("log retained on selection change")
	}
	// Unique empty peers remain visible; f reveals hidden duplicates.
	r.Local.Sessions = append(r.Local.Sessions, Row{SessionID: "empty", Name: "empty-peer", Product: "codex-peer"})
	m.Update(rosterMsg{report: r})
	press(m, "f")
	v = stripANSI(m.View())
	if !strings.Contains(v, "empty-peer") || !strings.Contains(v, "openclaw-peer") || !strings.Contains(v, uiShowAll) {
		t.Fatal(v)
	}
	// Idle rows retain normal color (U-17).
	if strings.Contains(m.View(), "\x1b[2m") {
		t.Fatal("idle row dimmed", m.View())
	}
	press(m, "L")
	v = stripANSI(m.View())
	if !strings.Contains(v, strings.TrimSpace(uiLog)) || !strings.Contains(v, "q "+uiHelpQuit) {
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
	cmd := press(m, "l")
	if calls != 0 || !m.inFlight || cmd == nil {
		t.Fatal("I/O ran in Update")
	}
	again := m.requestLog()
	if again != nil {
		t.Fatal("overlapping read")
	}
	msg := cmd()
	if calls != 1 {
		t.Fatal(calls)
	}
	oldGen := m.generation
	cmd = press(m, "down")
	if m.generation == oldGen {
		t.Fatal("no generation change")
	}
	m.Update(msg)
	if len(m.logLines) != 0 || !m.inFlight {
		t.Fatal("stale result applied")
	}
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
			m.startCollector = func(got context.Context, p, g string, _ io.Writer) error {
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
			want := uiCollectorOwn
			if external {
				want = uiCollectorExternal
			}
			if m.collectorMode != want || called != map[bool]int{false: 1, true: 0}[external] || !strings.Contains(stripANSI(m.View()), uiCollector+want) {
				t.Fatal(m.collectorMode, called, m.View())
			}
			m.Update(rosterMsg{report: r})
			if called > 1 {
				t.Fatal("collector restarted")
			}
			if external {
				r.Local.Sessions = nil
				m.Update(rosterMsg{report: r})
				if !strings.Contains(stripANSI(m.View()), uiCollector+uiCollectorMissing) {
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
	for _, want := range []string{"bus-status", uiCollector + uiCollectorExternal, "🔇", uiShowAll, "⚠ 1"} {
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
	cmd := press(m, "l")
	m.Update(cmd())
	if m.logErr != "" || len(m.logLines) != 5 {
		t.Fatal(m.logErr, m.logLines)
	}
	r := Report{Local: Host{Host: "local-alias"}, Remote: []Host{{Host: "remote", Sessions: []Row{{SessionID: "abc@remote", Product: "claude-peer"}}}}}
	_, cmd = m.Update(rosterMsg{report: r})
	applyLogCommand(m, cmd)
	if !strings.Contains(m.logErr, "remote-host") || len(m.logLines) != 0 {
		t.Fatal(m.logErr, m.logLines)
	}
}

func TestPeerActivityRefresh(t *testing.T) {
	m, r, s, now := testModel(t)
	s.Sessions["master@macbook"] = Record{Status: "old", UpdatedAt: now.Add(-time.Hour)}
	m.Update(stateMsg{state: s})
	v := stripANSI(m.View())
	if !strings.Contains(v, "◐ ▾ openclaw-peer") || !strings.Contains(v, uiLanesLegend) {
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
	if !strings.Contains(stripANSI(m.View()), "● ▾ openclaw-peer") {
		t.Fatal(m.View())
	}
	// Missing/old logs fall back to running lanes; no real homes are inspected.
	if err := os.Chtimes(p, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	msg = m.pollState()().(stateMsg)
	msg.state = s
	m.Update(msg)
	if !strings.Contains(stripANSI(m.View()), "◐ ▾ openclaw-peer") {
		t.Fatal(m.View())
	}
	for i := range r.Local.Sessions {
		r.Local.Sessions[i].Running = false
	}
	m.Update(rosterMsg{report: r})
	if !strings.Contains(stripANSI(m.View()), "○ ▾ openclaw-peer") {
		t.Fatal(m.View())
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if !strings.Contains(stripANSI(m.View()), uiLanesLegend) {
		t.Fatal(m.View())
	}
}

func TestInitialWindowSize(t *testing.T) {
	m := newModel(tuiConfig{muted: true})
	if got := m.View(); got != "" {
		t.Fatalf("frame before size: %q", got)
	}
	if m.collectorMode != uiCollectorChecking {
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
	m.startCollector = func(context.Context, string, string, io.Writer) error {
		calls++
		return fmt.Errorf("offline\x1b\u202e%s", strings.Repeat("x", 250))
	}
	cmd := m.collectorDecision()
	m.Update(cmd())
	if m.collectorDecided || m.ownCollector || !strings.Contains(stripANSI(m.View()), uiCollectorError+"offline") {
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
	if m.collectorMode != uiCollectorExternal || m.collectorErr != "" {
		t.Fatal(m.collectorMode, m.collectorErr)
	}
}

func TestCollectorShutdownWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	m := newModel(tuiConfig{ctx: ctx, cancel: cancel, collectorWG: wg})
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.startCollector = func(ctx context.Context, _, _ string, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		<-release
		return nil
	}
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
	m.report.Local.Error = "offline"
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

func TestDSHReadInterval(t *testing.T) {
	m, _, _, now := testModel(t)
	m.pane = "log"
	m.reader = &LogReader{kind: "dsh"}
	m.readLog = func(r *LogReader, _ visibleNode) (*LogReader, []LogLine, error) { return r, nil, nil }
	cmd := m.requestLog()
	m.Update(cmd())
	now = now.Add(2 * time.Second)
	m.now = func() time.Time { return now }
	if cmd = m.requestLog(); cmd != nil {
		t.Fatal("DSH read too frequent")
	}
	now = now.Add(time.Second)
	if cmd = m.requestLog(); cmd == nil {
		t.Fatal("DSH read not scheduled")
	}
}
func TestLogMovedToArchive(t *testing.T) {
	home := t.TempDir()
	m := newModel(tuiConfig{home: home})
	p := filepath.Join(home, ".codex/sessions/2026/10/20/rollout-t-id.jsonl")
	data, _ := os.ReadFile("testdata/logs/codex.jsonl")
	putLog(t, p, data)
	row := visibleNode{node: &Node{ID: "id@local", Harness: "codex"}, local: true}
	r, _, err := m.readLog(nil, row)
	if err != nil {
		t.Fatal(err)
	}
	archived := filepath.Join(home, ".codex/archived_sessions/rollout-t-id.jsonl")
	putLog(t, archived, data)
	os.Remove(p)
	r, _, err = m.readLog(r, row)
	if !os.IsNotExist(err) || r != nil {
		t.Fatal("reader retained", r, err)
	}
	r, lines, err := m.readLog(r, row)
	if err != nil || len(lines) != 5 || r.path != archived {
		t.Fatal(r, lines, err)
	}
}

func TestStateGuardAndImmediateLog(t *testing.T) {
	m, _, _, _ := testModel(t)
	cmd := m.pollState()
	if cmd == nil || m.pollState() != nil {
		t.Fatal("overlapping state poll")
	}
	m.Update(cmd())
	if m.pollState() == nil {
		t.Fatal("state guard not cleared")
	}
	m.readLog = func(*LogReader, visibleNode) (*LogReader, []LogLine, error) { return &LogReader{}, nil, nil }
	cmd = press(m, "l")
	if cmd == nil || !m.inFlight {
		t.Fatal("log read waits for timer")
	}
	m.Update(cmd())
	cmd = press(m, "down")
	if cmd == nil || !m.inFlight {
		t.Fatal("selection read waits for timer")
	}
	m.Update(cmd())
}

func applyLogCommand(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			applyLogCommand(m, c)
		}
	case logMsg:
		m.Update(msg)
	}
}

func TestTableViewGolden(t *testing.T) {
	for _, width := range []int{100, 60} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _, _, now := testModel(t)
			r := Report{Complete: true, Local: Host{Host: "local", Sessions: []Row{
				{SessionID: "parent", Name: "project", Kind: "peer", Product: "claude-peer"},
				{SessionID: "child", Name: "project/developer", Kind: "lane", Owner: "parent", Product: "sessionbus-dsh", Running: true},
			}}}
			s := State{Sessions: map[string]Record{"parent": {Status: strings.Repeat("busy ", 20), Model: "claude-opus-5-5", UpdatedAt: now.Add(-time.Minute)}, "child": {Status: strings.Repeat("tests ", 20), Model: "provider/glm-5.3", UpdatedAt: now.Add(-5 * time.Second)}}}
			m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
			m.Update(stateMsg{state: s})
			m.Update(rosterMsg{report: r})
			got := stripANSI(m.View()) + "\n"
			for _, want := range []string{uiSession, uiStatus, uiModel, uiHarness, "developer", "opus-5-5", "glm-5.3", "…"} {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q:\n%s", want, got)
				}
			}
			for _, bad := range []string{"project/developer", "claude-opus", "provider/"} {
				if strings.Contains(got, bad) {
					t.Fatalf("unshortened %q", bad)
				}
			}
			p := fmt.Sprintf("testdata/table-%d.golden", width)
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(p, []byte(got), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch:\n%s", got)
			}
		})
	}
}

func TestDuplicatePeerToggle(t *testing.T) {
	m, _, _, now := testModel(t)
	r := Report{Local: Host{Host: "local", Sessions: []Row{{SessionID: "old", Name: "same", Kind: "peer"}, {SessionID: "new", Name: "same", Kind: "peer"}, {SessionID: "empty", Name: "empty", Kind: "peer"}}}}
	s := State{Sessions: map[string]Record{"old": {Status: "old status", UpdatedAt: now.Add(-time.Hour)}, "new": {Status: "current", UpdatedAt: now}}}
	m.Update(stateMsg{state: s})
	m.Update(rosterMsg{report: r})
	v := stripANSI(m.View())
	if !strings.Contains(v, fmt.Sprintf(uiHidden, 1)) || strings.Contains(v, "same · old") || !strings.Contains(v, "empty") {
		t.Fatal(v)
	}
	press(m, "f")
	v = stripANSI(m.View())
	if !strings.Contains(v, "same · old") || strings.Contains(v, fmt.Sprintf(uiHidden, 1)) {
		t.Fatal(v)
	}
	press(m, "f")
	if strings.Contains(stripANSI(m.View()), "same · old") {
		t.Fatal(m.View())
	}
}

func TestLogPaneWrapAndColors(t *testing.T) {
	m, _, _, now := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	press(m, "l")
	text := strings.Repeat("界🙂", 150) + "\nSECOND\x1b\u202e"
	m.Update(logMsg{gen: m.generation, lines: []LogLine{{At: now, Kind: "TEXT", Text: text}, {At: now, Kind: "TOOL", Tool: "Bash\x1b", Text: "echo safe"}, {Kind: "RESULT", Text: "result"}, {Kind: "THINK", Text: "thinking"}, {Kind: "SYS", Text: "system"}}})
	plain := m.renderLogs(58)
	if !strings.HasPrefix(plain, now.Format("15:04:05")+"  TEXT     ") {
		t.Fatal("log columns", plain)
	}
	if strings.ContainsAny(plain, "\x1b\u202e") || !strings.Contains(plain, "SECOND") || !strings.Contains(plain, "Bash") {
		t.Fatal(plain)
	}
	var recovered strings.Builder
	for i, line := range strings.Split(plain, "\n") {
		if lipgloss.Width(line) > 58 {
			t.Fatal("wrap overflow", line)
		}
		if i > 0 && strings.Contains(line, "界") && !strings.HasPrefix(line, strings.Repeat(" ", 19)) {
			t.Fatal("missing hanging indent", line)
		}
		for _, r := range line {
			if r == '界' || r == '🙂' {
				recovered.WriteRune(r)
			}
		}
	}
	if recovered.String() != strings.Repeat("界🙂", 150) {
		t.Fatal("wrapped text lost")
	}
	m.config.color = true
	m.logRenderer = lipgloss.NewRenderer(io.Discard)
	m.logRenderer.SetColorProfile(termenv.ANSI)
	colored := m.renderLogs(58)
	for _, code := range []string{"[32m", "[36m", "[2m", "[33m", "[35m", "[1;36m"} {
		if !strings.Contains(colored, code) {
			t.Fatalf("missing style %s: %q", code, colored)
		}
	}
	if stripANSI(colored) != plain {
		t.Fatal("color changed text")
	}
	m.config.color = false
	if got := m.renderLogs(58); got != plain || strings.Contains(got, "\x1b") {
		t.Fatal("color leaked while disabled", got)
	}
}

func TestLogFocusScrollAndFollow(t *testing.T) {
	m, _, _, _ := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	press(m, "l")
	lines := make([]LogLine, 40)
	for i := range lines {
		lines[i] = LogLine{Kind: "TEXT", Text: fmt.Sprintf("record %02d", i)}
	}
	m.Update(logMsg{gen: m.generation, lines: lines})
	if !m.viewport.AtBottom() {
		t.Fatal("initial tail")
	}
	press(m, "tab")
	if m.focus != "log" || !strings.Contains(stripANSI(m.View()), "focus: log") {
		t.Fatal(m.View())
	}
	selected := m.selected
	press(m, "g")
	if m.viewport.YOffset != 0 || m.selected != selected {
		t.Fatal("g did not scroll log")
	}
	press(m, "down")
	if m.viewport.YOffset != 1 {
		t.Fatal(m.viewport.YOffset)
	}
	press(m, "up")
	if m.viewport.YOffset != 0 {
		t.Fatal(m.viewport.YOffset)
	}
	press(m, "pgdown")
	if m.viewport.YOffset == 0 {
		t.Fatal("page down")
	}
	press(m, "pgup")
	if m.viewport.YOffset != 0 {
		t.Fatal("page up")
	}
	press(m, "g")
	offset := m.viewport.YOffset
	m.Update(logMsg{gen: m.generation, lines: []LogLine{{Kind: "TEXT", Text: "appended one"}, {Kind: "TEXT", Text: "appended two"}}})
	if m.viewport.YOffset != offset || m.newLogLines != 2 || !strings.Contains(stripANSI(m.View()), "▼ 2│") {
		t.Fatal(m.viewport.YOffset, m.newLogLines, m.View())
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 15})
	if m.viewport.YOffset < 0 || m.viewport.YOffset > m.viewport.TotalLineCount() {
		t.Fatal("bad resize offset")
	}
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatal("resized frame overflow", line)
		}
	}
	if len(strings.Split(m.View(), "\n")) > 15 {
		t.Fatal("resized frame too tall")
	}
	press(m, "G")
	if !m.viewport.AtBottom() || m.newLogLines != 0 || strings.Contains(stripANSI(m.View()), "▼ 2") {
		t.Fatal("tail not restored")
	}
	press(m, "L")
	if m.pane != "logFull" || m.focus != "log" {
		t.Fatal(m.pane, m.focus)
	}
	press(m, "esc")
	if m.pane != "log" || m.focus != "tree" {
		t.Fatal(m.pane, m.focus)
	}
	press(m, "tab")
	if !strings.Contains(stripANSI(m.View()), "PgUp/PgDn") {
		t.Fatal("log help missing")
	}
}

func TestEnglishUIAndIdleRows(t *testing.T) {
	m, r, s, now := testModel(t)
	r.Local.Sessions = append(r.Local.Sessions, Row{SessionID: "wait", Name: "waiting-lane", Kind: "lane", Product: "codex-peer"})
	s.Sessions["wait"] = Record{Status: "жду master", UpdatedAt: now.Add(-13 * time.Minute)}
	m.Update(rosterMsg{report: r})
	m.Update(stateMsg{state: s})
	for _, got := range []string{stripANSI(m.View()), Render(r, s, now, false)} {
		for _, want := range []string{uiSession, uiStatus, uiAge, uiModel, uiHarness, "idle", "жду master"} {
			if !strings.Contains(got, want) {
				t.Fatalf("missing %s: %s", want, got)
			}
		}
		for _, bad := range []string{"сборщик:", "ЖДЁТ:", "нет статуса", "недоступен", "история", "работают"} {
			if strings.Contains(got, bad) {
				t.Fatal("Russian UI label", bad, got)
			}
		}
	}
	if strings.Contains(m.View(), "\x1b[2m") || strings.Contains(Render(r, s, now, true), "\x1b[2m") {
		t.Fatal("idle rows dimmed")
	}
	n := &fakeNotifier{}
	m.notifier = n
	m.muted = false
	m.alerts = Alerts{}
	m.alerts.Tick(now, nil)
	runActions(m.alertCmd())
	if len(n.bodies) != 1 || !strings.Contains(n.bodies[0], " asks: ") {
		t.Fatal(n.bodies)
	}
}

func TestSiblingTreeConnectors(t *testing.T) {
	m, _, _, now := testModel(t)
	r := Report{Complete: true, Local: Host{Host: "local", Sessions: []Row{
		{SessionID: "p", Name: "peer", Kind: "peer"},
		{SessionID: "a", Name: "alpha", Owner: "p", Kind: "lane"},
		{SessionID: "b", Name: "beta", Owner: "p", Kind: "lane"},
		{SessionID: "c", Name: "gamma", Owner: "p", Kind: "lane"},
		{SessionID: "n", Name: "nested", Owner: "a", Kind: "lane"},
	}}}
	m.Update(rosterMsg{report: r})
	for _, got := range []string{stripANSI(m.View()), Render(r, State{}, now, false)} {
		for _, want := range []string{"├ ▾ alpha", "│ └ nested", "├ beta", "└ gamma"} {
			if !strings.Contains(got, want) {
				t.Fatalf("missing %q:\n%s", want, got)
			}
		}
	}
	press(m, "down")
	press(m, "left")
	v := stripANSI(m.View())
	if !strings.Contains(v, "├ ▸ alpha") || strings.Contains(v, "nested") || !strings.Contains(v, "├ beta") {
		t.Fatal(v)
	}
}

func TestOwnerHostPrefixShortening(t *testing.T) {
	m, _, _, now := testModel(t)
	r := Report{Complete: true, Local: Host{Host: "local", Sessions: []Row{
		{SessionID: "p", Name: "project@local", Kind: "peer"},
		{SessionID: "c", Name: "project/review@local", Kind: "lane", Owner: "p"},
		{SessionID: "n", Name: "project/review/nested", Kind: "lane", Owner: "c"},
	}}}
	m.Update(rosterMsg{report: r})
	for _, got := range []string{stripANSI(m.View()), Render(r, State{}, now, false)} {
		for _, want := range []string{"└ ▾ review@local", "└ nested"} {
			if !strings.Contains(got, want) {
				t.Fatal("owner prefix retained", want, got)
			}
		}
		if strings.Contains(got, "project/review") {
			t.Fatal("owner prefix retained in session column", got)
		}
	}
}

func TestIdleAgeColumn(t *testing.T) {
	for _, duration := range []time.Duration{59 * time.Minute, 23 * time.Hour} {
		m, _, _, now := testModel(t)
		r := Report{Complete: true, Local: Host{Host: "local", Sessions: []Row{{SessionID: "lane", Name: "lane", Kind: "lane", Product: "codex-peer"}}}}
		s := State{Sessions: map[string]Record{"lane": {Status: "status-value", UpdatedAt: now.Add(-duration)}}}
		m.Update(stateMsg{state: s})
		m.Update(rosterMsg{report: r})
		for _, got := range []string{stripANSI(m.View()), Render(r, s, now, false)} {
			if !strings.Contains(got, "idle "+age(duration)) || strings.Contains(got, "idle:") || !strings.Contains(got, "status-value") {
				t.Fatal(got)
			}
		}
		table := newTreeTable(100, []string{"lane"})
		n := BuildTree(r, s, now, nil)[0].Nodes[0]
		if got := table.row(n, "lane"); !strings.Contains(got, tableCell("status-value", table.status)+" "+tableCell("idle "+age(duration), 8)+" ") {
			t.Fatal("idle is not in age column", got)
		}
	}
}

func TestVisibleDuplicateSuffixes(t *testing.T) {
	m, _, _, now := testModel(t)
	r := Report{Complete: true, Local: Host{Host: "local", Sessions: []Row{{SessionID: "old-id", Name: "same", Kind: "peer"}, {SessionID: "new-id", Name: "same", Kind: "peer"}}}}
	s := State{Sessions: map[string]Record{"old-id": {Status: "old-status", UpdatedAt: now.Add(-time.Hour)}, "new-id": {Status: "new-status", UpdatedAt: now}}}
	m.Update(stateMsg{state: s})
	m.Update(rosterMsg{report: r})
	for _, got := range []string{stripANSI(m.View()), Render(r, s, now, false)} {
		if strings.Contains(got, "same ·") || !strings.Contains(got, "same") || strings.Contains(got, "old-status") {
			t.Fatal("hidden twin affects visible display", got)
		}
	}
	n := BuildTree(r, s, now, nil)[0].Nodes[0]
	if n.Display != "same" {
		t.Fatal("default tree suffix", n.Display)
	}
	press(m, "f")
	v := stripANSI(m.View())
	for _, want := range []string{"same · old-id", "same · new-id"} {
		if !strings.Contains(v, want) {
			t.Fatal("visible duplicate lacks suffix", v)
		}
	}
	press(m, "f")
	if strings.Contains(stripANSI(m.View()), "same ·") {
		t.Fatal("suffix retained", m.View())
	}
}

func TestCollectorDiagnosticsStayOffTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, _, _, _ := testModel(t)
	m.config.ctx = ctx
	m.collectorDecided = false
	m.ownCollector = false
	m.report.Local.Sessions = nil
	logPath := filepath.Join(filepath.Dir(m.config.statePath), "collector.log")
	putLog(t, logPath, []byte("previous\n"))
	started, done := make(chan struct{}), make(chan struct{})
	var diagnostics io.Writer
	m.startCollector = func(ctx context.Context, _, _ string, w io.Writer) error {
		diagnostics = w
		fmt.Fprintln(w, "bus-status: connected")
		close(started)
		<-ctx.Done()
		return nil
	}
	stdout, stderr := captureTerminal(t, func() {
		cmd := m.collectorDecision()
		go func() { cmd(); close(done) }()
		defer func() { cancel(); <-done }()
		<-started
		m.Update(m.pollCollectorDiagnostics()())
		if !strings.Contains(stripANSI(m.View()), uiCollector+uiCollectorOwn+" (connected)") || m.collectorLast != "connected" {
			t.Fatal(m.View())
		}
		// Exercise the actual state recovery and delivery failure paths with this writer.
		bad := filepath.Join(t.TempDir(), "bad-state")
		putLog(t, bad, []byte("invalid"))
		loadCollectorState(bad, diagnostics)
		blocked := filepath.Join(t.TempDir(), "file")
		putLog(t, blocked, nil)
		c := &collector{path: filepath.Join(blocked, "state.json"), diagnostics: diagnostics}
		delivery(t, c, "sender", "status: working")
		fmt.Fprintln(diagnostics, "bus-status: save: failed\x1b\u202e")
		m.Update(m.pollCollectorDiagnostics()())
		if m.collectorLast != "save: failed" || !strings.Contains(stripANSI(m.View()), uiCollectorError+"save: failed") {
			t.Fatal(m.View())
		}
		if strings.ContainsAny(m.collectorLast+m.collectorErr, "\x1b\u202e") {
			t.Fatal("unsafe diagnostic")
		}
	})
	if stdout != "" || stderr != "" {
		t.Fatalf("stray terminal output: stdout=%q stderr=%q", stdout, stderr)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"previous\n", "bus-status: connected", "warning: cannot load state", "bus-status: save:"} {
		if !strings.Contains(string(data), want) {
			t.Fatal("missing log entry", want, string(data))
		}
	}
}

func captureTerminal(t *testing.T, fn func()) (string, string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; outW.Close(); errW.Close(); outR.Close(); errR.Close() }()
	fn()
	outW.Close()
	errW.Close()
	out, _ := io.ReadAll(outR)
	stderr, _ := io.ReadAll(errR)
	return string(out), string(stderr)
}

func TestRosterEligibleAlertsAndCount(t *testing.T) {
	m, _, _, now := testModel(t)
	n := &fakeNotifier{}
	m.notifier = n
	m.muted = false
	m.alerts = Alerts{}
	m.alerts.Tick(now, nil)
	m.report = Report{Complete: false, Local: Host{Host: "local", Sessions: []Row{{SessionID: "active@local", Name: "active", Kind: "peer"}}}, Remote: []Host{{Host: "office", Error: "offline"}}}
	m.state = State{Sessions: map[string]Record{"active@local": {Ask: "active question"}, "closed@local": {Ask: "ghost question"}, "remote@office": {Ask: "remote question"}}}
	m.rebuild()
	runActions(m.alertCmd())
	if len(n.bodies) != 2 || len(askIDs(m.report, m.state)) != 2 {
		t.Fatal("header/alerts disagree", n.bodies, askIDs(m.report, m.state))
	}
	for _, body := range n.bodies {
		if strings.Contains(body, "ghost") {
			t.Fatal("closed session alerted", body)
		}
	}
	for _, view := range []string{stripANSI(m.View()), Render(m.report, m.state, now, false)} {
		if !strings.Contains(view, fmt.Sprintf(uiWaitingCount, 2)) {
			t.Fatal("wrong header count", view)
		}
	}
	// A healthy roster that no longer lists any sessions must not ring for leftover asks.
	m.report = Report{Local: Host{Host: "local"}}
	m.rebuild()
	runActions(m.alertCmd())
	now = now.Add(time.Minute)
	m.now = func() time.Time { return now }
	runActions(m.alertCmd())
	if len(n.bodies) != 2 || len(askIDs(m.report, m.state)) != 0 {
		t.Fatal("ghost reminders", n.bodies)
	}
}

func TestFullLogTabDoesNotHideTreeFocus(t *testing.T) {
	m, _, _, _ := testModel(t)
	press(m, "L")
	press(m, "tab")
	if m.pane == "logFull" && m.focus == "tree" {
		t.Fatal("focused invisible tree")
	}
	if m.focus == "tree" && !strings.Contains(stripANSI(m.View()), uiSession) {
		t.Fatal("tree focus without tree", m.View())
	}
}

func TestLogCapKeepsWrappedScrollAnchor(t *testing.T) {
	m, _, _, _ := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	press(m, "l")
	press(m, "tab")
	lines := make([]LogLine, 200)
	for i := range lines {
		lines[i] = LogLine{Kind: "TEXT", Text: fmt.Sprintf("record %03d", i)}
	}
	lines[0].Text = strings.Repeat("界", 60)
	m.Update(logMsg{gen: m.generation, lines: lines})
	press(m, "g")
	for i := 0; i < 10; i++ {
		press(m, "down")
	}
	anchor := strings.Split(stripANSI(m.viewport.View()), "\n")[0]
	m.Update(logMsg{gen: m.generation, lines: []LogLine{{Kind: "TEXT", Text: "new one"}, {Kind: "TEXT", Text: "new two"}}})
	if got := strings.Split(stripANSI(m.viewport.View()), "\n")[0]; got != anchor {
		t.Fatalf("anchor moved: %q → %q", anchor, got)
	}
	if m.newLogLines != 2 || m.followTail {
		t.Fatal(m.newLogLines, m.followTail)
	}
}

func TestResizeBottomResumesTail(t *testing.T) {
	m, _, _, _ := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	press(m, "L")
	lines := make([]LogLine, 40)
	for i := range lines {
		lines[i] = LogLine{Kind: "TEXT", Text: fmt.Sprint(i)}
	}
	m.Update(logMsg{gen: m.generation, lines: lines})
	press(m, "g")
	for i := 0; i < 20; i++ {
		press(m, "down")
	}
	if m.followTail {
		t.Fatal("test must start scrolled up")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 35})
	if !m.viewport.AtBottom() || !m.followTail {
		t.Fatal("resize pinned bottom without following", m.viewport.YOffset, m.viewport.Height)
	}
	m.Update(logMsg{gen: m.generation, lines: []LogLine{{Kind: "TEXT", Text: "new tail"}}})
	if !m.viewport.AtBottom() || m.newLogLines != 0 {
		t.Fatal("tail did not follow after resize")
	}
}
