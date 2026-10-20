package main

import (
	"errors"
	"testing"
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
	state.Sessions["developer@macbook"] = Record{Ask: "second question"}
	bell, _ = nextBell(prev, report, nil, state)
	if !bell {
		t.Fatal("new ask did not ring")
	}
}
