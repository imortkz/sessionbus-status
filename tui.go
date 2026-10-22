package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	command, ignore                []string
	interval, timeout              time.Duration
	muted                          bool
	color                          bool
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
	firstSeen                      map[string]time.Time
	stateLoaded                    bool
	stateInFlight                  bool
	selected                       string
	collapsed                      map[string]bool
	pane                           string
	focus                          string
	followTail                     bool
	newLogLines                    int
	logRenderer                    *lipgloss.Renderer
	showAll, muted                 bool
	collectorMode                  string
	collectorErr                   string
	collectorLast                  string
	diagnostics                    *collectorDiagnostics
	collectorRetryAt               time.Time
	collectorDecided, ownCollector bool
	width, height                  int
	sized                          bool
	viewport                       viewport.Model
	help                           help.Model
	keys                           keyMap
	generation                     uint64
	inFlight                       bool
	lastLogRead                    time.Time
	reader                         *LogReader
	logLines                       []LogLine
	logErr, rosterErr, stateErr    string
	notifier                       Notifier
	alerts                         Alerts
	now                            func() time.Time
	startCollector                 func(context.Context, string, string, io.Writer) error
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
	m := &Model{config: c, collapsed: map[string]bool{}, pane: "history", muted: c.muted, collectorMode: uiCollectorChecking, focus: "tree", followTail: true, logRenderer: lipgloss.DefaultRenderer(), width: 100, height: 30, viewport: viewport.New(98, 8), help: help.New(), now: time.Now, notifier: newMacNotifier(), startCollector: startCollector}
	m.keys = keyMap{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", uiHelpSelect)),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←→", uiHelpCollapse)),
		key.NewBinding(key.WithKeys("l"), key.WithHelp("l", uiHelpPane)),
		key.NewBinding(key.WithKeys("L"), key.WithHelp("L", uiHelpFullLog)),
		key.NewBinding(key.WithKeys("f"), key.WithHelp("f", uiHelpShowAll)),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", uiHelpFocus)),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", uiHelpSound)),
		key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", uiHelpQuit)),
	}
	m.readLog = func(r *LogReader, v visibleNode) (*LogReader, []LogLine, error) {
		if !v.local {
			return nil, nil, fmt.Errorf("remote-host: %s %s", uiNoLog, cleanText(v.host, 120))
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
		if os.IsNotExist(err) {
			return nil, nil, err
		}
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
		return rosterMsg{report: filterReport(r, c.ignore)}
	}
}
func (m *Model) pollState() tea.Cmd {
	if m.stateInFlight {
		return nil
	}
	m.stateInFlight = true
	p, home := m.config.statePath, m.config.home
	rows := append([]Row(nil), m.report.Local.Sessions...)
	now := m.now
	return func() tea.Msg {
		s, err := loadState(p)
		activity := map[string]time.Time{}
		at := now()
		for _, row := range rows {
			if row.Kind != "peer" || row.Product == collectorProduct {
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
		if !m.showAll && n.HiddenDuplicate {
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
	m.lastLogRead = time.Time{}
	m.inFlight = false
	m.logLines = nil
	m.logErr = ""
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.followTail = true
	m.newLogLines = 0
}
func (m *Model) recordFirstSeen() {
	if m.firstSeen == nil {
		m.firstSeen = map[string]time.Time{}
	}
	now := m.now()
	for _, host := range append([]Host{m.report.Local}, m.report.Remote...) {
		for _, row := range host.Sessions {
			if _, ok := m.firstSeen[row.SessionID]; !ok {
				m.firstSeen[row.SessionID] = now
			}
		}
	}
}
func (m *Model) rebuild() {
	m.tree = BuildTree(m.report, m.state, m.now(), m.activity, m.firstSeen)
	if m.showAll {
		applyDisplayNames(m.tree, true)
	}
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
	remoteHosts := map[string]bool{}
	var walk func(*Node, string, HostTree)
	walk = func(n *Node, parent string, h HostTree) {
		display := sessionDisplayName(n, parent, h.Host)
		if !h.Local {
			display += fmt.Sprintf(uiOnHost, h.Host)
		}
		displays[n.ID] = display
		for _, c := range n.Children {
			walk(c, n.Name, h)
		}
	}
	for _, h := range m.tree {
		if !h.Local {
			remoteHosts[h.Host] = true
		}
		for _, n := range h.Nodes {
			walk(n, "", h)
		}
	}
	for id := range askIDs(m.report, m.state) {
		rec := m.state.Sessions[id]
		display := displays[id]
		if display == "" {
			display = cleanText(id, 120)
			_, host, _ := strings.Cut(id, "@")
			if remoteHosts[host] {
				display += fmt.Sprintf(uiOnHost, cleanText(host, 120))
			}
		}
		since := rec.UpdatedAt
		for i := len(rec.History) - 1; i >= 0; i-- {
			if rec.History[i].Kind == "ask" {
				since = rec.History[i].At
				break
			}
		}
		// A tab exists only on this machine; the GUID is re-checked before it reaches a shell.
		tab := ""
		if i := strings.LastIndex(id, "@"); i >= 0 && id[i+1:] == m.report.Local.Host && validITermGUID(rec.Tab) {
			tab = rec.Tab
		}
		asks = append(asks, AskInfo{ID: id, Display: display, Text: rec.Ask, Tab: tab, Since: since})
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
				n.Notify(Notice{Title: a.Title, Body: a.Body, ID: a.ID, Tab: a.Tab})
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
	if m.reader != nil && m.reader.kind == "dsh" && !m.lastLogRead.IsZero() && m.now().Sub(m.lastLogRead) < 3*time.Second {
		return nil
	}
	m.lastLogRead = m.now()
	gen, r, read := m.generation, m.reader, m.readLog
	m.inFlight = true
	return func() tea.Msg { reader, lines, err := read(r, row); return logMsg{gen, reader, lines, err} }
}
func (m *Model) collectorDecision() tea.Cmd {
	external := false
	for _, r := range m.report.Local.Sessions {
		if r.Product == collectorProduct && r.Connected {
			external = true
			break
		}
	}
	if m.collectorDecided {
		if !m.ownCollector {
			m.collectorMode = uiCollectorMissing
			if external {
				m.collectorMode = uiCollectorExternal
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
		m.collectorMode = uiCollectorExternal
		return nil
	}
	m.collectorMode = uiCollectorOwn
	m.ownCollector = true
	start, c := m.startCollector, m.config
	diagnostics := &collectorDiagnostics{path: filepath.Join(filepath.Dir(c.statePath), "collector.log")}
	m.diagnostics = diagnostics
	m.collectorLast = ""
	if c.collectorWG != nil {
		c.collectorWG.Add(1)
	}
	return func() tea.Msg {
		if c.collectorWG != nil {
			defer c.collectorWG.Done()
		}
		err := start(c.ctx, c.statePath, c.groups, diagnostics)
		if err != nil {
			fmt.Fprintln(diagnostics, uiAppName+": "+err.Error())
		}
		return collectorMsg{err}
	}
}
func (m *Model) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	gen, pane := m.generation, m.pane
	defer func() {
		m.syncLogViewport()
		if gen != m.generation || pane != m.pane {
			if log := m.requestLog(); log != nil {
				if cmd == nil {
					cmd = log
				} else {
					cmd = tea.Batch(cmd, log)
				}
			}
		}
	}()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.sized = true
		m.width = max(4, msg.Width)
		m.height = max(4, msg.Height)
	case rosterTick:
		return m, m.pollRoster()
	case clockTick:
		return m, tea.Batch(m.pollState(), m.requestLog(), m.pollCollectorDiagnostics(), secondTick())
	case rosterMsg:
		delay := tea.Tick(m.config.interval, func(time.Time) tea.Msg { return rosterTick{} })
		if msg.err != nil {
			m.rosterErr = cleanText(msg.err.Error(), 200)
			return m, delay
		}
		m.report = msg.report
		m.rosterErr = ""
		m.recordFirstSeen()
		m.rebuild()
		return m, tea.Batch(m.collectorDecision(), m.alertCmd(), delay)
	case stateMsg:
		m.stateInFlight = false
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
	case collectorDiagnosticMsg:
		if msg.source != m.diagnostics || msg.line == "" {
			return m, nil
		}
		m.collectorLast = cleanText(msg.line, 200)
		if m.collectorLast == uiCollectorConnected {
			if m.ownCollector {
				m.collectorErr = ""
			}
		} else {
			m.collectorErr = m.collectorLast
		}
	case collectorMsg:
		if m.config.ctx.Err() == nil {
			m.collectorMode = uiCollectorMissing
			m.ownCollector = false
			m.collectorDecided = false
			m.collectorRetryAt = m.now().Add(10 * time.Second)
			if msg.err != nil {
				m.collectorErr = cleanText(msg.err.Error(), 200)
			} else {
				m.collectorErr = uiCollectorStopped
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
			if !m.followTail {
				m.newLogLines += len(msg.lines)
			}
			m.logLines = append(m.logLines, msg.lines...)
			if len(m.logLines) > 200 {
				drop := len(m.logLines) - 200
				if !m.followTail {
					height := strings.Count(m.renderLogRecords(m.logLines[:drop], m.viewport.Width), "\n") + 1
					m.viewport.SetYOffset(max(0, m.viewport.YOffset-height))
				}
				m.logLines = m.logLines[drop:]
			}
		}
	case tea.KeyMsg:
		if m.focus == "log" {
			scrolled := true
			switch msg.String() {
			case "up":
				m.viewport.LineUp(1)
			case "down":
				m.viewport.LineDown(1)
			case "pgup":
				m.viewport.PageUp()
			case "pgdown":
				m.viewport.PageDown()
			case "g":
				m.viewport.GotoTop()
			case "G":
				m.viewport.GotoBottom()
			default:
				scrolled = false
			}
			if scrolled {
				m.followTail = m.viewport.AtBottom()
				if m.followTail {
					m.newLogLines = 0
				}
				return m, nil
			}
		}
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
				m.focus = "tree"
			}
		case "L":
			m.pane = "logFull"
			m.focus = "log"
		case "esc":
			m.focus = "tree"
			if m.pane == "logFull" {
				m.pane = "log"
			}
		case "tab":
			if m.pane == "logFull" {
				m.pane = "log"
				m.focus = "tree"
				break
			}
			if m.focus == "tree" {
				m.focus = "log"
				if m.pane == "history" {
					m.pane = "log"
				}
			} else {
				m.focus = "tree"
			}
		case "f":
			m.showAll = !m.showAll
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
	bindings := m.keys
	if m.focus == "log" {
		bindings = keyMap{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", uiHelpScroll)), key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("PgUp/PgDn", uiHelpPage)),
			key.NewBinding(key.WithKeys("g"), key.WithHelp("g", uiHelpTop)), key.NewBinding(key.WithKeys("G"), key.WithHelp("G", uiHelpTail)),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", uiHelpFocus)), key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", uiHelpBack)),
			key.NewBinding(key.WithKeys("l"), key.WithHelp("l", uiHelpPane)),
			key.NewBinding(key.WithKeys("L"), key.WithHelp("L", uiHelpFullLog)), key.NewBinding(key.WithKeys("s"), key.WithHelp("s", uiHelpSound)), key.NewBinding(key.WithKeys("q"), key.WithHelp("q", uiHelpQuit)),
		}
	}
	for _, b := range bindings {
		pieces = append(pieces, h.ShortHelpView([]key.Binding{b}))
	}
	pieces = append(pieces, uiLanesLegend)
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
	rec := m.state.Sessions[m.selected]
	var lines []string
	for _, e := range rec.History {
		lines = append(lines, e.At.Format("15:04:05")+" "+cleanText(e.Kind, 20)+"  "+cleanText(e.Text, 120))
	}
	if len(lines) == 0 {
		return uiHistoryEmpty
	}
	return strings.Join(lines, "\n")
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
	collector := uiCollector + m.collectorMode
	if m.collectorErr != "" {
		collector = uiCollector + uiCollectorErrorState
	}
	if m.collectorMode == uiCollectorMissing {
		collector = "\x1b[31m" + collector + "\x1b[0m"
	}
	badges := ""
	if m.showAll {
		badges += " · " + uiShowAll
	}
	if hidden := hiddenDuplicates(m.tree); hidden > 0 && !m.showAll {
		badges += " · " + fmt.Sprintf(uiHidden, hidden)
	}
	count := len(askIDs(m.report, m.state))
	if count > 0 {
		badges += " · " + fmt.Sprintf(uiWaitingCount, count)
	}
	core := uiAppName + " · " + collector + " · " + icon
	head := uiAppName + " · " + cleanText(m.report.Local.Host, 120) + " · " + m.now().Format("15:04:05") + " · " + collector + " · " + icon + badges
	if lipgloss.Width(head) > width {
		head = core + badges
	}
	if lipgloss.Width(head) > width && count > 0 {
		head = strings.Replace(head, uiWaitingSuffix, "", 1)
	}
	if lipgloss.Width(head) > width {
		head = core
		if count > 0 {
			head += fmt.Sprintf(" · ⚠ %d", count)
		}
	}
	bodyHeight := max(1, height-len(helpLines)-1)
	var body []string
	if m.rosterErr != "" {
		body = append(body, uiRosterError+m.rosterErr)
	}
	if m.stateErr != "" {
		body = append(body, uiStateError+m.stateErr)
	}
	if m.collectorErr != "" {
		body = append(body, uiCollectorError+m.collectorErr)
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
		labels := sessionLabels(m.tree, m.collapsed, m.showAll)
		var measure []string
		for _, v := range rows {
			measure = append(measure, labels[v.node.ID])
			byHost[v.host] = append(byHost[v.host], v)
		}
		table := newTreeTable(width, measure)
		body = append(body, table.header())
		treeHeight--
		for _, h := range m.tree {
			if !h.Local || h.Error != "" {
				line := "── " + h.Host
				if h.Error != "" {
					line += uiUnavailable + h.Error + ")"
				}
				lines = append(lines, clipLine(line+" "+strings.Repeat("─", width), width))
			}
			for _, v := range byHost[h.Host] {
				n := v.node
				line := table.row(n, labels[n.ID])
				if n.Ask {
					line = "\x1b[31m" + line + "\x1b[0m"
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
	title := uiHistory
	if m.pane != "history" {
		title = uiLog
	}
	if v, ok := m.selectedRow(); ok {
		title += v.node.Display
	}
	if m.pane != "history" {
		title += uiLiveSuffix
		if m.focus == "log" {
			title += " · " + uiFocusLog
		}
	}
	body = append(body, "── "+title+" "+strings.Repeat("─", width))
	if m.pane == "history" {
		vp := viewport.New(width, max(0, paneHeight-1))
		vp.SetContent(m.paneContent())
		vp.GotoBottom()
		if vp.Height > 0 {
			body = append(body, strings.Split(vp.View(), "\n")...)
		}
	} else {
		if m.viewport.Height > 0 {
			body = append(body, strings.Split(m.viewport.View(), "\n")...)
		}
		footer := ""
		if m.newLogLines > 0 {
			footer = fmt.Sprintf("▼ %d", m.newLogLines)
		}
		body = append(body, strings.Repeat(" ", max(0, width-lipgloss.Width(footer)))+footer)
	}
	lines := append([]string{head}, body...)
	lines = append(lines, helpLines...)
	for i := range lines {
		lines[i] = clipLine(lines[i], width)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(width).Render(strings.Join(lines, "\n"))
}

func clipLine(s string, width int) string { return lipgloss.NewStyle().MaxWidth(width).Render(s) }
