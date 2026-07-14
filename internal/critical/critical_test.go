// Tests for the critical-path engine. Each case is a small hand-computable
// trace; the assertions pin both which spans are on the path and exactly
// how much self time each one owns, because that arithmetic is the tool's
// central claim.
package critical

import (
	"testing"

	"github.com/JaydenCJ/spanfall/internal/model"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

func sp(id, parent, name string, start, end int64) *model.Span {
	return &model.Span{
		TraceID: "t1", SpanID: id, ParentID: parent, Name: name,
		Service: "svc", Start: start, End: end, Status: model.StatusUnset,
	}
}

func compute(t *testing.T, spans ...*model.Span) (*trace.Trace, *Path) {
	t.Helper()
	traces := trace.Build(spans)
	if len(traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(traces))
	}
	return traces[0], Compute(traces[0])
}

// selfOf finds the accumulated self time for the span with the given ID.
func selfOf(p *Path, id string) int64 {
	for s, v := range p.Self {
		if s.SpanID == id {
			return v
		}
	}
	return -1
}

func TestSingleSpanOwnsEverything(t *testing.T) {
	_, p := compute(t, sp("a", "", "root", 0, 100))
	if got := selfOf(p, "a"); got != 100 {
		t.Fatalf("self: %d", got)
	}
	if len(p.Order) != 1 {
		t.Fatalf("order: %d", len(p.Order))
	}
}

func TestSequentialChildrenAllOnPath(t *testing.T) {
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "first", 10, 40),
		sp("c", "a", "second", 50, 90),
	)
	// Root owns the gaps: [0,10) + [40,50) + [90,100) = 30.
	if got := selfOf(p, "a"); got != 30 {
		t.Fatalf("root self: %d", got)
	}
	if selfOf(p, "b") != 30 || selfOf(p, "c") != 40 {
		t.Fatalf("children self: b=%d c=%d", selfOf(p, "b"), selfOf(p, "c"))
	}
}

func TestParallelChildrenOnlyLastFinishingOnPath(t *testing.T) {
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "fast", 10, 60),
		sp("c", "a", "slow", 10, 95),
	)
	if !p.OnPath(findSpan(p, "c")) {
		t.Fatal("slow child must be on the path")
	}
	if got := selfOf(p, "b"); got != -1 {
		t.Fatalf("fast parallel child must be off the path, got self=%d", got)
	}
	// Root: [0,10) before the children plus [95,100) after = 15.
	if got := selfOf(p, "a"); got != 15 {
		t.Fatalf("root self: %d", got)
	}
}

func TestOverlappingChildTakesOverWhenLongerOneEnds(t *testing.T) {
	// b runs [10,50), c runs [30,90): the path is c back to 30, then b
	// covers [10,30) because it was the last thing running before that.
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "early", 10, 50),
		sp("c", "a", "late", 30, 90),
	)
	if selfOf(p, "c") != 60 {
		t.Fatalf("late self: %d", selfOf(p, "c"))
	}
	if selfOf(p, "b") != 20 {
		t.Fatalf("early self should be its pre-overlap slice: %d", selfOf(p, "b"))
	}
	if selfOf(p, "a") != 20 { // [0,10) + [90,100)
		t.Fatalf("root self: %d", selfOf(p, "a"))
	}
}

func TestGrandchildrenDescend(t *testing.T) {
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "mid", 10, 90),
		sp("c", "b", "leaf", 20, 80),
	)
	if selfOf(p, "a") != 20 || selfOf(p, "b") != 20 || selfOf(p, "c") != 60 {
		t.Fatalf("selves: a=%d b=%d c=%d", selfOf(p, "a"), selfOf(p, "b"), selfOf(p, "c"))
	}
}

