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
