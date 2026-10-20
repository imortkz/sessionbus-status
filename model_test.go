package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildTree(t *testing.T) {
	now := time.Now()
	r := Report{Local: Host{Host: "local", Sessions: []Row{
		{SessionID: "aaaaaaaa-1", Name: "same", Product: "claude-peer"},
		{SessionID: "bbbbbbbb-2", Name: "same", Product: "codex-peer", Running: true},
		{SessionID: "child", Name: "kid", Owner: "aaaaaaaa-1", Product: "codex-peer"},
		{SessionID: "orphanid-1", Owner: "gone", Product: "sessionbus-dsh"},
		{SessionID: "hide", Product: "bus-status"},
		{SessionID: "cycle-a", Owner: "cycle-b", Product: "claude-peer"},
		{SessionID: "cycle-b", Owner: "cycle-a", Product: "claude-peer"},
	}}}
	s := State{Sessions: map[string]Record{"child": {Ask: "approve"}, "aaaaaaaa-1": {Status: "old", UpdatedAt: now.Add(-11 * time.Minute)}, "bbbbbbbb-2": {Status: "old", UpdatedAt: now.Add(-11 * time.Minute)}}}
	trees := BuildTree(r, s, now, nil, nil)
	if len(trees) != 1 || !trees[0].Local {
		t.Fatal(trees)
	}
	roots := trees[0].Nodes
	if len(roots) != 4 {
		t.Fatalf("roots %+v", roots)
	}
	if roots[0].ID != "aaaaaaaa-1" || !roots[0].HasAskBelow || roots[1].ID != "bbbbbbbb-2" {
		t.Fatal(roots)
	}
	if roots[0].Display != "same · aaaaaaaa" || roots[1].Display != "same · bbbbbbbb" || roots[0].Age != "11m" || roots[1].Age != "11m" {
		t.Fatal(roots[0], roots[1])
	}
	if len(roots[0].Children) != 1 || !roots[0].Children[0].Ask || roots[0].Children[0].Line != uiWaiting+"approve" {
		t.Fatal(roots[0])
	}
	var ids []string
	var walk func(*Node)
	walk = func(n *Node) {
		ids = append(ids, n.ID)
		if n.ID == "orphanid-1" && n.Display != "dsh · orphanid" {
			t.Fatal(n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, n := range roots {
		walk(n)
	}
	if len(ids) != 6 || strings.Contains(strings.Join(ids, ","), "hide") {
		t.Fatal(ids)
	}
}

func TestPeerActivity(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name                         string
		ask, ownRunning, laneRunning bool
		mtime                        time.Time
		want                         Activity
	}{
		{name: "running descendant", laneRunning: true, want: ActivityLanes},
		{name: "fresh own log", laneRunning: true, mtime: now.Add(-30 * time.Second), want: ActivityLive},
		{name: "ask wins", ask: true, laneRunning: true, mtime: now, want: ActivityAsk},
		{name: "idle lanes", want: ActivityIdle},
		{name: "old log", mtime: now.Add(-31 * time.Second), want: ActivityIdle},
		{name: "peer running ignored", ownRunning: true, want: ActivityIdle},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Report{Local: Host{Host: "local", Sessions: []Row{
				{SessionID: "peer", Name: "peer", Kind: "peer", Product: "claude-peer", Running: tt.ownRunning},
				{SessionID: "child", Name: "child", Kind: "lane", Owner: "peer"},
				{SessionID: "grandchild", Name: "grandchild", Kind: "lane", Owner: "child", Running: tt.laneRunning},
			}}}
			s := State{Sessions: map[string]Record{"peer": {Status: "old", UpdatedAt: now.Add(-time.Hour)}}}
			if tt.ask {
				rec := s.Sessions["peer"]
				rec.Ask = "question"
				s.Sessions["peer"] = rec
			}
			n := BuildTree(r, s, now, map[string]time.Time{"peer": tt.mtime}, nil)[0].Nodes[0]
			if n.Activity != tt.want || n.Age != "1h" {
				t.Fatalf("activity=%s age=%s", n.Activity, n.Age)
			}
			if tt.laneRunning && n.Children[0].Children[0].Activity != ActivityLive {
				t.Fatal("lane running rule changed")
			}
			r.Remote = []Host{{Host: "remote", Sessions: r.Local.Sessions}}
			r.Local.Sessions = nil
			n = BuildTree(r, s, now, map[string]time.Time{"peer": now}, nil)[1].Nodes[0]
			if !tt.ask && !tt.laneRunning && n.Activity != ActivityIdle {
				t.Fatal("remote mtime used")
			}
		})
	}
}

