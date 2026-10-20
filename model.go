package main

import (
	"sort"
	"strings"
	"time"
)

type HostTree struct {
	Host  string
	Error string
	Local bool
	Nodes []*Node
}

const noStatus = "нет статуса"

type Activity string

const (
	ActivityAsk   Activity = "ask"
	ActivityLive  Activity = "live"
	ActivityLanes Activity = "lanes"
	ActivityIdle  Activity = "idle"
)

type Node struct {
	Activity                          Activity
	ID, Display, Harness, Model, Line string
	Name                              string
	Ask, Running, Stale               bool
	Age                               string
	Children                          []*Node
	HasAskBelow                       bool
}

func shortID(id string) string {
	id, _, _ = strings.Cut(id, "@")
	r := []rune(cleanText(id, 0))
	if len(r) > 8 {
		r = r[:8]
	}
	return string(r)
}
func BuildTree(report Report, state State, now time.Time, activity map[string]time.Time) []HostTree {
	var trees []HostTree
	for hi, host := range append([]Host{report.Local}, report.Remote...) {
		tree := HostTree{Host: cleanText(host.Host, 120), Error: cleanText(host.Error, 200), Local: hi == 0}
		if host.Error != "" {
			trees = append(trees, tree)
			continue
		}
		rows := map[string]Row{}
		names := map[string]int{}
		for _, row := range host.Sessions {
			if row.Product != "bus-status" {
				rows[row.SessionID] = row
				if n := cleanText(row.Name, 120); n != "" {
					names[n]++
				}
			}
		}
		children := map[string][]string{}
		var roots, all []string
		for id, row := range rows {
			all = append(all, id)
			if _, ok := rows[row.Owner]; row.Owner == "" || !ok {
				roots = append(roots, id)
			} else {
				children[row.Owner] = append(children[row.Owner], id)
			}
		}
		var hasAsk func(string, map[string]bool) bool
		hasAsk = func(id string, seen map[string]bool) bool {
			if seen[id] {
				return false
			}
			seen[id] = true
			if cleanText(state.Sessions[id].Ask, 120) != "" {
				return true
			}
			for _, c := range children[id] {
				if hasAsk(c, seen) {
					return true
				}
			}
			return false
		}
		var runningLaneBelow func(string, map[string]bool) bool
		runningLaneBelow = func(id string, seen map[string]bool) bool {
			if seen[id] {
				return false
			}
			seen[id] = true
			for _, child := range children[id] {
				if rows[child].Kind != "peer" && rows[child].Running {
					return true
				}
				if runningLaneBelow(child, seen) {
					return true
				}
			}
			return false
		}
		nodes := map[string]*Node{}
		for id, row := range rows {
			rec := state.Sessions[id]
			h := strings.TrimSuffix(cleanText(row.Product, 120), "-peer")
			if row.Product == "sessionbus-dsh" {
				h = "dsh"
			}
			display := cleanText(row.Name, 120)
			if display == "" {
				display = h + " · " + shortID(id)
			} else if names[display] > 1 {
				display += " · " + shortID(id)
			}
			line := cleanText(rec.Status, 120)
			ask := cleanText(rec.Ask, 120) != ""
			if ask {
				line = "ЖДЁТ: " + cleanText(rec.Ask, 120)
			}
			if line == "" {
				line = noStatus
			}
			n := &Node{Name: cleanText(row.Name, 120), ID: id, Display: display, Harness: h, Model: cleanText(rec.Model, 120), Line: line, Ask: ask, Running: row.Running, HasAskBelow: hasAsk(id, map[string]bool{})}
			live := row.Running
			lanes := false
			if row.Kind == "peer" {
				at := activity[id]
				live = tree.Local && !at.IsZero() && !at.After(now) && now.Sub(at) <= 30*time.Second
				lanes = runningLaneBelow(id, map[string]bool{})
			}
			n.Activity = ActivityIdle
			switch {
			case ask:
				n.Activity = ActivityAsk
			case live:
				n.Activity = ActivityLive
			case lanes:
				n.Activity = ActivityLanes
			}
			if !rec.UpdatedAt.IsZero() {
				n.Age = age(now.Sub(rec.UpdatedAt))
				n.Stale = !live && !lanes && now.Sub(rec.UpdatedAt) > 10*time.Minute
			}
			nodes[id] = n
		}
		sortIDs := func(ids []string) {
			sort.Slice(ids, func(i, j int) bool {
				a, b := nodes[ids[i]], nodes[ids[j]]
				if a.HasAskBelow != b.HasAskBelow {
					return a.HasAskBelow
				}
				if a.Running != b.Running {
					return a.Running
				}
				if a.Display != b.Display {
					return a.Display < b.Display
				}
				return a.ID < b.ID
			})
		}
		visited := map[string]bool{}
		var build func(string) *Node
		build = func(id string) *Node {
			if visited[id] {
				return nil
			}
			visited[id] = true
			n := nodes[id]
			sortIDs(children[id])
			for _, c := range children[id] {
				if child := build(c); child != nil {
					n.Children = append(n.Children, child)
				}
			}
			return n
		}
		sortIDs(roots)
		for _, id := range roots {
			if n := build(id); n != nil {
				tree.Nodes = append(tree.Nodes, n)
			}
		}
		sortIDs(all)
		for _, id := range all {
			if n := build(id); n != nil {
				tree.Nodes = append(tree.Nodes, n)
			}
		}
		trees = append(trees, tree)
	}
	return trees
}

func (n *Node) Icon() string {
	switch n.Activity {
	case ActivityAsk:
		return "⚠"
	case ActivityLive:
		return "●"
	case ActivityLanes:
		return "◐"
	default:
		return "○"
	}
}
