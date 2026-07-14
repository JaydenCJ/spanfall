// Tests for the text renderers. Rendering is pure (writer in, bytes out),
// so these assert on real output lines: bar geometry, tree glyphs, column
// alignment, filters, ASCII mode, and the JSON envelopes.
package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JaydenCJ/spanfall/internal/critical"
	"github.com/JaydenCJ/spanfall/internal/model"
	"github.com/JaydenCJ/spanfall/internal/stats"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

func sp(id, parent, service, name string, start, end int64) *model.Span {
	return &model.Span{
		TraceID: "aabbccddeeff00112233445566778899", SpanID: id, ParentID: parent,
		Name: name, Service: service, Kind: model.KindInternal,
		Start: start, End: end, Status: model.StatusUnset,
	}
}

// build assembles one trace and its critical path from spans.
func build(t *testing.T, spans ...*model.Span) (*trace.Trace, *critical.Path) {
	t.Helper()
	traces := trace.Build(spans)
	if len(traces) != 1 {
		t.Fatalf("want 1 trace, got %d", len(traces))
	}
	return traces[0], critical.Compute(traces[0])
}

func waterfall(t *testing.T, opt Options, spans ...*model.Span) string {
	t.Helper()
	tr, cp := build(t, spans...)
	var buf bytes.Buffer
	Waterfall(&buf, tr, cp, opt)
	return buf.String()
}

func demoSpans() []*model.Span {
	root := sp("a", "", "web", "GET /x", 0, 100_000_000)
	fast := sp("b", "a", "db", "SELECT one", 10_000_000, 30_000_000)
	slow := sp("c", "a", "db", "SELECT two", 10_000_000, 95_000_000)
	return []*model.Span{root, fast, slow}
}

func TestWaterfallHeaderAndFooterSummarizeTrace(t *testing.T) {
	out := waterfall(t, Options{}, demoSpans()...)
	if !strings.Contains(out, "trace aabbccddeeff00112233445566778899 · GET /x · 100.0ms · 3 spans · 2 services") {
		t.Fatalf("header missing:\n%s", out)
	}
	if !strings.Contains(out, "critical path: 2 of 3 spans") {
		t.Fatalf("footer missing:\n%s", out)
	}
}

func TestWaterfallCriticalSpansUseSolidBarsAndTreeGlyphs(t *testing.T) {
	out := waterfall(t, Options{}, demoSpans()...)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "SELECT two") && !strings.Contains(line, "█") {
			t.Fatalf("critical span lacks solid bar: %q", line)
		}
		if strings.Contains(line, "SELECT one") && strings.Contains(line, "█") {
			t.Fatalf("off-path span drawn solid: %q", line)
		}
		if strings.Contains(line, "SELECT one") && !strings.Contains(line, "░") {
			t.Fatalf("off-path span lacks light bar: %q", line)
		}
	}
	if !strings.Contains(out, "├─ SELECT one") || !strings.Contains(out, "└─ SELECT two") {
		t.Fatalf("tree glyphs missing:\n%s", out)
	}
}

func TestWaterfallBarGeometryScalesWithTime(t *testing.T) {
	// The root spans the whole envelope, so its bar must start at column 0
	// and be the widest bar in the output.
	out := waterfall(t, Options{Width: 80}, demoSpans()...)
	var rootBar, childBar int
	for _, line := range strings.Split(out, "\n") {
		bars := strings.Count(line, "█") + strings.Count(line, "░")
		if strings.Contains(line, "GET /x") && !strings.Contains(line, "trace ") {
			rootBar = bars
		}
		if strings.Contains(line, "SELECT one") {
			childBar = bars
		}
	}
	if rootBar == 0 || childBar == 0 {
		t.Fatalf("bars missing:\n%s", out)
	}
	// SELECT one is 20% of the trace: its bar must be roughly a fifth.
	if childBar >= rootBar/2 {
		t.Fatalf("bar not proportional: root=%d child=%d", rootBar, childBar)
	}
}

func TestWaterfallErrorFlag(t *testing.T) {
	spans := demoSpans()
	spans[1].Status = model.StatusError
	out := waterfall(t, Options{}, spans...)
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "SELECT one") && strings.Contains(line, "✗") {
			found = true
		}
	}
	if !found {
		t.Fatalf("error flag missing:\n%s", out)
	}
	if !strings.Contains(out, "1 error") {
		t.Fatalf("header error count missing:\n%s", out)
	}
}

func TestWaterfallASCIIModeIsPureASCII(t *testing.T) {
	spans := demoSpans()
	spans[1].Status = model.StatusError
	spans[1].End = spans[1].Start + 24_900 // exercises the µs unit too
	out := waterfall(t, Options{ASCII: true}, spans...)
	for i, r := range out {
		if r > 127 {
			t.Fatalf("non-ASCII rune %q at byte %d:\n%s", r, i, out)
		}
	}
	if !strings.Contains(out, "#") || !strings.Contains(out, "|- ") {
		t.Fatalf("ASCII bars/glyphs missing:\n%s", out)
	}
}

func TestWaterfallMinDurationHidesAndCounts(t *testing.T) {
	out := waterfall(t, Options{MinDur: 50_000_000}, demoSpans()...)
	if strings.Contains(out, "SELECT one") {
		t.Fatalf("short span not hidden:\n%s", out)
	}
	if !strings.Contains(out, "(1 span hidden") {
		t.Fatalf("hidden count missing:\n%s", out)
	}
}

func TestWaterfallMaxDepthHidesSubtrees(t *testing.T) {
	spans := append(demoSpans(), sp("d", "c", "db", "parse rows", 20_000_000, 90_000_000))
	out := waterfall(t, Options{MaxDepth: 1}, spans...)
	if strings.Contains(out, "SELECT") || strings.Contains(out, "parse rows") {
		t.Fatalf("depth filter leaked spans:\n%s", out)
	}
	if !strings.Contains(out, "(3 spans hidden") {
		t.Fatalf("hidden count missing:\n%s", out)
	}
}

