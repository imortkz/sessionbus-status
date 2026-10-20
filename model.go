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
	Lane                              bool
	Ask, Running                      bool
	Age                               string
	Children                          []*Node
	HasAskBelow                       bool
	HiddenDuplicate                   bool
}

func shortID(id string) string {
	id, _, _ = strings.Cut(id, "@")
	r := []rune(cleanText(id, 0))
	if len(r) > 8 {
		r = r[:8]
	}
	return string(r)
}
func BuildTree(report Report, state State, now time.Time, activity, firstSeen map[string]time.Time) []HostTree {
	var trees []HostTree
	for hi, host := range append([]Host{report.Local}, report.Remote...) {
		tree := HostTree{Host: cleanText(host.Host, 120), Error: cleanText(host.Error, 200), Local: hi == 0}
		if host.Error != "" {
			trees = append(trees, tree)
			continue
		}
		rows := map[string]Row{}
		for _, row := range host.Sessions {
			if row.Product != collectorProduct {
				rows[row.SessionID] = row
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
			}
			line := cleanText(rec.Status, 120)
			ask := cleanText(rec.Ask, 120) != ""
			if ask {
				line = uiWaiting + cleanText(rec.Ask, 120)
			}
			if line == "" {
				line = noStatus
			}
			n := &Node{Lane: row.Kind != "peer", Name: cleanText(row.Name, 120), ID: id, Display: display, Harness: h, Model: cleanText(rec.Model, 120), Line: line, Ask: ask, Running: row.Running, HasAskBelow: hasAsk(id, map[string]bool{})}
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
			}
			nodes[id] = n
		}
		for id, n := range nodes {
			if rows[id].Kind != "peer" || n.Name == "" || len(children[id]) > 0 || n.Activity != ActivityIdle {
				continue
			}
			rec := state.Sessions[id]
			absent := rec.Status == "" || rec.UpdatedAt.IsZero()
			for otherID, other := range nodes {
				if otherID == id || n.Name != other.Name {
					continue
				}
				if rows[id].Connected != rows[otherID].Connected {
					if !rows[id].Connected {
						n.HiddenDuplicate = true
						break
					}
					continue
				}
				if a, aok := firstSeen[id]; aok {
					if b, bok := firstSeen[otherID]; bok && !a.Equal(b) {
						if a.Before(b) {
							n.HiddenDuplicate = true
							break
						}
						continue
					}
				}
				otherRec := state.Sessions[otherID]
				otherAbsent := otherRec.Status == "" || otherRec.UpdatedAt.IsZero()
				if (!otherAbsent && (absent || rec.UpdatedAt.Before(otherRec.UpdatedAt))) ||
					(absent && otherAbsent && (other.Activity != ActivityIdle || len(children[otherID]) > 0 || id > otherID)) {
					n.HiddenDuplicate = true
					break
				}
			}
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
	applyDisplayNames(trees, false)
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

func hiddenDuplicates(trees []HostTree) int {
	count := 0
	var walk func(*Node)
	walk = func(n *Node) {
		if n.HiddenDuplicate {
			count++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, h := range trees {
		for _, n := range h.Nodes {
			walk(n)
		}
	}
	return count
}

// Count duplicate names within each host after applying the chosen visibility.
func applyDisplayNames(trees []HostTree, showAll bool) {
	for _, h := range trees {
		counts := map[string]int{}
		var nodes []*Node
		var collect func(*Node)
		collect = func(n *Node) {
			nodes = append(nodes, n)
			if n.Name != "" && (showAll || !n.HiddenDuplicate) {
				counts[n.Name]++
			}
			for _, c := range n.Children {
				collect(c)
			}
		}
		for _, n := range h.Nodes {
			collect(n)
		}
		for _, n := range nodes {
			n.Display = n.Name
			if n.Name == "" {
				n.Display = n.Harness + " · " + shortID(n.ID)
			} else if (showAll || !n.HiddenDuplicate) && counts[n.Name] > 1 {
				n.Display += " · " + shortID(n.ID)
			}
		}
	}
}
