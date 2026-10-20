package main

import (
	"os/exec"
	"sort"
	"time"
)

type AskInfo struct {
	ID, Display, Text string
	Since             time.Time
}
type Action struct{ Kind, Title, Body string }
type Alerts struct {
	initialized bool
	muted       bool
	last        time.Time
	known       map[string]bool
}

func (a *Alerts) Tick(now time.Time, asks []AskInfo) []Action {
	asks = append([]AskInfo(nil), asks...)
	sort.Slice(asks, func(i, j int) bool {
		if !asks[i].Since.Equal(asks[j].Since) {
			return asks[i].Since.Before(asks[j].Since)
		}
		return asks[i].ID < asks[j].ID
	})
	if !a.initialized {
		a.initialized = true
		a.last = now
		a.known = map[string]bool{}
		for _, ask := range asks {
			a.known[ask.ID] = true
		}
		return nil
	}
	if len(asks) == 0 {
		a.known = nil
		a.last = time.Time{}
		return nil
	}
	known := make(map[string]bool)
	var actions []Action
	emit := func(ask AskInfo) {
		if !a.muted {
			actions = append(actions, Action{Kind: "sound"}, Action{Kind: "notify", Title: uiAppName, Body: cleanText(ask.Display+uiNotifyAsks+ask.Text, 200)})
		}
	}
	newAsk := false
	for _, ask := range asks {
		known[ask.ID] = true
		if !a.known[ask.ID] {
			newAsk = true
			emit(ask)
		}
	}
	if newAsk {
		a.last = now
	} else if now.Sub(a.last) >= time.Minute {
		emit(asks[0])
		a.last = now
	}
	a.known = known
	return actions
}

type Notifier interface {
	Sound(path string)
	Notify(title, body string)
}
type commandStarter func(name string, args ...string)
type macNotifier struct{ start commandStarter }

func startCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}
func (n macNotifier) starter() commandStarter {
	if n.start != nil {
		return n.start
	}
	return startCommand
}
func (n macNotifier) Sound(path string) { n.starter()("afplay", path) }
func (n macNotifier) Notify(title, body string) {
	n.starter()("osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body)
}
