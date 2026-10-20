package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseBody(t *testing.T) {
	got := ParseBody("ignored\n  STATUS: working\nmodel: gpt\nask: approve\nstatus: resumed\nask: \nunknown: no")
	want := []Update{{"status", "working"}, {"model", "gpt"}, {"ask", "approve"}, {"status", "resumed"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	rec := Apply(Record{}, got, time.Unix(100, 0))
	if rec.Status != "resumed" || rec.Ask != "" || rec.Model != "gpt" {
		t.Fatalf("ordered updates: %+v", rec)
	}
}
func TestApplyClearsAskAndSanitizes(t *testing.T) {
	now := time.Unix(100, 0)
	rec := Apply(Record{Ask: "old"}, []Update{{"status", "\x1b[31mhello\n\tworld\x7f\u0085"}, {"model", strings.Repeat("я", 200)}}, now)
	if rec.Status != "[31mhello  world" || rec.Ask != "" || len([]rune(rec.Model)) != 120 || !rec.UpdatedAt.Equal(now) {
		t.Fatalf("record: %+v", rec)
	}
	if got := Apply(rec, nil, now.Add(time.Hour)); !got.UpdatedAt.Equal(now) {
		t.Fatal("empty update changed age")
	}
}

func TestSanitizeDropsUnicodeFormatCharacters(t *testing.T) {
	text := "ab\u202Ecd\u200Bef\u2066gh\u2069"
	if got := sanitize(text); got != "abcdefgh" {
		t.Fatalf("format characters survived: %q", got)
	}
	updates := ParseBody("status: " + text)
	rec := Apply(Record{}, updates, time.Now())
	if rec.Status != "abcdefgh" {
		t.Fatalf("write sanitizer: %q", rec.Status)
	}
	report, state, now := fixture(t)
	state.Sessions["developer@macbook"] = Record{Status: text}
	if got := Render(report, state, now, false); !strings.Contains(got, "abcdefgh") || strings.ContainsAny(got, "\u202E\u200B\u2066\u2069") {
		t.Fatalf("render sanitizer: %q", got)
	}
}

func TestModelPreservesStatusAge(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	now := old.Add(time.Hour)
	rec := Apply(Record{Status: "old", UpdatedAt: old}, []Update{{Kind: "model", Text: "new-model"}}, now)
	if !rec.UpdatedAt.Equal(old) || rec.Model != "new-model" || len(rec.History) != 1 || !rec.History[0].At.Equal(now) {
		t.Fatal(rec)
	}
	rec = Apply(rec, []Update{{Kind: "ask", Text: "question"}}, now)
	if !rec.UpdatedAt.Equal(now) {
		t.Fatal(rec)
	}
	rec = Apply(rec, []Update{{Kind: "status", Text: "done"}}, now.Add(time.Second))
	if !rec.UpdatedAt.Equal(now.Add(time.Second)) {
		t.Fatal(rec)
	}
}
