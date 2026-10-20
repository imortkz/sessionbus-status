package main

import (
	"strings"
	"time"
)

type Entry struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

type Record struct {
	History   []Entry   `json:"history,omitempty"`
	Product   string    `json:"product"`
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	Ask       string    `json:"ask"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Update struct{ Kind, Text string }

func sanitize(text string) string { return cleanText(text, 120) }
func ParseBody(body string) []Update {
	var updates []Update
	for _, line := range strings.Split(body, "\n") {
		kind, text, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		kind = strings.ToLower(kind)
		if kind != "status" && kind != "ask" && kind != "model" {
			continue
		}
		text = sanitize(text)
		if text != "" {
			updates = append(updates, Update{kind, text})
		}
	}
	return updates
}
func Apply(rec Record, updates []Update, now time.Time) Record {
	for _, u := range updates {
		text := sanitize(u.Text)
		if text == "" {
			continue
		}
		switch u.Kind {
		case "status":
			rec.Status = text
			rec.Ask = ""
		case "ask":
			rec.Ask = text
		case "model":
			rec.Model = text
		default:
			continue
		}
		rec.History = append(append([]Entry(nil), rec.History...), Entry{At: now, Kind: u.Kind, Text: text})
		if len(rec.History) > 20 {
			rec.History = rec.History[len(rec.History)-20:]
		}
		rec.UpdatedAt = now
	}
	return rec
}
