// Package stats aggregates spans into per-operation rows: how often each
// (service, operation) ran, how much wall time it took, and how much of
// that was self time — the part not covered by its own children, which is
// where an optimization would actually land.
package stats

import (
	"sort"

	"github.com/JaydenCJ/spanfall/internal/trace"
)

// Row is one (service, operation) aggregate.
type Row struct {
	Service string
	Name    string
	Count   int
	Errors  int
	Total   int64 // summed span durations
	Self    int64 // summed self times (duration minus child coverage)
	Max     int64 // longest single span
}

// Aggregate folds every span of every trace into rows, sorted by summed
// self time descending (the "where does time actually go" ordering), then
// total, then service/name for stability.
func Aggregate(traces []*trace.Trace) []Row {
	type key struct{ service, name string }
	acc := make(map[key]*Row)
	var order []key
	for _, t := range traces {
		t.Walk(func(n *trace.Node, depth int) bool {
			k := key{n.Span.Service, n.Span.Name}
			r, ok := acc[k]
			if !ok {
				r = &Row{Service: k.service, Name: k.name}
				acc[k] = r
				order = append(order, k)
			}
			d := n.Span.Duration()
			r.Count++
			r.Total += d
			if d > r.Max {
				r.Max = d
			}
			if n.Span.IsError() {
				r.Errors++
			}
			r.Self += SelfTime(n)
			return true
		})
	}
	rows := make([]Row, 0, len(order))
	for _, k := range order {
		rows = append(rows, *acc[k])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Self != rows[j].Self {
			return rows[i].Self > rows[j].Self
		}
		if rows[i].Total != rows[j].Total {
			return rows[i].Total > rows[j].Total
		}
		if rows[i].Service != rows[j].Service {
			return rows[i].Service < rows[j].Service
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

// SelfTime is the span's duration minus the union of its children's
// intervals (clamped to the span). Overlapping parallel children are only
// subtracted once, so self time never goes negative.
func SelfTime(n *trace.Node) int64 {
	s := n.Span
	d := s.Duration()
	if d == 0 || len(n.Children) == 0 {
		return d
	}
	type iv struct{ start, end int64 }
	ivs := make([]iv, 0, len(n.Children))
	for _, c := range n.Children {
		start := max64(c.Span.Start, s.Start)
		end := min64(c.Span.End, s.End)
		if end > start {
			ivs = append(ivs, iv{start, end})
		}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].start < ivs[j].start })
	var covered, curStart, curEnd int64
	first := true
	for _, v := range ivs {
		if first || v.start > curEnd {
			if !first {
				covered += curEnd - curStart
			}
			curStart, curEnd = v.start, v.end
			first = false
			continue
		}
		if v.end > curEnd {
			curEnd = v.end
		}
	}
	if !first {
		covered += curEnd - curStart
	}
	return d - covered
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
