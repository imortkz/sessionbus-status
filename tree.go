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
func askIDs(report Report, state State) map[string]bool {
	ids := make(map[string]bool)
	for _, host := range append([]Host{report.Local}, report.Remote...) {
		if host.Error != "" {
			continue
		}
		for _, row := range host.Sessions {
			if row.Product != "bus-status" && sanitize(state.Sessions[row.SessionID].Ask) != "" {
				ids[row.SessionID] = true
			}
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
		text += fmt.Sprintf("    ⚠ %d ждёт тебя", count)
	}
	if incomplete {
		text += "  · федерация неполная"
	}
	return text + "\n\n"
}
func Render(report Report, state State, now time.Time, color bool) string {
	var out strings.Builder
	trees := BuildTree(report, state, now, nil)
	heading := header(now, report.Local.Host, len(askIDs(report, state)), !report.Complete)
	if count := hiddenDuplicates(trees); count > 0 {
		heading = strings.TrimSuffix(heading, "\n\n") + fmt.Sprintf(" · скрыто %d\n\n", count)
	}
	out.WriteString(heading)
	if report.Error != "" {
		out.WriteString("Ошибка: " + sanitize(report.Error) + "\n")
	}
	var labels []string
	var measure func(*Node, string, int)
	measure = func(n *Node, parent string, depth int) {
		if n.HiddenDuplicate {
			return
		}
		labels = append(labels, sessionText(n, parent, depth, false))
		for _, c := range n.Children {
			measure(c, n.Name, depth+1)
		}
	}
	for _, h := range trees {
		for _, n := range h.Nodes {
			measure(n, "", 0)
		}
	}
	table := newTreeTable(100, labels)
	out.WriteString(table.header() + "\n")
	for _, host := range trees {
		renderHost(&out, host, color, table)
	}
	return out.String()
}

type treeTable struct{ width, session, status int }

func newTreeTable(width int, labels []string) treeTable {
	session := 6
	for _, s := range labels {
		session = max(session, lipgloss.Width(s))
	}
	session = min(32, session, max(6, width-32))
	return treeTable{width, session, max(1, width-session-31)}
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
	return "  " + tableCell("СЕССИЯ", t.session) + " " + tableCell("СТАТУС", t.status) + " " + tableCell("⏱", 4) + " " + tableCell("МОДЕЛЬ", 14) + " " + tableCell("ХАРНЕСС", 7)
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
func sessionText(n *Node, parent string, depth int, collapsed bool) string {
	name := n.Display
	if parent != "" {
		name = strings.TrimPrefix(name, parent+"/")
	}
	branch := ""
	if len(n.Children) > 0 {
		branch = "▾ "
		if collapsed {
			branch = "▸ "
		}
	} else if depth > 0 {
		branch = "└ "
	}
	return strings.Repeat("  ", depth) + branch + name
}
func (t treeTable) row(n *Node, label string) string {
	return tableCell(n.Icon(), 1) + " " + tableCell(label, t.session) + " " + tableCell(n.Line, t.status) + " " + tableCell(n.Age, 4) + " " + tableCell(shortModel(n), 14) + " " + tableCell(n.Harness, 7)
}
func renderHost(out *strings.Builder, host HostTree, color bool, table treeTable) {
	if !host.Local || host.Error != "" {
		line := "── " + host.Host
		if host.Error != "" {
			line += ": недоступен (" + host.Error + ")"
		}
		out.WriteString(clipLine(line+" "+strings.Repeat("─", table.width), table.width) + "\n")
	}
	if host.Error != "" {
		return
	}
	var draw func(*Node, string, int)
	draw = func(n *Node, parent string, depth int) {
		if n.HiddenDuplicate {
			return
		}
		line := table.row(n, sessionText(n, parent, depth, false))
		if color {
			if n.Ask {
				line = "\x1b[31m" + line + "\x1b[0m"
			} else if n.Stale {
				line = "\x1b[2m" + line + "\x1b[0m"
			}
		}
		out.WriteString(strings.TrimRight(line, " ") + "\n")
		for _, c := range n.Children {
			draw(c, n.Name, depth+1)
		}
	}
	for _, n := range host.Nodes {
		draw(n, "", 0)
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
	return header(now, "", 0, false) + "Ошибка roster: " + sanitize(message) + "\n"
}
