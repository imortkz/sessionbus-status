package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBellPreservesAsksAcrossFailedTick(t *testing.T) {
	report, state, _ := fixture(t)
	bell, prev := nextBell(nil, report, nil, state)
	if !bell {
		t.Fatal("first ask did not ring")
	}
	bell, prev = nextBell(prev, Report{}, errors.New("invalid roster"), state)
	if bell {
		t.Fatal("failed roster tick rang")
	}
	bell, prev = nextBell(prev, report, nil, state)
	if bell {
		t.Fatal("unchanged ask rang again after failed tick")
	}
	state.Sessions["developer@laptop"] = Record{Ask: "second question"}
	bell, _ = nextBell(prev, report, nil, state)
	if !bell {
		t.Fatal("new ask did not ring")
	}
}

func TestRosterTimeout(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	saved := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = saved }()
	start := time.Now()
	code := runWatch([]string{"-once", "-roster", "sleep 5", "-roster-timeout", "200ms", "-state", t.TempDir() + "/missing"})
	elapsed := time.Since(start)
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 || !strings.Contains(string(data), strings.TrimSpace(uiRosterError)) || !strings.Contains(string(data), "deadline exceeded") {
		t.Fatalf("code %d output %q", code, data)
	}
	if elapsed < 150*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("deadline took %s", elapsed)
	}
}