func TestHiddenDuplicatePeers(t *testing.T) {
	now := time.Now()
	r := Report{Local: Host{Host: "local", Sessions: []Row{{SessionID: "old", Name: "same", Kind: "peer"}, {SessionID: "new", Name: "same", Kind: "peer"}}}}
	s := State{Sessions: map[string]Record{"old": {Status: "old", UpdatedAt: now.Add(-time.Hour)}, "new": {Status: "new", UpdatedAt: now}}}
	nodes := BuildTree(r, s, now, nil, nil)[0].Nodes
	byID := map[string]*Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if !byID["old"].HiddenDuplicate || byID["new"].HiddenDuplicate {
		t.Fatal(nodes)
	}
	if got := Render(r, s, now, false); !strings.Contains(got, fmt.Sprintf(uiHidden, 1)) || strings.Contains(got, "same · old") {
		t.Fatal(got)
	}
	for _, activity := range []string{"ask", "live", "lanes"} {
		t.Run(activity, func(t *testing.T) {
			copyState := State{Sessions: map[string]Record{"old": s.Sessions["old"], "new": s.Sessions["new"]}}
			copyReport := r
			copyReport.Local.Sessions = append([]Row(nil), r.Local.Sessions...)
			mtime := map[string]time.Time{}
			switch activity {
			case "ask":
				rec := copyState.Sessions["old"]
				rec.Ask = "question"
				copyState.Sessions["old"] = rec
			case "live":
				mtime["old"] = now
			case "lanes":
				copyReport.Local.Sessions = append(copyReport.Local.Sessions, Row{SessionID: "child", Owner: "old", Kind: "lane", Running: true})
			}
			for _, n := range BuildTree(copyReport, copyState, now, mtime, nil)[0].Nodes {
				if n.ID == "old" && n.HiddenDuplicate {
					t.Fatal("active duplicate hidden", activity)
				}
			}
		})
	}
	r.Remote = []Host{{Host: "remote", Sessions: []Row{{SessionID: "remote", Name: "same", Kind: "peer"}}}}
	if BuildTree(r, s, now, nil, nil)[1].Nodes[0].HiddenDuplicate {
		t.Fatal("cross-host duplicate hidden")
	}
}

func TestNodeHasNoStaleFlag(t *testing.T) {
	if _, exists := reflect.TypeOf(Node{}).FieldByName("Stale"); exists {
		t.Fatal("dead Stale field remains")
	}
}

func hiddenByID(t *testing.T, r Report, s State, now time.Time, firstSeen map[string]time.Time) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, n := range BuildTree(r, s, now, nil, firstSeen)[0].Nodes {
		out[n.ID] = n.HiddenDuplicate
	}
	return out
}

func TestHideOlderTwinFirstSeen(t *testing.T) {
	now := time.Now()
	r := Report{Local: Host{Host: "local", Sessions: []Row{
		{SessionID: "old", Name: "same", Kind: "peer", Connected: true},
		{SessionID: "new", Name: "same", Kind: "peer", Connected: true},
	}}}
	s := State{Sessions: map[string]Record{"old": {Status: "working", UpdatedAt: now.Add(-time.Hour)}}}
	got := hiddenByID(t, r, s, now, map[string]time.Time{"old": now.Add(-time.Hour), "new": now.Add(-time.Minute)})
	if !got["old"] || got["new"] {
		t.Fatalf("newer silent twin must replace older one with status: %v", got)
	}
	got = hiddenByID(t, r, s, now, map[string]time.Time{"old": now, "new": now})
	if got["old"] || !got["new"] {
		t.Fatalf("equal first-seen must use the status rule: %v", got)
	}
	got = hiddenByID(t, r, s, now, nil)
	if got["old"] || !got["new"] {
		t.Fatalf("nil first-seen must use the status rule: %v", got)
	}
}

func TestHideOlderTwinDisconnected(t *testing.T) {
	now := time.Now()
	r := Report{Local: Host{Host: "local", Sessions: []Row{
		{SessionID: "old", Name: "same", Kind: "peer"},
		{SessionID: "new", Name: "same", Kind: "peer", Connected: true},
	}}}
	s := State{Sessions: map[string]Record{"old": {Status: "working", UpdatedAt: now}}}
	got := hiddenByID(t, r, s, now, nil)
	if !got["old"] || got["new"] {
		t.Fatalf("disconnected idle twin must be hidden: %v", got)
	}
}

func TestHideOlderTwinKeepsActive(t *testing.T) {
	now := time.Now()
	firstSeen := map[string]time.Time{"old": now.Add(-time.Hour), "new": now}
	base := func() Report {
		return Report{Local: Host{Host: "local", Sessions: []Row{
			{SessionID: "old", Name: "same", Kind: "peer", Connected: true},
			{SessionID: "new", Name: "same", Kind: "peer", Connected: true},
		}}}
	}
	ask := State{Sessions: map[string]Record{"old": {Ask: "q", UpdatedAt: now}}}
	if hiddenByID(t, base(), ask, now, firstSeen)["old"] {
		t.Fatal("asking peer hidden")
	}
	withLane := base()
	withLane.Local.Sessions = append(withLane.Local.Sessions, Row{SessionID: "child", Owner: "old", Kind: "lane", Running: true})
	if hiddenByID(t, withLane, State{}, now, firstSeen)["old"] {
		t.Fatal("peer with lanes hidden")
	}
}
