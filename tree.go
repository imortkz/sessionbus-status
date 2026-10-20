package main

import (
	"encoding/json"
	"fmt"
	"sort"
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
	renderHost(&out, report.Local, state, now, color, false)
	for _, host := range report.Remote {
		renderHost(&out, host, state, now, color, true)
	}
	return out.String()
}
func rowName(row Row) string {
	if row.Name != "" {
		return sanitize(row.Name)
	}
	return sanitize(row.SessionID)
}
func renderHost(out *strings.Builder, host Host, state State, now time.Time, color, remote bool) {
	if host.Error != "" {
		fmt.Fprintf(out, "── %s: недоступен (%s)\n", sanitize(host.Host), sanitize(host.Error))
		return
	}
	if remote {
		fmt.Fprintf(out, "── %s\n", sanitize(host.Host))
	}
	rows := make(map[string]Row)
	visible := make([]Row, 0, len(host.Sessions))
	for _, row := range host.Sessions {
		if row.Product == "bus-status" {
			continue
		}
		rows[row.SessionID] = row
		visible = append(visible, row)
	}
	host.Sessions = visible
	children := make(map[string][]Row)
	var roots []Row
	for _, row := range host.Sessions {
		if _, ok := rows[row.Owner]; row.Owner == "" || !ok {
			roots = append(roots, row)
		} else {
			children[row.Owner] = append(children[row.Owner], row)
		}
	}
	var hasAsk func(Row, map[string]bool) bool
	hasAsk = func(row Row, seen map[string]bool) bool {
		if seen[row.SessionID] {
			return false
		}
		seen[row.SessionID] = true
		if sanitize(state.Sessions[row.SessionID].Ask) != "" {
			return true
		}
		for _, child := range children[row.SessionID] {
			if hasAsk(child, seen) {
				return true
			}
		}
		return false
	}
	sortRows := func(list []Row) {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			aa, ba := hasAsk(a, make(map[string]bool)), hasAsk(b, make(map[string]bool))
			if aa != ba {
				return aa
			}
			if a.Running != b.Running {
				return a.Running
			}
			an, bn := rowName(a), rowName(b)
			if an != bn {
				return an < bn
			}
			return a.SessionID < b.SessionID
		})
	}
	visited := make(map[string]bool)
	var draw func(Row, string, string, string)
	draw = func(row Row, prefix, branch, continuation string) {
		if visited[row.SessionID] {
			return
		}
		visited[row.SessionID] = true
		rec, exists := state.Sessions[row.SessionID]
		ask := sanitize(rec.Ask)
		icon := "○"
		if row.Running {
			icon = "●"
		}
		if ask != "" {
			icon = "⚠"
		}
		product := sanitize(row.Product)
		harness := strings.TrimSuffix(product, "-peer")
		if product == "sessionbus-dsh" {
			harness = "dsh"
		}
		if model := sanitize(rec.Model); model != "" {
			harness += " · " + model
		}
		status := sanitize(rec.Status)
		if ask != "" {
			status = "ЖДЁТ: " + ask
		}
		if status == "" {
			status = "нет статуса"
		}
		if exists && !rec.UpdatedAt.IsZero() {
			status += "  " + age(now.Sub(rec.UpdatedAt))
		}
		sub := children[row.SessionID]
		sortRows(sub)
		statusPrefix := prefix + continuation + "   "
		if branch == "" {
			statusPrefix = "     "
			if len(sub) > 0 {
				statusPrefix = "  │  "
			}
		}
		line1 := prefix + branch + icon + " " + rowName(row) + "    " + harness
		line2 := statusPrefix + status
		if ask != "" && color {
			line1 = "\x1b[31m" + line1 + "\x1b[0m"
			line2 = "\x1b[31m" + line2 + "\x1b[0m"
		}
		out.WriteString(line1 + "\n" + line2 + "\n")
		childPrefix := prefix + continuation
		if branch == "" {
			childPrefix = "  "
		}
		for i, child := range sub {
			b, c := "├─ ", "│ "
			if i == len(sub)-1 {
				b, c = "└─ ", "  "
			}
			draw(child, childPrefix, b, c)
		}
	}
	sortRows(roots)
	for _, row := range roots {
		draw(row, "", "", "")
	}
	// Malformed owner cycles still get displayed once, rather than disappearing.
	remaining := append([]Row(nil), host.Sessions...)
	sortRows(remaining)
	for _, row := range remaining {
		if !visited[row.SessionID] {
			draw(row, "", "", "")
		}
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
