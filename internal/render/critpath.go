// CriticalText and CriticalJSON present the critical-path breakdown: every
// span on the path with its self time — the latency that belongs to that
// span alone and would disappear if it did.
package render

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/JaydenCJ/spanfall/internal/critical"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

// CriticalText writes the human-readable breakdown, ordered by first
// appearance along the timeline so it reads as the story of the request.
func CriticalText(w io.Writer, t *trace.Trace, path *critical.Path, opt Options) {
	cs := opt.charset()
	total := t.Duration()
	head := fmt.Sprintf("critical path%strace %s%s%s%s%d of %d %s on path",
		cs.sep, t.ID, cs.sep, opt.dur(total), cs.sep,
		len(path.Order), t.Spans, plural(t.Spans, "span"))
	fmt.Fprintln(w, opt.paint(head, ansiBold))
	fmt.Fprintln(w)

	nameW := utf8.RuneCountInString("span")
	svcW := utf8.RuneCountInString("service")
	durW := utf8.RuneCountInString("self")
	for _, s := range path.Order {
		nameW = maxInt(nameW, utf8.RuneCountInString(s.Name))
		svcW = maxInt(svcW, utf8.RuneCountInString(s.Service))
		durW = maxInt(durW, utf8.RuneCountInString(opt.dur(path.Self[s])))
	}
	nameW = minInt(nameW, maxLabelW)

	header := padLeft("self", durW) + "  " + padLeft("% of trace", 10) + "  " +
		padRight("span", nameW) + "  service"
	fmt.Fprintln(w, opt.paint(header, ansiDim))
	for _, s := range path.Order {
		self := path.Self[s]
		name := padRight(truncate(s.Name, nameW, cs.ellipsis), nameW)
		line := fmt.Sprintf("%s  %s  %s  %s",
			padLeft(opt.dur(self), durW),
			padLeft(fmt.Sprintf("%.1f%%", percent(self, total)), 10),
			name, opt.paint(padRight(s.Service, svcW), ansiDim))
		if s.IsError() {
			line += " " + opt.paint(cs.errFlag, ansiRed)
			if s.StatusMsg != "" {
				line += " " + opt.paint(s.StatusMsg, ansiRed)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
	if gap := path.GapTime(); gap > 0 {
		fmt.Fprintf(w, "%s  %s  %s\n",
			padLeft(opt.dur(gap), durW),
			padLeft(fmt.Sprintf("%.1f%%", percent(gap, total)), 10),
			"(gap: no span active)")
	}

	var accounted int64
	for _, seg := range path.Segments {
		if seg.Span != nil {
			accounted += seg.Duration()
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "on-path self time accounts for %.1f%% of the %s trace\n",
		percent(accounted, total), opt.dur(total))
}

type critEntry struct {
	SpanID      string  `json:"span_id"`
	Name        string  `json:"name"`
	Service     string  `json:"service"`
	Kind        string  `json:"kind"`
	SelfNS      int64   `json:"self_ns"`
	SelfPercent float64 `json:"self_percent"`
	Error       bool    `json:"error"`
}

type critDoc struct {
	Tool          string      `json:"tool"`
	SchemaVersion int         `json:"schema_version"`
	TraceID       string      `json:"trace_id"`
	DurationNS    int64       `json:"trace_duration_ns"`
	Spans         int         `json:"span_count"`
	PathSpans     int         `json:"path_span_count"`
	GapNS         int64       `json:"gap_ns"`
	Path          []critEntry `json:"path"`
}

// CriticalJSON writes the same breakdown for machines, self_percent rounded
// to one decimal to keep the document byte-stable across float formatting.
func CriticalJSON(w io.Writer, t *trace.Trace, path *critical.Path) error {
	doc := critDoc{
		Tool:          "spanfall",
		SchemaVersion: 1,
		TraceID:       displayID(t.ID),
		DurationNS:    t.Duration(),
		Spans:         t.Spans,
		PathSpans:     len(path.Order),
		GapNS:         path.GapTime(),
		Path:          []critEntry{},
	}
	for _, s := range path.Order {
		self := path.Self[s]
		doc.Path = append(doc.Path, critEntry{
			SpanID:      s.SpanID,
			Name:        s.Name,
			Service:     s.Service,
			Kind:        s.Kind,
			SelfNS:      self,
			SelfPercent: round1(percent(self, t.Duration())),
			Error:       s.IsError(),
		})
	}
	return writeJSON(w, doc)
}

func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}
