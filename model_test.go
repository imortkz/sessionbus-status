package main

import (
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
	trees := BuildTree(r, s, now)
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
	if roots[0].Display != "same · aaaaaaaa" || roots[1].Display != "same · bbbbbbbb" || !roots[0].Stale || roots[1].Stale {
		t.Fatal(roots[0], roots[1])
	}
	if len(roots[0].Children) != 1 || !roots[0].Children[0].Ask || roots[0].Children[0].Line != "ЖДЁТ: approve" {
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
