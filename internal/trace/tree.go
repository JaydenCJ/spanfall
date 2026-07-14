// Package trace groups flat spans into per-trace trees. It is defensive
// about the mess real trace files contain: orphaned spans whose parent was
// sampled away, duplicated span IDs, and even parent cycles all degrade to
// extra roots instead of infinite loops or dropped data.
package trace

import (
	"sort"

	"github.com/JaydenCJ/spanfall/internal/model"
)

// Node is a span with its resolved children, sorted by start time.
type Node struct {
	Span     *model.Span
	Children []*Node
}

// Trace is one trace ID's span tree plus precomputed summary facts.
type Trace struct {
	ID       string
	Roots    []*Node
	Start    int64 // min span start
	End      int64 // max span end
	Spans    int
	Errors   int
	Services []string // sorted unique
}

// Duration is the wall-clock envelope of the whole trace.
func (t *Trace) Duration() int64 {
	if t.End < t.Start {
		return 0
	}
	return t.End - t.Start
}

// RootName is the display name for the trace: the earliest root's span name.
func (t *Trace) RootName() string {
	if len(t.Roots) == 0 {
		return "(empty trace)"
	}
	return t.Roots[0].Span.Name
}

// Walk visits every node depth-first in render order (children by start
// time), calling fn with the node's depth. Returning false from fn prunes
// that node's subtree.
func (t *Trace) Walk(fn func(n *Node, depth int) bool) {
	var rec func(n *Node, depth int)
	rec = func(n *Node, depth int) {
		if !fn(n, depth) {
			return
		}
		for _, c := range n.Children {
			rec(c, depth+1)
		}
	}
	for _, r := range t.Roots {
		rec(r, 0)
	}
}

// Build groups spans by trace ID and assembles one tree per trace. Traces
// come back sorted by start time then ID, so output order is stable.
func Build(spans []*model.Span) []*Trace {
	byTrace := make(map[string][]*model.Span)
	var order []string
	for _, s := range spans {
		if _, seen := byTrace[s.TraceID]; !seen {
			order = append(order, s.TraceID)
		}
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
	}
	traces := make([]*Trace, 0, len(order))
	for _, id := range order {
		traces = append(traces, buildOne(id, byTrace[id]))
	}
	sort.SliceStable(traces, func(i, j int) bool {
		if traces[i].Start != traces[j].Start {
			return traces[i].Start < traces[j].Start
		}
		return traces[i].ID < traces[j].ID
	})
	return traces
}

func buildOne(id string, spans []*model.Span) *Trace {
	nodes := make([]*Node, len(spans))
	// First span wins the ID slot; a duplicated span ID still renders (its
	// node exists) but cannot be addressed as a parent twice.
	byID := make(map[string]*Node, len(spans))
	for i, s := range spans {
		nodes[i] = &Node{Span: s}
		if _, dup := byID[s.SpanID]; !dup {
			byID[s.SpanID] = nodes[i]
		}
	}

	t := &Trace{ID: id}
	services := make(map[string]bool)
	for _, n := range nodes {
		s := n.Span
		t.Spans++
		if s.IsError() {
			t.Errors++
		}
		services[s.Service] = true
		if t.Spans == 1 || s.Start < t.Start {
			t.Start = s.Start
		}
		if s.End > t.End {
			t.End = s.End
		}
	}
	for svc := range services {
		t.Services = append(t.Services, svc)
	}
	sort.Strings(t.Services)

	for _, n := range nodes {
		if isRoot(n, byID) {
			t.Roots = append(t.Roots, n)
			continue
		}
		parent := byID[n.Span.ParentID]
		parent.Children = append(parent.Children, n)
	}
	for _, n := range nodes {
		sortNodes(n.Children)
	}
	sortNodes(t.Roots)
	return t
}

// isRoot decides whether a node anchors the tree: no parent ID, a parent
// that is missing from the file (orphan), a self-parent, or membership in a
// parent cycle. Cycle members whose walk revisits themselves become roots,
// which breaks the cycle deterministically.
func isRoot(n *Node, byID map[string]*Node) bool {
	s := n.Span
	if s.ParentID == "" || s.ParentID == s.SpanID {
		return true
	}
	parent, ok := byID[s.ParentID]
	if !ok || parent == n {
		return true
	}
	seen := map[*Node]bool{n: true}
	for cur := parent; ; {
		if seen[cur] {
			return true // walked back into the chain: cycle
		}
		seen[cur] = true
		pid := cur.Span.ParentID
		if pid == "" || pid == cur.Span.SpanID {
			return false // chain terminates at a real root
		}
		next, ok := byID[pid]
		if !ok || next == cur {
			return false // chain terminates at an orphan root
		}
		cur = next
	}
}

func sortNodes(ns []*Node) {
	sort.SliceStable(ns, func(i, j int) bool {
		a, b := ns[i].Span, ns[j].Span
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		return a.SpanID < b.SpanID
	})
}
