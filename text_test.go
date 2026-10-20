package main

import (
	"strings"
	"testing"
)

func TestCleanText(t *testing.T) {
	for _, limit := range []int{120, 140, 200, 300} {
		if got := cleanText(strings.Repeat("я", 400), limit); len([]rune(got)) != limit {
			t.Fatalf("limit %d: %q", limit, got)
		}
	}
	if got := cleanText(" \tA\nB\rC\x1b\x07\u202e\u200b ", 0); got != "A B C" {
		t.Fatalf("clean: %q", got)
	}
	if got := sanitize(strings.Repeat("я", 150)); len([]rune(got)) != 120 {
		t.Fatal("protocol cap")
	}
}