func TestWaterfallColorEmitsANSIOnlyWhenEnabled(t *testing.T) {
	plain := waterfall(t, Options{}, demoSpans()...)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain output contains ANSI escapes")
	}
	colored := waterfall(t, Options{Color: true}, demoSpans()...)
	if !strings.Contains(colored, "\x1b[31m") {
		t.Fatalf("colored output lacks red critical bars")
	}
}

func TestWaterfallLongNamesTruncated(t *testing.T) {
	spans := demoSpans()
	spans[2].Name = strings.Repeat("very-long-operation-", 5)
	out := waterfall(t, Options{}, spans...)
	if !strings.Contains(out, "…") {
		t.Fatalf("long name not truncated:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "very-long") && len([]rune(line)) > 110 {
			t.Fatalf("line overflows width budget: %d runes", len([]rune(line)))
		}
	}
}

func TestListShowsOneLinePerTrace(t *testing.T) {
	s1 := sp("a", "", "web", "GET /x", 1_770_112_800_000_000_000, 1_770_112_800_100_000_000)
	s2 := sp("b", "", "web", "GET /y", 1_770_112_900_000_000_000, 1_770_112_900_050_000_000)
	s2.TraceID = "ffeeddccbbaa00112233445566778899"
	traces := trace.Build([]*model.Span{s1, s2})
	var buf bytes.Buffer
	List(&buf, traces, Options{})
	out := buf.String()
	if !strings.Contains(out, "aabbccddeeff00112233445566778899") ||
		!strings.Contains(out, "ffeeddccbbaa00112233445566778899") {
		t.Fatalf("trace ids missing:\n%s", out)
	}
	if !strings.Contains(out, "2026-02-03T10:00:00Z") {
		t.Fatalf("start timestamp missing:\n%s", out)
	}
}

func TestListJSONRoundTrips(t *testing.T) {
	s1 := sp("a", "", "web", "GET /x", 0, 100)
	traces := trace.Build([]*model.Span{s1})
	var buf bytes.Buffer
	if err := ListJSON(&buf, traces); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tool   string `json:"tool"`
		Traces []struct {
			TraceID string `json:"trace_id"`
			Spans   int    `json:"spans"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if doc.Tool != "spanfall" || len(doc.Traces) != 1 || doc.Traces[0].Spans != 1 {
		t.Fatalf("doc: %+v", doc)
	}
}

func TestCriticalTextBreakdown(t *testing.T) {
	tr, cp := build(t, demoSpans()...)
	var buf bytes.Buffer
	CriticalText(&buf, tr, cp, Options{})
	out := buf.String()
	if !strings.Contains(out, "2 of 3 spans on path") {
		t.Fatalf("header missing:\n%s", out)
	}
	if !strings.Contains(out, "85.0%") { // SELECT two: 85ms of 100ms
		t.Fatalf("self percent missing:\n%s", out)
	}
	if strings.Contains(out, "SELECT one") {
		t.Fatalf("off-path span listed:\n%s", out)
	}
	if !strings.Contains(out, "accounts for 100.0%") {
		t.Fatalf("footer missing:\n%s", out)
	}
}

func TestCriticalJSONSchema(t *testing.T) {
	tr, cp := build(t, demoSpans()...)
	var buf bytes.Buffer
	if err := CriticalJSON(&buf, tr, cp); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tool          string `json:"tool"`
		SchemaVersion int    `json:"schema_version"`
		DurationNS    int64  `json:"trace_duration_ns"`
		Path          []struct {
			Name        string  `json:"name"`
			SelfNS      int64   `json:"self_ns"`
			SelfPercent float64 `json:"self_percent"`
		} `json:"path"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.SchemaVersion != 1 || doc.DurationNS != 100_000_000 || len(doc.Path) != 2 {
		t.Fatalf("doc: %+v", doc)
	}
	var total int64
	for _, e := range doc.Path {
		total += e.SelfNS
	}
	if total != 100_000_000 {
		t.Fatalf("path self times must sum to the trace: %d", total)
	}
}

func TestStatsTextTable(t *testing.T) {
	tr, _ := build(t, demoSpans()...)
	rows := stats.Aggregate([]*trace.Trace{tr})
	var buf bytes.Buffer
	StatsText(&buf, rows, Options{})
	out := buf.String()
	if !strings.Contains(out, "operation") || !strings.Contains(out, "GET /x") {
		t.Fatalf("table missing rows:\n%s", out)
	}
}

func TestStatsJSONSchema(t *testing.T) {
	tr, _ := build(t, demoSpans()...)
	rows := stats.Aggregate([]*trace.Trace{tr})
	var buf bytes.Buffer
	if err := StatsJSON(&buf, rows); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Operations []struct {
			Operation string `json:"operation"`
			TotalNS   int64  `json:"total_ns"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(doc.Operations) != 3 {
		t.Fatalf("operations: %+v", doc.Operations)
	}
}

func TestTruncateAndPadHelpers(t *testing.T) {
	if got := truncate("hello", 10, "…"); got != "hello" {
		t.Fatalf("no-op truncate: %q", got)
	}
	if got := truncate("hello world", 6, "…"); got != "hello…" {
		t.Fatalf("truncate: %q", got)
	}
	if got := padRight("é", 3); got != "é  " { // rune-aware, not byte-aware
		t.Fatalf("padRight: %q", got)
	}
	if got := padLeft("42", 4); got != "  42" {
		t.Fatalf("padLeft: %q", got)
	}
}
