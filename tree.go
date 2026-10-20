package main

import (
	"encoding/json"
	"fmt"
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
	out.WriteString(header(now, report.Local.Host, len(askIDs(report, state)), !report.Complete))
	if report.Error != "" {
		out.WriteString("Ошибка: " + sanitize(report.Error) + "\n")
	}
	for _, host := range BuildTree(report, state, now, nil) {
		renderHost(&out, host, color)
	}
	return out.String()
}
func renderHost(out *strings.Builder, host HostTree, color bool) {
	if host.Error != "" {
		fmt.Fprintf(out, "── %s: недоступен (%s)\n", host.Host, host.Error)
		return
	}
	if !host.Local {
		fmt.Fprintf(out, "── %s\n", host.Host)
	}
	var draw func(*Node, string, string, string)
	draw = func(n *Node, prefix, branch, continuation string) {
		icon := n.Icon()
		harness := n.Harness
		if n.Model != "" {
			harness += " · " + n.Model
		}
		status := n.Line
		if n.Age != "" {
			status += "  " + n.Age
		}
		statusPrefix := prefix + continuation + "   "
		if branch == "" {
			statusPrefix = "     "
			if len(n.Children) > 0 {
				statusPrefix = "  │  "
			}
		}
		line1 := prefix + branch + icon + " " + n.Display + "    " + harness
		line2 := statusPrefix + status
		if color {
			style := ""
			if n.Ask {
				style = "\x1b[31m"
			} else if n.Stale {
				style = "\x1b[2m"
			}
			if style != "" {
				line1 = style + line1 + "\x1b[0m"
				line2 = style + line2 + "\x1b[0m"
			}
		}
		out.WriteString(line1 + "\n" + line2 + "\n")
		childPrefix := prefix + continuation
		if branch == "" {
			childPrefix = "  "
		}
		for i, c := range n.Children {
			b, k := "├─ ", "│ "
			if i == len(n.Children)-1 {
				b, k = "└─ ", "  "
			}
			draw(c, childPrefix, b, k)
		}
	}
	for _, n := range host.Nodes {
		draw(n, "", "", "")
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
