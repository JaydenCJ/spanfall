// Waterfall renders one trace as an aligned tree + timeline view. Bars are
// positioned proportionally inside the trace envelope; spans on the critical
// path get the solid fill so the eye lands on what actually cost time.
package render

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/JaydenCJ/spanfall/internal/critical"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

// row is one prepared waterfall line before column sizing.
type row struct {
	label   string // tree prefix + span name
	service string
	dur     string
	err     bool
	crit    bool
	start   int64
	end     int64
}

const (
	minBarWidth = 16
	maxLabelW   = 48
	maxSvcW     = 16
)

// Waterfall writes the full waterfall view for one trace.
func Waterfall(w io.Writer, t *trace.Trace, path *critical.Path, opt Options) {
	cs := opt.charset()
	rows, hidden := collectRows(t, path, opt)

	// Column widths adapt to content within fixed ceilings.
	labelW := utf8.RuneCountInString("span")
	svcW := utf8.RuneCountInString("service")
	durW := utf8.RuneCountInString("duration")
	for _, r := range rows {
		labelW = maxInt(labelW, utf8.RuneCountInString(r.label))
		svcW = maxInt(svcW, utf8.RuneCountInString(r.service))
		durW = maxInt(durW, utf8.RuneCountInString(r.dur))
	}
	labelW = minInt(labelW, maxLabelW)
	svcW = minInt(svcW, maxSvcW)
	barW := opt.width() - labelW - svcW - durW - 7 // "  ", "  ", " F "
	if barW < minBarWidth {
		barW = minBarWidth
	}

	// Trace header.
	head := fmt.Sprintf("trace %s%s%s%s%s%s%d %s%s%d %s",
		t.ID, cs.sep, t.RootName(), cs.sep, opt.dur(t.Duration()), cs.sep,
		t.Spans, plural(t.Spans, "span"), cs.sep, len(t.Services), plural(len(t.Services), "service"))
	if t.Errors > 0 {
		head += fmt.Sprintf("%s%d %s", cs.sep, t.Errors, plural(t.Errors, "error"))
	}
	fmt.Fprintln(w, opt.paint(head, ansiBold))
	fmt.Fprintln(w)

	// Column header with a timeline ruler over the bar area.
	header := padRight("span", labelW) + "  " + padRight("service", svcW) + "  " +
		padLeft("duration", durW) + "   " + ruler(t.Duration(), barW, opt)
	fmt.Fprintln(w, opt.paint(strings.TrimRight(header, " "), ansiDim))

	for _, r := range rows {
		label := padRight(truncate(r.label, labelW, cs.ellipsis), labelW)
		flag := " "
		if r.err {
			flag = opt.paint(cs.errFlag, ansiRed)
		}
		bar := bar(r, t, barW, cs, opt)
		line := label + "  " + opt.paint(padRight(r.service, svcW), ansiDim) + "  " +
			padLeft(r.dur, durW) + " " + flag + " " + bar
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}

	// Footer: critical-path summary plus what was hidden, if anything.
	fmt.Fprintln(w)
	onPath := len(path.Order)
	fmt.Fprintf(w, "critical path: %d of %d %s (%s)%srun 'spanfall critical' for the breakdown\n",
		onPath, t.Spans, plural(t.Spans, "span"), cs.crit, cs.sep)
	if hidden > 0 {
		fmt.Fprintf(w, "(%d %s hidden by --min-duration/--max-depth)\n", hidden, plural(hidden, "span"))
	}
}

// collectRows walks the tree building tree-drawing prefixes, applying the
// depth and duration filters. hidden counts every span pruned by a filter,
// including its descendants.
func collectRows(t *trace.Trace, path *critical.Path, opt Options) (rows []row, hidden int) {
	cs := opt.charset()
	var rec func(n *trace.Node, prefix string, isLast bool, depth int, isRoot bool)
	rec = func(n *trace.Node, prefix string, isLast bool, depth int, isRoot bool) {
		if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
			hidden += countNodes(n)
			return
		}
		if opt.MinDur > 0 && n.Span.Duration() < opt.MinDur {
			hidden += countNodes(n)
			return
		}
		label := n.Span.Name
		childPrefix := prefix
		if !isRoot {
			branch := cs.mid
			if isLast {
				branch = cs.last
			}
			label = prefix + branch + label
			if isLast {
				childPrefix += cs.blank
			} else {
				childPrefix += cs.cont
			}
		}
		rows = append(rows, row{
			label:   label,
			service: n.Span.Service,
			dur:     opt.dur(n.Span.Duration()),
			err:     n.Span.IsError(),
			crit:    path.OnPath(n.Span),
			start:   n.Span.Start,
			end:     n.Span.End,
		})
		for i, c := range n.Children {
			rec(c, childPrefix, i == len(n.Children)-1, depth+1, false)
		}
	}
	for _, r := range t.Roots {
		rec(r, "", false, 0, true)
	}
	return rows, hidden
}

func countNodes(n *trace.Node) int {
	total := 1
	for _, c := range n.Children {
		total += countNodes(c)
	}
	return total
}

// bar draws one span's timeline bar: offset spaces, then fill. Every span
// gets at least one cell so microsecond spans stay visible next to
// second-long parents.
func bar(r row, t *trace.Trace, barW int, cs charset, opt Options) string {
	total := t.Duration()
	if total <= 0 {
		return ""
	}
	scale := float64(barW) / float64(total)
	start := int(float64(r.start-t.Start) * scale)
	length := int(float64(r.end-r.start)*scale + 0.5)
	if length < 1 {
		length = 1
	}
	if start > barW-1 {
		start = barW - 1
	}
	if start+length > barW {
		length = barW - start
	}
	fill, color := cs.bar, ansiDim
	if r.crit {
		fill, color = cs.crit, ansiRed
	}
	return strings.Repeat(" ", start) + opt.paint(strings.Repeat(fill, length), color)
}

// ruler renders the timeline header: "0" anchored left, the total duration
// anchored right, dots between.
func ruler(total int64, barW int, opt Options) string {
	cs := opt.charset()
	label := opt.dur(total)
	dots := barW - 2 - utf8.RuneCountInString(label) - 2
	if dots < 0 {
		return "0"
	}
	return "0 " + strings.Repeat(cs.dot, dots) + " " + label
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
