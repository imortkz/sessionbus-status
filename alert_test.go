package main

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAlertSchedule(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	a := Alerts{}
	a.Tick(now, nil)
	old := AskInfo{ID: "b", Display: "old", Text: "question", Since: now.Add(-time.Hour)}
	newer := AskInfo{ID: "a", Display: "new", Text: "new question", Since: now}
	check := func(at time.Time, asks []AskInfo, want string) {
		t.Helper()
		got := a.Tick(at, asks)
		if want == "" {
			if len(got) != 0 {
				t.Fatal(got)
			}
			return
		}
		if len(got) != 2 || got[0].Kind != "sound" || got[1].Kind != "notify" || got[1].Title != "sessionbus-status" || got[1].Body != want {
			t.Fatal(got)
		}
	}
	check(now, []AskInfo{old}, "old asks: question")
	check(now.Add(30*time.Second), []AskInfo{old}, "")
	check(now.Add(35*time.Second), []AskInfo{newer, old}, "new asks: new question")
	check(now.Add(95*time.Second), []AskInfo{newer, old}, "old asks: question")
	a.muted = true
	check(now.Add(155*time.Second), []AskInfo{old, newer}, "")
	a.muted = false
	check(now.Add(156*time.Second), []AskInfo{old, newer}, "")
	check(now.Add(157*time.Second), nil, "")
	check(now.Add(158*time.Second), []AskInfo{old}, "old asks: question")
	// Clearing one ID makes its reappearance new, even while another ask remains.
	check(now.Add(159*time.Second), []AskInfo{newer}, "new asks: new question")
	check(now.Add(160*time.Second), []AskInfo{newer, old}, "old asks: question")
	a = Alerts{}
	a.Tick(now, nil)
	got := a.Tick(now, []AskInfo{{ID: "z", Display: "z", Since: now}, {ID: "a", Display: "a", Since: now}})
	if len(got) != 4 || got[1].Body != "a asks:" || got[3].Body != "z asks:" {
		t.Fatal(got)
	}
	a = Alerts{}
	a.Tick(now, nil)
	got = a.Tick(now, []AskInfo{{ID: "x", Display: "\x1b\u202e" + strings.Repeat("я", 300), Text: "x"}})
	if len([]rune(got[1].Body)) != 200 || strings.ContainsAny(got[1].Body, "\x1b\u202e") {
		t.Fatal(got)
	}
}
func TestNotifierArgs(t *testing.T) {
	var calls [][]string
	n := macNotifier{start: func(cmd *exec.Cmd) { calls = append(calls, append([]string(nil), cmd.Args...)) }}
	body := `quote " & do shell script "bad"`
	n.Sound("sound with spaces.aiff")
	n.Notify(Notice{Title: "sessionbus-status", Body: body})
	want := [][]string{{"afplay", "sound with spaces.aiff"}, {"osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", "sessionbus-status", body}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("%q", calls)
	}
}

func TestAlertStartupSilent(t *testing.T) {
	now := time.Now()
	a := Alerts{}
	asks := []AskInfo{{ID: "old", Display: "old", Text: "question"}}
	if got := a.Tick(now, asks); len(got) != 0 {
		t.Fatal("startup burst", got)
	}
	if got := a.Tick(now.Add(59*time.Second), asks); len(got) != 0 {
		t.Fatal(got)
	}
	if got := a.Tick(now.Add(time.Minute), asks); len(got) != 2 {
		t.Fatal("missing reminder", got)
	}
	asks = append(asks, AskInfo{ID: "new", Display: "new", Text: "question"})
	if got := a.Tick(now.Add(61*time.Second), asks); len(got) != 2 || got[1].Body != "new asks: question" {
		t.Fatal(got)
	}
}

func TestNotifierStreams(t *testing.T) {
	var commands []*exec.Cmd
	n := macNotifier{start: func(cmd *exec.Cmd) {
		commands = append(commands, cmd)
		if cmd.Stdout != nil || cmd.Stderr != nil || cmd.Stdin != nil {
			t.Fatal("notifier inherits terminal streams", cmd)
		}
	}}
	n.Sound("fake.aiff")
	n.Notify(Notice{Title: "sessionbus-status", Body: "question"})
	if len(commands) != 2 || commands[0].Args[0] != "afplay" || commands[1].Args[0] != "osascript" {
		t.Fatal(commands)
	}
}

func TestNotifierTerminalNotifierArgs(t *testing.T) {
	body := `quote " $(x) ' & do shell script "bad"`
	plain := macNotifier{notifierPath: "/bin/tn"}
	want := []string{"-title", "sessionbus-status", "-message", body, "-group", "sessionbus-status"}
	if got := plain.terminalNotifierArgs(Notice{Title: "sessionbus-status", Body: body}); !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	full := macNotifier{notifierPath: "/bin/tn", bundle: "com.googlecode.iterm2"}
	if got := full.terminalNotifierArgs(Notice{Title: "sessionbus-status", Body: body}); !reflect.DeepEqual(got, append(want, "-activate", "com.googlecode.iterm2")) {
		t.Fatalf("%q", got)
	}
}

