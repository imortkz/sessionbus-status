package main

import (
	"regexp"
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
	Tab       string    `json:"tab,omitempty"`
	Status    string    `json:"status"`
	Ask       string    `json:"ask"`
	UpdatedAt time.Time `json:"updated_at"`
}
var itermGUIDRE = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// validITermGUID gates every value that may reach a shell command line.
func validITermGUID(s string) bool { return itermGUIDRE.MatchString(s) }

// tabGUID takes ITERM_SESSION_ID ("w1t6p0:GUID") or a bare GUID and returns the GUID or "".
func tabGUID(v string) string {
	if i := strings.LastIndex(v, ":"); i >= 0 {
		v = v[i+1:]
	}
	if validITermGUID(v) {
		return v
	}
	return ""
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
		if kind != "status" && kind != "ask" && kind != "model" && kind != "tab" {
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
		case "tab":
			if g := tabGUID(text); g != "" {
				rec.Tab = g
			}
			continue
		default:
			continue
		}
		rec.History = append(append([]Entry(nil), rec.History...), Entry{At: now, Kind: u.Kind, Text: text})
		if len(rec.History) > 20 {
			rec.History = rec.History[len(rec.History)-20:]
		}
		if u.Kind != "model" {
			rec.UpdatedAt = now
		}
	}
	return rec
}
