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
	state.Sessions["developer@laptop"] = Record{Status: text}
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

func TestApplyTab(t *testing.T) {
	const guid = "0EB11E86-12BA-4739-B792-1F3699034011"
	t0 := time.Unix(100, 0)
	for _, v := range []string{guid, "w1t6p0:" + guid} {
		rec := Apply(Record{}, ParseBody("tab: "+v), t0)
		if rec.Tab != guid || len(rec.History) != 0 || !rec.UpdatedAt.IsZero() {
			t.Fatalf("%q: %+v", v, rec)
		}
	}
	base := Record{Tab: guid, UpdatedAt: t0}
	for _, v := range []string{guid + "'", "w1t6p0:" + guid + "$(x)", "a b", guid + "x", guid[:35], "'" + guid, "w1t6p0:", ""} {
		rec := Apply(base, ParseBody("tab: "+v), t0.Add(time.Hour))
		if rec.Tab != guid || len(rec.History) != 0 || !rec.UpdatedAt.Equal(t0) {
			t.Fatalf("%q: %+v", v, rec)
		}
	}
	rec := Apply(base, ParseBody("status: working"), t0.Add(time.Hour))
	if rec.Tab != guid || rec.Status != "working" {
		t.Fatalf("status cleared tab: %+v", rec)
	}
	for _, e := range rec.History {
		if e.Kind == "tab" {
			t.Fatal("tab in history")
		}
	}
}

func TestApplyName(t *testing.T) {
	t0 := time.Unix(100, 0)
	rec := Apply(Record{UpdatedAt: t0}, ParseBody("name: bus-status"), t0.Add(time.Hour))
	if rec.Name != "bus-status" || len(rec.History) != 0 || !rec.UpdatedAt.Equal(t0) {
		t.Fatalf("%+v", rec)
	}
	rec = Apply(rec, []Update{{"name", strings.Repeat("я", 100)}}, t0)
	if len([]rune(rec.Name)) != 60 {
		t.Fatalf("not truncated: %d", len([]rune(rec.Name)))
	}
	rec = Apply(rec, ParseBody("name: \nstatus: x"), t0)
	if len([]rune(rec.Name)) != 60 || rec.Status != "x" {
		t.Fatalf("%+v", rec)
	}
}
