package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

type AskInfo struct {
	ID, Display, Text, Tab string
	Since                  time.Time
}
type Action struct{ Kind, Title, Body, ID, Tab string }
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
			actions = append(actions, Action{Kind: "sound"}, Action{Kind: "notify", Title: uiAppName, Body: cleanText(ask.Display+uiNotifyAsks+ask.Text, 200), ID: ask.ID, Tab: ask.Tab})
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
	Notify(Notice)
}

// Notice is one notification; Tab is an iTerm2 session GUID to select on click.
type Notice struct{ Title, Body, ID, Tab string }
type commandStarter func(*exec.Cmd)

// macNotifier prefers terminal-notifier (click selects the asker's iTerm2 tab,
// else activates the terminal) and falls back to osascript.
type macNotifier struct {
	start        commandStarter
	run          func(*exec.Cmd) (string, error)
	notifierPath string
	bundle       string
}

var bundleRE = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)

// newMacNotifier resolves terminal-notifier and the click target once.
func newMacNotifier() macNotifier {
	n := macNotifier{}
	n.notifierPath, _ = exec.LookPath("terminal-notifier")
	if b := os.Getenv("__CFBundleIdentifier"); bundleRE.MatchString(b) {
		n.bundle = b
	}
	return n
}
func startCommand(cmd *exec.Cmd) {
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}
func runCapture(cmd *exec.Cmd) (string, error) {
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
func (n macNotifier) starter() commandStarter {
	if n.start != nil {
		return n.start
	}
	return startCommand
}
func (n macNotifier) launch(name string, args ...string) {
	cmd := exec.Command(name, args...)
	// nil streams connect to the null device, never the live TUI terminal.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	n.starter()(cmd)
}
func (n macNotifier) Sound(path string) { n.launch("afplay", path) }

// selectITermCommand runs under /bin/sh from terminal-notifier -execute. Only a
// GUID that passed validITermGUID is interpolated; the check lives here too
// because Notify can receive a Tab that bypassed the protocol parser.
func selectITermCommand(guid string) string {
	script := `tell application "iTerm2"
repeat with w in windows
repeat with t in tabs of w
repeat with s in sessions of t
if unique id of s is "` + guid + `" then
select w
select t
select s
end if
end repeat
end repeat
end repeat
activate
end tell`
	return "/usr/bin/osascript -e '" + script + "'"
}
func (n macNotifier) terminalNotifierArgs(nt Notice) []string {
	group := uiAppName
	if nt.ID != "" {
		group += ":" + nt.ID
	}
	args := []string{"-title", nt.Title, "-message", nt.Body, "-group", group}
	if validITermGUID(nt.Tab) {
		args = append(args, "-execute", selectITermCommand(nt.Tab))
	} else if n.bundle != "" {
		args = append(args, "-activate", n.bundle)
	}
	return args
}

// notifierTimeout bounds a terminal-notifier call; a hung call falls back to osascript.
var notifierTimeout = 10 * time.Second

// terminalNotify reports whether terminal-notifier showed the notification.
func (n macNotifier) terminalNotify(nt Notice) bool {
	run := n.run
	if run == nil {
		run = runCapture
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifierTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, n.notifierPath, n.terminalNotifierArgs(nt)...)
	cmd.WaitDelay = time.Second
	out, err := run(cmd)
	return err == nil && !strings.Contains(out, "not allowed") && !strings.Contains(out, "Could not request notification permission")
}
func (n macNotifier) osascript(title, body string) {
	n.launch("osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body)
}
func (n macNotifier) Notify(nt Notice) {
	if n.notifierPath == "" {
		n.osascript(nt.Title, nt.Body)
		return
	}
	go func() {
		if !n.terminalNotify(nt) {
			n.osascript(nt.Title, nt.Body)
		}
	}()
}