func TestNotifierEnvValidation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("__CFBundleIdentifier", "com.googlecode.iterm2; rm -rf /")
	if n := newMacNotifier(); n.bundle != "" || n.notifierPath != "" {
		t.Fatalf("%+v", n)
	}
	t.Setenv("__CFBundleIdentifier", "com.googlecode.iterm2")
	if n := newMacNotifier(); n.bundle != "com.googlecode.iterm2" {
		t.Fatalf("%+v", n)
	}
}

func TestNotifierFallbackToOsascript(t *testing.T) {
	cases := map[string]struct {
		out string
		err error
	}{
		"nonzero":    {err: errors.New("exit 1")},
		"permission": {out: "Could not request notification permission: Notifications are not allowed for this application"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			started := make(chan []string, 2)
			n := macNotifier{notifierPath: "/bin/tn",
				run:   func(cmd *exec.Cmd) (string, error) { return c.out, c.err },
				start: func(cmd *exec.Cmd) { started <- cmd.Args }}
			n.Notify(Notice{Title: "title", Body: "body"})
			select {
			case args := <-started:
				if args[0] != "osascript" || args[len(args)-1] != "body" {
					t.Fatalf("%q", args)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no osascript fallback")
			}
		})
	}
}

func TestNotifierSuccessSkipsOsascript(t *testing.T) {
	ran := make(chan []string, 1)
	started := make(chan []string, 1)
	n := macNotifier{notifierPath: "/bin/tn",
		run:   func(cmd *exec.Cmd) (string, error) { ran <- cmd.Args; return "", nil },
		start: func(cmd *exec.Cmd) { started <- cmd.Args }}
	n.Notify(Notice{Title: "title", Body: "body"})
	if args := <-ran; args[0] != "/bin/tn" {
		t.Fatalf("%q", args)
	}
	select {
	case args := <-started:
		t.Fatalf("unexpected fallback %q", args)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNotifierTimeoutFallsBack(t *testing.T) {
	script := t.TempDir() + "/terminal-notifier"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := notifierTimeout
	notifierTimeout = 100 * time.Millisecond
	defer func() { notifierTimeout = saved }()
	started := make(chan []string, 1)
	(macNotifier{notifierPath: script, start: func(cmd *exec.Cmd) { started <- cmd.Args }}).Notify(Notice{Title: "sessionbus-status", Body: "question"})
	select {
	case args := <-started:
		if args[0] != "osascript" {
			t.Fatalf("%q", args)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hung terminal-notifier did not fall back to osascript")
	}
}

const testGUID = "0EB11E86-12BA-4739-B792-1F3699034011"

func TestNotifierTabExecute(t *testing.T) {
	body := `quote " $(x) ' secret-body`
	n := macNotifier{notifierPath: "/bin/tn", bundle: "com.googlecode.iterm2"}
	got := n.terminalNotifierArgs(Notice{Title: "t", Body: body, ID: "s@h", Tab: testGUID})
	var exec string
	for i, a := range got {
		if a == "-activate" {
			t.Fatalf("-activate with tab: %q", got)
		}
		if a == "-execute" {
			exec = got[i+1]
		}
	}
	if !strings.HasPrefix(exec, "/usr/bin/osascript") || !strings.Contains(exec, testGUID) || strings.Contains(exec, "secret-body") {
		t.Fatalf("execute: %q", exec)
	}
}

func TestNotifierRejectsBadTab(t *testing.T) {
	n := macNotifier{notifierPath: "/bin/tn", bundle: "b.c"}
	for _, tab := range []string{testGUID + "'; touch /tmp/x; '", "w1t6p0:" + testGUID, "$(x)"} {
		got := n.terminalNotifierArgs(Notice{Title: "t", Body: "b", Tab: tab})
		for _, a := range got {
			if a == "-execute" || strings.Contains(a, "touch") {
				t.Fatalf("%q: %q", tab, got)
			}
		}
		if got[len(got)-2] != "-activate" {
			t.Fatalf("no activate fallback: %q", got)
		}
	}
}

func TestNotifierGroupPerSession(t *testing.T) {
	n := macNotifier{notifierPath: "/bin/tn"}
	group := func(id string) string {
		a := n.terminalNotifierArgs(Notice{Title: "t", Body: "b", ID: id})
		for i, v := range a {
			if v == "-group" {
				return a[i+1]
			}
		}
		return ""
	}
	if a, b := group("a@h"), group("b@h"); a == b || a != "sessionbus-status:a@h" {
		t.Fatalf("%q %q", a, b)
	}
}

func TestAlertScheduleCarriesIDAndTab(t *testing.T) {
	now := time.Now()
	a := Alerts{}
	a.Tick(now, nil)
	got := a.Tick(now, []AskInfo{{ID: "a", Display: "a", Tab: testGUID, Since: now}, {ID: "b", Display: "b", Since: now}})
	if len(got) != 4 || got[1].ID != "a" || got[1].Tab != testGUID || got[3].ID != "b" || got[3].Tab != "" {
		t.Fatal(got)
	}
}

func TestSelectITermCommandRejectsInvalid(t *testing.T) {
	for _, g := range []string{"", testGUID + "'; touch x; '", "w1t6p0:" + testGUID} {
		if got := selectITermCommand(g); got != "" {
			t.Fatalf("%q: %q", g, got)
		}
	}
	if selectITermCommand(testGUID) == "" {
		t.Fatal("valid GUID rejected")
	}
}
