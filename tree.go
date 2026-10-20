package main

import (
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"time"
)

type Row struct {
	SessionID      string   `json:"session_id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Product        string   `json:"product"`
	Groups         []string `json:"groups"`
	Connected      bool     `json:"connected"`
	Running        bool     `json:"running"`
	Owner          string   `json:"owner"`
	Persistent     bool     `json:"persistent"`
	PermissionMode string   `json:"permission_mode"`
}
type Host struct {
	Host     string   `json:"host"`
	Products []string `json:"products"`
	Sessions []Row    `json:"sessions"`
	Error    string   `json:"error"`
}
type Report struct {
	Schema     string `json:"schema"`
	Federation string `json:"federation"`
	Complete   bool   `json:"complete"`
	Local      Host   `json:"local"`
	Remote     []Host `json:"remote"`
	Error      string `json:"error"`
}

func decodeReport(data []byte) (Report, error) {
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return report, err
	}
	if report.Schema != "sessionbus.roster.v1" || report.Local.Host == "" {
		return report, fmt.Errorf("invalid roster report")
	}
	return report, nil
}

// A pending ask must belong to a current row, or to a host whose roster failed.
// This same set drives both notifications and header counts.
func askIDs(report Report, state State) map[string]bool {
	present, collectors, failedHosts := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, host := range append([]Host{report.Local}, report.Remote...) {
		if host.Error != "" {
			failedHosts[host.Host] = true
		}
		for _, row := range host.Sessions {
			present[row.SessionID] = true
			if row.Product == "bus-status" {
				collectors[row.SessionID] = true
			}
		}
	}
	ids := map[string]bool{}
	for id, rec := range state.Sessions {
		_, host, _ := strings.Cut(id, "@")
		if !collectors[id] && rec.Product != "bus-status" && sanitize(rec.Ask) != "" && (present[id] || failedHosts[host]) {
			ids[id] = true
		}
	}
	return ids
}
func newAsks(prev, cur map[string]bool) bool {
	for id, present := range cur {
		if present && !prev[id] {
			return true
		}
	}
	return false
}
func header(now time.Time, host string, count int, incomplete bool) string {
	text := "bus-status · " + now.Format("15:04:05")
	if host != "" {
		text += " · " + sanitize(host)
	}
	if count > 0 {
		text += "    " + fmt.Sprintf(uiWaitingCount, count)
	}
	if incomplete {
		text += "  · " + uiIncomplete
	}
	return text + "\n\n"
}
func Render(report Report, state State, now time.Time, color bool) string {
	var out strings.Builder
	trees := BuildTree(report, state, now, nil)
	heading := header(now, report.Local.Host, len(askIDs(report, state)), !report.Complete)
	if count := hiddenDuplicates(trees); count > 0 {
		heading = strings.TrimSuffix(heading, "\n\n") + " · " + fmt.Sprintf(uiHidden, count) + "\n\n"
	}
	out.WriteString(heading)
	if report.Error != "" {
		out.WriteString(uiReportError + sanitize(report.Error) + "\n")
	}
	labels := sessionLabels(trees, nil, false)
	var measure []string
	for _, label := range labels {
		measure = append(measure, label)
	}
	table := newTreeTable(100, measure)
	out.WriteString(table.header() + "\n")
	for _, host := range trees {
		renderHost(&out, host, color, table, labels)
	}
	return out.String()
}

const treeAgeWidth = 8
const treeFixedWidth = 1 + 5 + treeAgeWidth + 14 + 7

type treeTable struct{ width, session, status int }

func newTreeTable(width int, labels []string) treeTable {
	session := len(uiSession)
	for _, s := range labels {
		session = max(session, lipgloss.Width(s))
	}
	session = min(32, session, max(len(uiSession), width-treeFixedWidth-1))
	return treeTable{width, session, max(1, width-session-treeFixedWidth)}
}
func tableCell(s string, width int) string {
	if lipgloss.Width(s) > width {
		if width <= 1 {
			s = "…"
		} else {
			s = clipLine(s, width-1) + "…"
		}
	}
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}
func (t treeTable) header() string {
	return "  " + tableCell(uiSession, t.session) + " " + tableCell(uiStatus, t.status) + " " + tableCell(uiAge, treeAgeWidth) + " " + tableCell(uiModel, 14) + " " + tableCell(uiHarness, 7)
}
func shortModel(n *Node) string {
	model := n.Model
	if n.Harness == "dsh" {
		if _, short, ok := strings.Cut(model, "/"); ok {
			model = short
		}
	}
	return strings.TrimPrefix(model, "claude-")
}

// Strip only this row's host suffix; internal @ characters belong to the name.
func sessionDisplayName(n *Node, parent, host string) string {
	name := n.Display
	if n.Name != "" && host != "" {
		name = strings.TrimSuffix(n.Name, "@"+host) + strings.TrimPrefix(n.Display, n.Name)
	}
	if n.Lane && parent != "" {
		if host != "" {
			parent = strings.TrimSuffix(parent, "@"+host)
		}
		name = strings.TrimPrefix(name, parent+"/")
	}
	return name
}

// Session labels share sibling and ancestor connectors across both renderers.
func sessionLabels(trees []HostTree, collapsed map[string]bool, showAll bool) map[string]string {
	labels := map[string]string{}
	visible := func(nodes []*Node) []*Node {
		var out []*Node
		for _, n := range nodes {
			if showAll || !n.HiddenDuplicate {
				out = append(out, n)
			}
		}
		return out
	}
	var walk func(*Node, string, string, string, string, bool)
	walk = func(n *Node, parent, prefix, branch, host string, last bool) {
		name := sessionDisplayName(n, parent, host)
		children := visible(n.Children)
		arrow := ""
		if len(children) > 0 {
			arrow = "▾ "
			if collapsed[n.ID] {
				arrow = "▸ "
			}
		}
		labels[n.ID] = prefix + branch + arrow + name
		if collapsed[n.ID] {
			return
		}
		childPrefix := "  "
		if branch != "" {
			childPrefix = prefix + "│ "
			if last {
				childPrefix = prefix + "  "
			}
		}
		for i, c := range children {
			b := "├ "
			isLast := i == len(children)-1
			if isLast {
				b = "└ "
			}
			walk(c, n.Name, childPrefix, b, host, isLast)
		}
	}
	for _, h := range trees {
		for _, n := range visible(h.Nodes) {
			walk(n, "", "", "", h.Host, true)
		}
	}
	return labels
}
func (t treeTable) row(n *Node, label string) string {
	ageText := n.Age
	if n.Lane && !n.Running {
		ageText = strings.TrimSpace(uiIdle + " " + ageText)
	}
	return tableCell(n.Icon(), 1) + " " + tableCell(label, t.session) + " " + tableCell(n.Line, t.status) + " " + tableCell(ageText, treeAgeWidth) + " " + tableCell(shortModel(n), 14) + " " + tableCell(n.Harness, 7)
}
func renderHost(out *strings.Builder, host HostTree, color bool, table treeTable, labels map[string]string) {
	if !host.Local || host.Error != "" {
		line := "── " + host.Host
		if host.Error != "" {
			line += uiUnavailable + host.Error + ")"
		}
		out.WriteString(clipLine(line+" "+strings.Repeat("─", table.width), table.width) + "\n")
	}
	if host.Error != "" {
		return
	}
	var draw func(*Node)
	draw = func(n *Node) {
		if n.HiddenDuplicate {
			return
		}
		line := table.row(n, labels[n.ID])
		if color {
			if n.Ask {
				line = "\x1b[31m" + line + "\x1b[0m"
			}
		}
		out.WriteString(strings.TrimRight(line, " ") + "\n")
		for _, c := range n.Children {
			draw(c)
		}
	}
	for _, n := range host.Nodes {
		draw(n)
	}
}
func age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int64(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int64(d/(24*time.Hour)))
	}
}
func Frame(stdout, stderr []byte, runErr error, state State, now time.Time, color bool) string {
	report, err := decodeReport(stdout)
	if err == nil {
		return Render(report, state, now, color)
	}
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		if runErr != nil {
			message = runErr.Error()
		} else {
			message = err.Error()
		}
	}
	return header(now, "", 0, false) + uiRosterError + sanitize(message) + "\n"
}
