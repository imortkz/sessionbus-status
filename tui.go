package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tuiConfig struct {
	collectorWG                    *sync.WaitGroup
	ctx                            context.Context
	cancel                         context.CancelFunc
	statePath, home, groups, sound string
	command                        []string
	interval, timeout              time.Duration
	muted                          bool
}
type rosterMsg struct {
	report Report
	err    error
}
type stateMsg struct {
	activity map[string]time.Time
	state    State
	err      error
}
type rosterTick struct{}
type clockTick struct{}
type logTick struct{}
type collectorMsg struct{ err error }
type logMsg struct {
	gen    uint64
	reader *LogReader
	lines  []LogLine
	err    error
}
type visibleNode struct {
	node  *Node
	host  string
	local bool
	depth int
}
type logReadFunc func(*LogReader, visibleNode) (*LogReader, []LogLine, error)
type Model struct {
	config                         tuiConfig
	report                         Report
	state                          State
	tree                           []HostTree
	activity                       map[string]time.Time
	stateLoaded                    bool
	selected                       string
	collapsed                      map[string]bool
	pane                           string
	filter, muted                  bool
	collectorMode                  string
	collectorErr                   string
	collectorRetryAt               time.Time
	collectorDecided, ownCollector bool
	width, height                  int
	sized                          bool
	viewport                       viewport.Model
	help                           help.Model
	keys                           keyMap
	generation                     uint64
	inFlight                       bool
	reader                         *LogReader
	logLines                       []LogLine
	logErr, rosterErr, stateErr    string
	notifier                       Notifier
	alerts                         Alerts
	now                            func() time.Time
	startCollector                 func(context.Context, string, string) error
	readLog                        logReadFunc
}
type keyMap []key.Binding

