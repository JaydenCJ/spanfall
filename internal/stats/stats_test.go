// Tests for per-operation aggregation and the self-time interval math that
// backs it. Self time must subtract overlapping children only once and can
// never go negative, or the stats table would lie about hot spots.
package stats

import (
	"testing"

	"github.com/JaydenCJ/spanfall/internal/model"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

func sp(id, parent, service, name string, start, end int64) *model.Span {
	return &model.Span{
		TraceID: "t1", SpanID: id, ParentID: parent, Name: name,
		Service: service, Start: start, End: end, Status: model.StatusUnset,
	}
}

func TestSelfTimeSubtractsChildCoverage(t *testing.T) {
	traces := trace.Build([]*model.Span{
		sp("a", "", "svc", "root", 0, 100),
		sp("b", "a", "svc", "child", 20, 60),
	})
	if got := SelfTime(traces[0].Roots[0]); got != 60 {
		t.Fatalf("self: %d", got)
	}
}

func TestSelfTimeOverlappingChildrenCountedOnce(t *testing.T) {
	// Two parallel children covering [10,50) and [30,80): union is 70,
	// not 90; naive subtraction would yield 10 instead of 30.
	traces := trace.Build([]*model.Span{
		sp("a", "", "svc", "root", 0, 100),
		sp("b", "a", "svc", "one", 10, 50),
		sp("c", "a", "svc", "two", 30, 80),
	})
	if got := SelfTime(traces[0].Roots[0]); got != 30 {
		t.Fatalf("self: %d", got)
	}
}

func TestSelfTimeClampsChildrenAndNeverGoesNegative(t *testing.T) {
	// An async child outliving its parent only subtracts the overlap.
	traces := trace.Build([]*model.Span{
		sp("a", "", "svc", "root", 0, 100),
		sp("b", "a", "svc", "async", 50, 300),
	})
	if got := SelfTime(traces[0].Roots[0]); got != 50 {
		t.Fatalf("self must clamp child to parent window: %d", got)
	}
	// A child covering more than its parent (clock skew) floors at zero.
	traces = trace.Build([]*model.Span{
		sp("a", "", "svc", "root", 10, 90),
		sp("b", "a", "svc", "cover", 0, 100),
	})
	if got := SelfTime(traces[0].Roots[0]); got != 0 {
		t.Fatalf("self went negative: %d", got)
	}
}

func TestAggregateGroupsByServiceAndName(t *testing.T) {
	traces := trace.Build([]*model.Span{
		sp("a", "", "web", "GET /x", 0, 100),
		sp("b", "a", "db", "SELECT", 10, 40),
		sp("c", "a", "db", "SELECT", 50, 90),
	})
	rows := Aggregate(traces)
	if len(rows) != 2 {
		t.Fatalf("rows: %d", len(rows))
	}
	var dbRow *Row
	for i := range rows {
		if rows[i].Service == "db" {
			dbRow = &rows[i]
		}
	}
	if dbRow == nil || dbRow.Count != 2 || dbRow.Total != 70 || dbRow.Max != 40 {
		t.Fatalf("db row: %+v", dbRow)
	}
}

func TestAggregateCountsErrors(t *testing.T) {
	errSpan := sp("b", "a", "db", "SELECT", 10, 40)
	errSpan.Status = model.StatusError
	traces := trace.Build([]*model.Span{
		sp("a", "", "web", "GET /x", 0, 100),
		errSpan,
	})
	for _, r := range Aggregate(traces) {
		if r.Service == "db" && r.Errors != 1 {
			t.Fatalf("db errors: %d", r.Errors)
		}
		if r.Service == "web" && r.Errors != 0 {
			t.Fatalf("web errors: %d", r.Errors)
		}
	}
}

func TestAggregateSortsBySelfTimeDescending(t *testing.T) {
	traces := trace.Build([]*model.Span{
		sp("a", "", "web", "root", 0, 100), // self 100-90=10
		sp("b", "a", "db", "big", 5, 95),   // self 90
	})
	rows := Aggregate(traces)
	if rows[0].Name != "big" || rows[1].Name != "root" {
		t.Fatalf("order: %s, %s", rows[0].Name, rows[1].Name)
	}
}

func TestAggregateSpansAcrossTraces(t *testing.T) {
	s1 := sp("a", "", "web", "GET /x", 0, 100)
	s2 := sp("b", "", "web", "GET /x", 0, 300)
	s2.TraceID = "t2"
	rows := Aggregate(trace.Build([]*model.Span{s1, s2}))
	if len(rows) != 1 || rows[0].Count != 2 || rows[0].Total != 400 || rows[0].Max != 300 {
		t.Fatalf("cross-trace row: %+v", rows[0])
	}
}
