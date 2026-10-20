package main

import (
	"strings"
	"unicode"
)

func cleanText(text string, max int) string {
	text = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	runes := []rune(strings.TrimSpace(text))
	if max > 0 && len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}
