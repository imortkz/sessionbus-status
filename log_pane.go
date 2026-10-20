package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Data is cleaned before any style is applied; only generated styles contain ANSI.
func (m *Model) renderLogs(width int) string {
	if m.logErr != "" {
		return lipgloss.NewStyle().Width(width).Render(cleanText(m.logErr, 200))
	}
	if len(m.logLines) == 0 {
		return uiLogWaiting
	}
	const indent = 19 // time (8), two spaces, kind (7), two spaces
	textWidth := max(1, width-indent)
	var rows []string
	for _, line := range m.logLines {
		kind := cleanText(line.Kind, 20)
		text := cleanLogText(line.Text, logLimit(kind))
		tool := cleanText(line.Tool, 120)
		style := m.logRenderer.NewStyle()
		if m.config.color {
			switch kind {
			case "TEXT":
				style = style.Foreground(lipgloss.Color("2"))
			case "TOOL":
				style = style.Foreground(lipgloss.Color("6"))
			case "RESULT":
				style = style.Faint(true)
			case "THINK":
				style = style.Foreground(lipgloss.Color("3"))
			case "SYS":
				style = style.Foreground(lipgloss.Color("5"))
			}
		}
		if tool != "" {
			if m.config.color {
				tool = m.logRenderer.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(tool)
			}
			text = tool + " " + text
		}
		if kind == "RESULT" && m.config.color {
			text = style.Render(text)
		}
		wrapped := lipgloss.NewStyle().Width(textWidth).Render(text)
		stamp := uiNoTime
		if !line.At.IsZero() {
			stamp = line.At.Format("15:04:05")
		}
		prefix := stamp + "  " + style.Render(tableCell(kind, 7)) + "  "
		for i, part := range strings.Split(wrapped, "\n") {
			part = strings.TrimRight(part, " ")
			if i == 0 {
				rows = append(rows, prefix+part)
			} else {
				rows = append(rows, strings.Repeat(" ", indent)+part)
			}
		}
	}
	return strings.Join(rows, "\n")
}

func (m *Model) syncLogViewport() {
	if !m.sized {
		return
	}
	width := max(2, m.width-2)
	height := max(2, m.height-2)
	bodyHeight := max(1, height-len(m.helpLines(width))-1)
	errors := 0
	for _, err := range []string{m.rosterErr, m.stateErr, m.collectorErr} {
		if err != "" {
			errors++
		}
	}
	available := max(1, bodyHeight-errors)
	paneHeight := min(available, max(5, bodyHeight*40/100))
	if m.pane == "logFull" {
		paneHeight = available
	}
	m.viewport.Width = width
	m.viewport.Height = max(0, paneHeight-2)
	offset := m.viewport.YOffset
	m.viewport.SetContent(m.renderLogs(width))
	if m.followTail {
		m.viewport.GotoBottom()
		m.newLogLines = 0
	} else {
		m.viewport.SetYOffset(offset)
	}
}
