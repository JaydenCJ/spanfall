// Package critical computes the critical path of a trace: the chain of
// span segments that actually determined end-to-end latency. Shortening any
// segment on the path shortens the trace; shortening anything off it does
// not. The algorithm is the classic "last finishing child" walk used by
// trace analyzers, extended to follow async children that outlive their
// parent instead of writing that tail off as unexplained time.
package critical

import (
	"github.com/JaydenCJ/spanfall/internal/model"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

// Segment is a half-open time slice [Start, End) attributed to one span.
// Span is nil for gaps where no span was active (possible between multiple
// roots of a broken trace).
type Segment struct {
	Span  *model.Span
	Start int64
	End   int64
}

// Duration returns the segment length in nanoseconds.
func (s Segment) Duration() int64 { return s.End - s.Start }

// Path is the computed critical path for one trace.
type Path struct {
	// Segments tile the trace envelope exactly, ordered start to end.
	Segments []Segment
	// Self sums each span's segment time; only on-path spans appear.
	Self map[*model.Span]int64
	// Order lists on-path spans by first appearance along the timeline.
	Order []*model.Span
}

// OnPath reports whether the span owns any critical-path time.
func (p *Path) OnPath(s *model.Span) bool {
	_, ok := p.Self[s]
	return ok
}

// GapTime is the total time attributed to no span at all.
func (p *Path) GapTime() int64 {
	var total int64
	for _, seg := range p.Segments {
		if seg.Span == nil {
			total += seg.Duration()
		}
	}
	return total
}

// Compute walks the trace from its envelope down. Multiple roots are
// handled by treating them as children of a virtual span covering the whole
// envelope; time no root's subtree covers shows up as nil-span gap segments.
func Compute(t *trace.Trace) *Path {
	p := &Path{Self: make(map[*model.Span]int64)}
	if t.Duration() <= 0 {
		return p
	}
	w := walker{ends: make(map[*trace.Node]int64)}
	w.descend(nil, t.Roots, t.Start, t.End, &p.Segments)
	// Segments were collected walking backwards from the trace end.
	reverse(p.Segments)
	for _, seg := range p.Segments {
		if seg.Span == nil {
			continue
		}
		if _, seen := p.Self[seg.Span]; !seen {
			p.Order = append(p.Order, seg.Span)
		}
		p.Self[seg.Span] += seg.Duration()
	}
	return p
}

// walker carries the memoized subtree-end table through the recursion.
type walker struct {
	ends map[*trace.Node]int64
}

// descend attributes the window [from, to) to sp and its children,
// appending segments in reverse time order. The cursor sweeps from the
// window's end toward its start; at every step the child whose subtree
// finishes last inside the remaining window is the one the trace was
// waiting on, so the path descends into it. Using the subtree end (not the
// span's own end) means async children that outlive their parent are still
// charged for the tail latency they cause.
func (w *walker) descend(sp *model.Span, children []*trace.Node, from, to int64, out *[]Segment) {
	cursor := to
	for cursor > from {
		best, bs, be := w.lastFinishing(children, from, cursor)
		if best == nil {
			// Nothing was running: the remainder is the span's own time.
			*out = append(*out, Segment{Span: sp, Start: from, End: cursor})
			return
		}
		if be < cursor {
			// The parent kept working (or waiting) after its last child.
			*out = append(*out, Segment{Span: sp, Start: be, End: cursor})
		}
		w.descend(best.Span, best.Children, bs, be, out)
		cursor = bs
	}
}

// lastFinishing picks the child whose clamped subtree end is latest inside
// [from, cursor). Ties break toward the later start (the tighter span),
// then the larger span ID, keeping the choice fully deterministic.
func (w *walker) lastFinishing(children []*trace.Node, from, cursor int64) (best *trace.Node, bs, be int64) {
	for _, c := range children {
		cs := max64(c.Span.Start, from)
		ce := min64(w.subtreeEnd(c), cursor)
		if ce <= cs {
			continue // zero-length inside the window, or no overlap
		}
		if best == nil || ce > be || (ce == be && cs > bs) ||
			(ce == be && cs == bs && c.Span.SpanID > best.Span.SpanID) {
			best, bs, be = c, cs, ce
		}
	}
	return best, bs, be
}

// subtreeEnd is the latest end time in a node's whole subtree, memoized.
func (w *walker) subtreeEnd(n *trace.Node) int64 {
	if v, ok := w.ends[n]; ok {
		return v
	}
	end := n.Span.End
	for _, c := range n.Children {
		if e := w.subtreeEnd(c); e > end {
			end = e
		}
	}
	w.ends[n] = end
	return end
}

func reverse(segs []Segment) {
	for i, j := 0, len(segs)-1; i < j; i, j = i+1, j-1 {
		segs[i], segs[j] = segs[j], segs[i]
	}
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