func (k keyMap) ShortHelp() []key.Binding  { return k }
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k} }
func newModel(c tuiConfig) *Model {
	if c.ctx == nil {
		c.ctx = context.Background()
	}
	if c.interval <= 0 {
		c.interval = 2 * time.Second
	}
	if c.timeout <= 0 {
		c.timeout = 10 * time.Second
	}
	m := &Model{config: c, collapsed: map[string]bool{}, pane: "history", muted: c.muted, collectorMode: "проверяю…", width: 100, height: 30, viewport: viewport.New(98, 8), help: help.New(), now: time.Now, notifier: macNotifier{}, startCollector: startCollector}
	m.keys = keyMap{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "выбор")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", "свернуть")),
		key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "лог/история")),
		key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "лог во весь экран")),
		key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "фильтр")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "звук вкл/выкл")),
		key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "выход")),
	}
	m.readLog = func(r *LogReader, v visibleNode) (*LogReader, []LogLine, error) {
		if !v.local {
			return nil, nil, fmt.Errorf("remote-host: локальный лог недоступен")
		}
		if r == nil {
			id, _, _ := strings.Cut(v.node.ID, "@")
			p, k, err := LocateLog(c.home, id, v.node.Harness)
			if err != nil {
				return nil, nil, err
			}
			r = &LogReader{path: p, kind: k}
		}
		lines, err := r.Read()
		return r, lines, err
	}
	return m
}
func (m *Model) Init() tea.Cmd { return tea.Batch(m.pollRoster(), m.pollState(), secondTick()) }
func secondTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return clockTick{} })
}
func (m *Model) pollRoster() tea.Cmd {
	c := m.config
	return func() tea.Msg {
		out, stderr, err := runRoster(c.ctx, c.command, c.timeout)
		r, decodeErr := decodeReport(out)
		if decodeErr != nil {
			message := strings.TrimSpace(string(stderr))
			if message != "" {
				err = fmt.Errorf("%s", message)
			} else if err == nil {
				err = decodeErr
			}
			return rosterMsg{err: err}
		}
		return rosterMsg{report: r}
	}
}
func (m *Model) pollState() tea.Cmd {
	p, home := m.config.statePath, m.config.home
	rows := append([]Row(nil), m.report.Local.Sessions...)
	now := m.now
	return func() tea.Msg {
		s, err := loadState(p)
		activity := map[string]time.Time{}
		at := now()
		for _, row := range rows {
			if row.Kind != "peer" || row.Product == "bus-status" {
				continue
			}
			id, _, _ := strings.Cut(row.SessionID, "@")
			if stamp, statErr := LogActive(home, id, row.Product, at, 30*time.Second); statErr == nil && !stamp.IsZero() {
				activity[row.SessionID] = stamp
			}
		}
		return stateMsg{state: s, err: err, activity: activity}
	}
}
func (m *Model) visible() []visibleNode {
	var rows []visibleNode
	var walk func(*Node, HostTree, int)
	walk = func(n *Node, h HostTree, depth int) {
		if m.filter && n.Line == "нет статуса" && len(n.Children) == 0 {
			return
		}
		rows = append(rows, visibleNode{n, h.Host, h.Local, depth})
		if !m.collapsed[n.ID] {
			for _, c := range n.Children {
				walk(c, h, depth+1)
			}
		}
	}
	for _, h := range m.tree {
		for _, n := range h.Nodes {
			walk(n, h, 0)
		}
	}
	return rows
}
func (m *Model) selectID(id string) {
	if id == m.selected {
		return
	}
	m.selected = id
	m.generation++
	m.reader = nil
	m.inFlight = false
	m.logLines = nil
	m.logErr = ""
	m.viewport.SetContent("")
}
func (m *Model) rebuild() {
	m.tree = BuildTree(m.report, m.state, m.now(), m.activity)
	rows := m.visible()
	for _, v := range rows {
		if v.node.ID == m.selected {
			return
		}
	}
	if len(rows) > 0 {
		m.selectID(rows[0].node.ID)
	} else {
		m.selectID("")
	}
}
func (m *Model) selectedRow() (visibleNode, bool) {
	for _, v := range m.visible() {
		if v.node.ID == m.selected {
			return v, true
		}
	}
	return visibleNode{}, false
}
func (m *Model) alertCmd() tea.Cmd {
	if !m.stateLoaded {
		return nil
	}
	var asks []AskInfo
	displays := map[string]string{}
	collectors := map[string]bool{}
	var walk func(*Node)
	walk = func(n *Node) {
		displays[n.ID] = n.Display
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, h := range m.tree {
		for _, n := range h.Nodes {
			walk(n)
		}
	}
	for _, h := range append([]Host{m.report.Local}, m.report.Remote...) {
		for _, r := range h.Sessions {
			if r.Product == "bus-status" {
				collectors[r.SessionID] = true
			}
		}
	}
	for id, rec := range m.state.Sessions {
		if cleanText(rec.Ask, 120) == "" || rec.Product == "bus-status" || collectors[id] {
			continue
		}
		display := displays[id]
		if display == "" {
			display = cleanText(id, 120)
		}
		since := rec.UpdatedAt
		for i := len(rec.History) - 1; i >= 0; i-- {
			if rec.History[i].Kind == "ask" {
				since = rec.History[i].At
				break
			}
		}
		asks = append(asks, AskInfo{id, display, rec.Ask, since})
	}
	m.alerts.muted = m.muted
	actions := m.alerts.Tick(m.now(), asks)
	if len(actions) == 0 {
		return nil
	}
	n, sound := m.notifier, m.config.sound
	return func() tea.Msg {
		for _, a := range actions {
			switch a.Kind {
			case "sound":
				n.Sound(sound)
			case "notify":
				n.Notify(a.Title, a.Body)
			}
		}
		return nil
	}
}
func (m *Model) requestLog() tea.Cmd {
	if m.pane == "history" || m.inFlight {
		return nil
	}
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	gen, r, read := m.generation, m.reader, m.readLog
	m.inFlight = true
	return func() tea.Msg { reader, lines, err := read(r, row); return logMsg{gen, reader, lines, err} }
}
func (m *Model) collectorDecision() tea.Cmd {
	external := false
	for _, r := range m.report.Local.Sessions {
		if r.Product == "bus-status" && r.Connected {
			external = true
			break
		}
	}
	if m.collectorDecided {
		if !m.ownCollector {
			m.collectorMode = "не найден"
			if external {
				m.collectorMode = "внешний"
			}
		}
		return nil
	}
	if !external && m.now().Before(m.collectorRetryAt) {
		return nil
	}
	m.collectorDecided = true
	m.collectorErr = ""
	if external {
		m.collectorMode = "внешний"
		return nil
	}
	m.collectorMode = "свой"
	m.ownCollector = true
	start, c := m.startCollector, m.config
	if c.collectorWG != nil {
		c.collectorWG.Add(1)
	}
	return func() tea.Msg {
		if c.collectorWG != nil {
			defer c.collectorWG.Done()
		}
		return collectorMsg{start(c.ctx, c.statePath, c.groups)}
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.sized = true
		m.width = max(4, msg.Width)
		m.height = max(4, msg.Height)
	case rosterTick:
		return m, m.pollRoster()
	case clockTick:
		return m, tea.Batch(m.pollState(), m.requestLog(), secondTick())
	case logTick:
		return m, m.requestLog()
	case rosterMsg:
		delay := tea.Tick(m.config.interval, func(time.Time) tea.Msg { return rosterTick{} })
		if msg.err != nil {
			m.rosterErr = cleanText(msg.err.Error(), 200)
			return m, delay
		}
		m.report = msg.report
		m.rosterErr = ""
		m.rebuild()
		return m, tea.Batch(m.collectorDecision(), m.alertCmd(), delay)
	case stateMsg:
		m.activity = msg.activity
		if msg.err != nil {
			m.stateErr = cleanText(msg.err.Error(), 200)
			m.rebuild()
			return m, nil
		}
		m.state = msg.state
		m.stateLoaded = true
		m.stateErr = ""
		m.rebuild()
		return m, m.alertCmd()
	case collectorMsg:
		if m.config.ctx.Err() == nil {
			m.collectorMode = "не найден"
			m.ownCollector = false
			m.collectorDecided = false
			m.collectorRetryAt = m.now().Add(10 * time.Second)
			if msg.err != nil {
				m.collectorErr = cleanText(msg.err.Error(), 200)
			} else {
				m.collectorErr = "сборщик остановлен"
			}
		}
	case logMsg:
		if msg.gen != m.generation {
			return m, nil
		}
		m.inFlight = false
		m.reader = msg.reader
		if msg.err != nil {
			m.logErr = cleanText(msg.err.Error(), 200)
		} else {
			m.logErr = ""
			m.logLines = append(m.logLines, msg.lines...)
			if len(m.logLines) > 200 {
				m.logLines = m.logLines[len(m.logLines)-200:]
			}
		}
	case tea.KeyMsg:
		rows := m.visible()
		idx := 0
		for i, v := range rows {
			if v.node.ID == m.selected {
				idx = i
				break
			}
		}
		switch msg.String() {
		case "q", "ctrl+c":
			if m.config.cancel != nil {
				m.config.cancel()
			}
			return m, tea.Quit
		case "down":
			if idx+1 < len(rows) {
				m.selectID(rows[idx+1].node.ID)
			}
		case "up":
			if idx > 0 {
				m.selectID(rows[idx-1].node.ID)
			}
		case "left":
			m.collapsed[m.selected] = true
			m.rebuild()
		case "right":
			delete(m.collapsed, m.selected)
			m.rebuild()
		case "l":
			if m.pane == "history" {
				m.pane = "log"
			} else {
				m.pane = "history"
			}
		case "L":
			m.pane = "logFull"
		case "esc":
			m.pane = "history"
		case "f":
			m.filter = !m.filter
			m.rebuild()
		case "s":
			m.muted = !m.muted
			m.alerts.muted = m.muted
		}
	}
	return m, nil
}
func (m *Model) helpLines(width int) []string {
	// Wrap bindings as complete units so every shortcut remains present at 60 columns.
	h := m.help
	h.Width = 0
	var lines []string
	line := ""
	var pieces []string
	for _, b := range m.keys {
		pieces = append(pieces, h.ShortHelpView([]key.Binding{b}))
	}
	pieces = append(pieces, "◐ работают lanes")
	for _, piece := range pieces {
		if line != "" && lipgloss.Width(line+"  "+piece) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "  "
		}
		line += piece
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
func (m *Model) paneContent() string {
	if m.pane != "history" {
		if m.logErr != "" {
			return m.logErr
		}
		if len(m.logLines) == 0 {
			return "ожидание лога…"
		}
		var lines []string
		for _, l := range m.logLines {
			stamp := ""
			if !l.At.IsZero() {
				stamp = l.At.Format("15:04:05") + " "
			}
			lines = append(lines, stamp+cleanText(l.Kind, 20)+"  "+cleanText(l.Text, logLimit(l.Kind)))
		}
		return strings.Join(lines, "\n")
	}
	rec := m.state.Sessions[m.selected]
	var lines []string
	for _, e := range rec.History {
		lines = append(lines, e.At.Format("15:04:05")+" "+cleanText(e.Kind, 20)+"  "+cleanText(e.Text, 120))
	}
	if len(lines) == 0 {
		return "история пуста"
	}
	return strings.Join(lines, "\n")
}
func logLimit(kind string) int {
	switch kind {
	case "TEXT":
		return 300
	case "TOOL":
		return 120
	case "RESULT":
		return 140
	default:
		return 200
	}
}
func (m *Model) View() string {
	if !m.sized {
		return ""
	}
	width := max(2, m.width-2)
	height := max(2, m.height-2)
	helpLines := m.helpLines(width)
	icon := "🔈"
	if m.muted {
		icon = "🔇"
	}
	collector := "сборщик: " + m.collectorMode
	if m.collectorMode == "не найден" {
		collector = "\x1b[31m" + collector + "\x1b[0m"
	}
	badges := ""
	if m.filter {
		badges += " · фильтр"
	}
	count := len(askIDs(m.report, m.state))
	if count > 0 {
		badges += fmt.Sprintf(" · ⚠ %d ждёт тебя", count)
	}
	core := "bus-status · " + collector + " · " + icon
	head := "bus-status · " + cleanText(m.report.Local.Host, 120) + " · " + m.now().Format("15:04:05") + " · " + collector + " · " + icon + badges
	if lipgloss.Width(head) > width {
		head = core + badges
	}
	if lipgloss.Width(head) > width && count > 0 {
		head = strings.Replace(head, " ждёт тебя", "", 1)
	}
	bodyHeight := max(1, height-len(helpLines)-1)
	var body []string
	if m.rosterErr != "" {
		body = append(body, "Ошибка roster: "+m.rosterErr)
	}
	if m.stateErr != "" {
		body = append(body, "Ошибка state: "+m.stateErr)
	}
	if m.collectorErr != "" {
		body = append(body, "Ошибка сборщика: "+m.collectorErr)
	}
	available := max(1, bodyHeight-len(body))
	paneHeight := min(available, max(5, bodyHeight*40/100))
	if m.pane == "logFull" {
		paneHeight = available
	}
	treeHeight := available - paneHeight
	if treeHeight > 0 {
		var lines []string
		selectedIndex := 0
		rows := m.visible()
		byHost := map[string][]visibleNode{}
		for _, v := range rows {
			byHost[v.host] = append(byHost[v.host], v)
		}
		for _, h := range m.tree {
			if !h.Local || h.Error != "" {
				line := "── " + h.Host
				if h.Error != "" {
					line += ": недоступен (" + h.Error + ")"
				}
				lines = append(lines, line)
			}
			for _, v := range byHost[h.Host] {
				n := v.node
				mark := n.Icon()
				branch := "  "
				if len(n.Children) > 0 {
					branch = "▾ "
					if m.collapsed[n.ID] {
						branch = "▸ "
					}
				} else if v.depth > 0 {
					branch = "└ "
				}
				harness := n.Harness
				if n.Model != "" {
					harness += " · " + n.Model
				}
				line := strings.Repeat("  ", v.depth) + branch + mark + " " + n.Display + "    " + harness + "    " + n.Line + "  " + n.Age
				line = clipLine(line, width)
				if n.Ask {
					line = "\x1b[31m" + line + "\x1b[0m"
				} else if n.Stale {
					line = "\x1b[2m" + line + "\x1b[0m"
				}
				if n.ID == m.selected {
					selectedIndex = len(lines)
					line = "\x1b[7m" + line + "\x1b[0m"
				}
				lines = append(lines, line)
			}
		}
		start := max(0, selectedIndex-treeHeight+1)
		for i := 0; i < treeHeight; i++ {
			line := ""
			if start+i < len(lines) {
				line = lines[start+i]
			}
			body = append(body, line)
		}
	}
	title := "история: "
	if m.pane != "history" {
		title = "лог: "
	}
	if v, ok := m.selectedRow(); ok {
		title += v.node.Display
	}
	if m.pane != "history" {
		title += " (live)"
	}
	body = append(body, "── "+title+" "+strings.Repeat("─", width))
	vp := m.viewport
	vp.Width = width
	vp.Height = max(0, paneHeight-1)
	vp.SetContent(m.paneContent())
	vp.GotoBottom()
	if vp.Height > 0 {
		body = append(body, strings.Split(vp.View(), "\n")...)
	}
	lines := append([]string{head}, body...)
	lines = append(lines, helpLines...)
	for i := range lines {
		lines[i] = clipLine(lines[i], width)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(width).Render(strings.Join(lines, "\n"))
}

func clipLine(s string, width int) string { return lipgloss.NewStyle().MaxWidth(width).Render(s) }