func TestChildOverflowingParentIsClamped(t *testing.T) {
	// Async work that outlives its parent must not push the path outside
	// the parent's window (clock skew and fire-and-forget both do this).
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "async", 50, 200),
	)
	// Trace envelope is [0,200): b owns [50,200) = 150, a owns [0,50).
	if selfOf(p, "b") != 150 {
		t.Fatalf("async self: %d", selfOf(p, "b"))
	}
	if selfOf(p, "a") != 50 {
		t.Fatalf("root self: %d", selfOf(p, "a"))
	}
}

func TestZeroDurationSpansNeverOnPath(t *testing.T) {
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "instant", 50, 50),
	)
	if got := selfOf(p, "b"); got != -1 {
		t.Fatalf("zero-duration span on path with self=%d", got)
	}
	if selfOf(p, "a") != 100 {
		t.Fatalf("root self: %d", selfOf(p, "a"))
	}
}

func TestSegmentsTileTheTraceExactly(t *testing.T) {
	tr, p := compute(t,
		sp("a", "", "root", 0, 1000),
		sp("b", "a", "one", 100, 400),
		sp("c", "a", "two", 350, 900),
		sp("d", "c", "leaf", 400, 800),
	)
	var covered int64
	prevEnd := tr.Start
	for _, seg := range p.Segments {
		if seg.Start != prevEnd {
			t.Fatalf("segments not contiguous at %d (got start %d)", prevEnd, seg.Start)
		}
		if seg.End <= seg.Start {
			t.Fatalf("empty segment %+v", seg)
		}
		covered += seg.Duration()
		prevEnd = seg.End
	}
	if covered != tr.Duration() || prevEnd != tr.End {
		t.Fatalf("segments cover %d of %d", covered, tr.Duration())
	}
}

func TestSelfTimesSumToTraceDuration(t *testing.T) {
	tr, p := compute(t,
		sp("a", "", "root", 0, 500),
		sp("b", "a", "x", 50, 200),
		sp("c", "a", "y", 180, 480),
		sp("d", "b", "z", 60, 190),
	)
	var total int64
	for _, v := range p.Self {
		total += v
	}
	if total+p.GapTime() != tr.Duration() {
		t.Fatalf("self sum %d + gaps %d != trace %d", total, p.GapTime(), tr.Duration())
	}
}

func TestMultipleRootsProduceGapSegments(t *testing.T) {
	// Two roots with dead air between them (broken instrumentation):
	// the uncovered middle must be reported as a gap, not invented.
	tr, p := compute(t,
		sp("a", "", "first", 0, 100),
		sp("b", "missing", "second", 300, 500),
	)
	if tr.Duration() != 500 {
		t.Fatalf("envelope: %d", tr.Duration())
	}
	if p.GapTime() != 200 {
		t.Fatalf("gap: %d", p.GapTime())
	}
	if selfOf(p, "a") != 100 || selfOf(p, "b") != 200 {
		t.Fatalf("selves: a=%d b=%d", selfOf(p, "a"), selfOf(p, "b"))
	}
}

func TestOrderFollowsTimeline(t *testing.T) {
	_, p := compute(t,
		sp("a", "", "root", 0, 100),
		sp("b", "a", "first", 10, 40),
		sp("c", "a", "second", 50, 90),
	)
	if len(p.Order) != 3 {
		t.Fatalf("order size: %d", len(p.Order))
	}
	if p.Order[0].SpanID != "a" || p.Order[1].SpanID != "b" || p.Order[2].SpanID != "c" {
		t.Fatalf("order: %s %s %s", p.Order[0].SpanID, p.Order[1].SpanID, p.Order[2].SpanID)
	}
}

func TestEmptyTraceYieldsEmptyPath(t *testing.T) {
	_, p := compute(t, sp("a", "", "instant root", 42, 42))
	if len(p.Segments) != 0 || len(p.Order) != 0 {
		t.Fatalf("zero-duration trace must yield empty path: %+v", p.Segments)
	}
}

func findSpan(p *Path, id string) *model.Span {
	for s := range p.Self {
		if s.SpanID == id {
			return s
		}
	}
	return &model.Span{SpanID: id}
}
