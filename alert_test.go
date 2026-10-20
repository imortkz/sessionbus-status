package main

import (
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
		if len(got) != 2 || got[0].Kind != "sound" || got[1].Kind != "notify" || got[1].Title != "bus-status" || got[1].Body != want {
			t.Fatal(got)
		}
	}
	check(now, []AskInfo{old}, "old ждёт: question")
	check(now.Add(30*time.Second), []AskInfo{old}, "")
	check(now.Add(35*time.Second), []AskInfo{newer, old}, "new ждёт: new question")
	check(now.Add(95*time.Second), []AskInfo{newer, old}, "old ждёт: question")
	a.muted = true
	check(now.Add(155*time.Second), []AskInfo{old, newer}, "")
	a.muted = false
	check(now.Add(156*time.Second), []AskInfo{old, newer}, "")
	check(now.Add(157*time.Second), nil, "")
	check(now.Add(158*time.Second), []AskInfo{old}, "old ждёт: question")
	// Clearing one ID makes its reappearance new, even while another ask remains.
	check(now.Add(159*time.Second), []AskInfo{newer}, "new ждёт: new question")
	check(now.Add(160*time.Second), []AskInfo{newer, old}, "old ждёт: question")
	a = Alerts{}
	a.Tick(now, nil)
	got := a.Tick(now, []AskInfo{{ID: "z", Display: "z", Since: now}, {ID: "a", Display: "a", Since: now}})
	if len(got) != 4 || got[1].Body != "a ждёт:" || got[3].Body != "z ждёт:" {
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
	n := macNotifier{start: func(name string, args ...string) { calls = append(calls, append([]string{name}, args...)) }}
	body := `quote " & do shell script "bad"`
	n.Sound("sound with spaces.aiff")
	n.Notify("bus-status", body)
	want := [][]string{{"afplay", "sound with spaces.aiff"}, {"osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", "bus-status", body}}
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
	if got := a.Tick(now.Add(61*time.Second), asks); len(got) != 2 || got[1].Body != "new ждёт: question" {
		t.Fatal(got)
	}
}
