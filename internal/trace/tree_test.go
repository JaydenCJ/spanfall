// Tests for tree assembly from flat spans, focused on the degenerate
// shapes real incident files contain: orphans, duplicate IDs, self-parents,
// and outright parent cycles. None of them may drop data or hang.
package trace

import (
	"testing"

	"github.com/JaydenCJ/spanfall/internal/model"
)

// sp is the span factory used across the tree tests.
func sp(traceID, id, parent, name string, start, end int64) *model.Span {
	return &model.Span{
		TraceID: traceID, SpanID: id, ParentID: parent, Name: name,
		Service: "svc", Start: start, End: end, Status: model.StatusUnset,
	}
}

func TestBuildSimpleParentChild(t *testing.T) {
	traces := Build([]*model.Span{
		sp("t1", "a", "", "root", 0, 100),
		sp("t1", "b", "a", "child", 10, 50),
	})
	if len(traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(traces))
	}
	tr := traces[0]
	if len(tr.Roots) != 1 || tr.Roots[0].Span.Name != "root" {
		t.Fatalf("roots: %+v", tr.Roots)
	}
	if len(tr.Roots[0].Children) != 1 || tr.Roots[0].Children[0].Span.Name != "child" {
		t.Fatalf("children: %+v", tr.Roots[0].Children)
	}
}

func TestBuildGroupsByTraceIDAndSortsByStart(t *testing.T) {
	traces := Build([]*model.Span{
		sp("later", "a", "", "second", 500, 600),
		sp("early", "b", "", "first", 100, 200),
	})
	if len(traces) != 2 {
		t.Fatalf("want 2 traces, got %d", len(traces))
	}
	if traces[0].ID != "early" || traces[1].ID != "later" {
		t.Fatalf("order: %s, %s", traces[0].ID, traces[1].ID)
	}
}

func TestBuildChildrenSortedByStartTime(t *testing.T) {
	traces := Build([]*model.Span{
		sp("t1", "a", "", "root", 0, 100),
		sp("t1", "c", "a", "late", 60, 90),
		sp("t1", "b", "a", "early", 10, 50),
	})
	kids := traces[0].Roots[0].Children
	if kids[0].Span.Name != "early" || kids[1].Span.Name != "late" {
		t.Fatalf("children not time-sorted: %s, %s", kids[0].Span.Name, kids[1].Span.Name)
	}
}

func TestBuildOrphanBecomesRoot(t *testing.T) {
	// The parent was sampled away or lives in another file: the span must
	// surface as a root, not vanish.
	traces := Build([]*model.Span{
		sp("t1", "a", "", "root", 0, 100),
		sp("t1", "b", "missing", "orphan", 20, 40),
	})
	if len(traces[0].Roots) != 2 {
		t.Fatalf("want 2 roots, got %d", len(traces[0].Roots))
	}
}

func TestBuildSelfParentAndCyclesBreakIntoRoots(t *testing.T) {
	// A self-parent and a mutual a -> b -> a cycle can only come from
	// corrupt data, but they must terminate and keep every span visible.
	traces := Build([]*model.Span{sp("t1", "a", "a", "loop", 0, 10)})
	if len(traces[0].Roots) != 1 {
		t.Fatalf("self-parent roots: %d", len(traces[0].Roots))
	}
	traces = Build([]*model.Span{
		sp("t1", "a", "b", "a", 0, 10),
		sp("t1", "b", "a", "b", 0, 10),
	})
	tr := traces[0]
	total := 0
	tr.Walk(func(n *Node, depth int) bool { total++; return true })
	if total != 2 {
		t.Fatalf("cycle lost spans: walked %d of 2", total)
	}
	if len(tr.Roots) == 0 {
		t.Fatal("cycle must surface as roots")
	}
}

func TestBuildDuplicateSpanIDsBothKept(t *testing.T) {
	traces := Build([]*model.Span{
		sp("t1", "a", "", "root", 0, 100),
		sp("t1", "b", "a", "dup one", 10, 20),
		sp("t1", "b", "a", "dup two", 30, 40),
	})
	total := 0
	traces[0].Walk(func(n *Node, depth int) bool { total++; return true })
	if total != 3 {
		t.Fatalf("duplicate ID dropped a span: walked %d of 3", total)
	}
}

func TestTraceEnvelopeAndCounts(t *testing.T) {
	traces := Build([]*model.Span{
		sp("t1", "a", "", "root", 100, 900),
		func() *model.Span {
			s := sp("t1", "b", "a", "child", 50, 950) // overflows its parent
			s.Service = "other"
			s.Status = model.StatusError
			return s
		}(),
	})
	tr := traces[0]
	if tr.Start != 50 || tr.End != 950 || tr.Duration() != 900 {
		t.Fatalf("envelope: start=%d end=%d", tr.Start, tr.End)
	}
	if tr.Spans != 2 || tr.Errors != 1 {
		t.Fatalf("counts: spans=%d errors=%d", tr.Spans, tr.Errors)
	}
	if len(tr.Services) != 2 || tr.Services[0] != "other" || tr.Services[1] != "svc" {
		t.Fatalf("services: %v", tr.Services)
	}
}

func TestWalkDepthAndPruning(t *testing.T) {
	traces := Build([]*model.Span{
		sp("t1", "a", "", "root", 0, 100),
		sp("t1", "b", "a", "mid", 10, 90),
		sp("t1", "c", "b", "leaf", 20, 80),
	})
	var depths []int
	traces[0].Walk(func(n *Node, depth int) bool {
		depths = append(depths, depth)
		return depth < 1 // prune below depth 1: leaf must not be visited
	})
	if len(depths) != 2 || depths[0] != 0 || depths[1] != 1 {
		t.Fatalf("depths: %v", depths)
	}
}

func TestRootNameOfEmptyAndMultiRootTraces(t *testing.T) {
	traces := Build([]*model.Span{
		sp("t1", "b", "missing", "later root", 50, 60),
		sp("t1", "a", "missing", "earlier root", 10, 20),
	})
	if got := traces[0].RootName(); got != "earlier root" {
		t.Fatalf("RootName: %q", got)
	}
	empty := &Trace{}
	if got := empty.RootName(); got != "(empty trace)" {
		t.Fatalf("empty RootName: %q", got)
	}
}
