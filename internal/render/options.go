// Package render turns trace trees and critical paths into terminal output:
// the waterfall view, the trace list, the critical-path breakdown, and the
// per-operation stats table, each in text and (where useful) JSON.
package render

import (
	"strings"
	"unicode/utf8"

	"github.com/JaydenCJ/spanfall/internal/timefmt"
)

// Options controls all text renderers.
type Options struct {
	Width    int   // total line budget in cells; <= 0 means the default 100
	Color    bool  // emit ANSI colors
	ASCII    bool  // restrict output to 7-bit ASCII
	MaxDepth int   // hide spans deeper than this; 0 means unlimited
	MinDur   int64 // hide spans shorter than this many nanoseconds
}

// DefaultWidth is used when Options.Width is unset.
const DefaultWidth = 100

func (o Options) width() int {
	if o.Width <= 0 {
		return DefaultWidth
	}
	if o.Width < 60 {
		return 60 // below this the waterfall column would collapse
	}
	return o.Width
}

// dur formats a duration honoring ASCII mode ("us" instead of "µs").
func (o Options) dur(ns int64) string {
	if o.ASCII {
		return timefmt.ASCIIDuration(ns)
	}
	return timefmt.Duration(ns)
}

// charset is the drawing alphabet; one Unicode set, one pure-ASCII set.
type charset struct {
	crit     string // waterfall bar fill for critical-path spans
	bar      string // waterfall bar fill for everything else
	mid      string // tree branch
	last     string // tree final branch
	cont     string // tree continuation
	blank    string // tree spacer
	errFlag  string // error marker
	sep      string // header field separator
	dot      string // timeline ruler fill
	ellipsis string // truncation marker
}

var unicodeSet = charset{
	crit: "█", bar: "░",
	mid: "├─ ", last: "└─ ", cont: "│  ", blank: "   ",
	errFlag: "✗", sep: " · ", dot: "·", ellipsis: "…",
}

var asciiSet = charset{
	crit: "#", bar: "-",
	mid: "|- ", last: "`- ", cont: "|  ", blank: "   ",
	errFlag: "x", sep: " | ", dot: ".", ellipsis: "..",
}

func (o Options) charset() charset {
	if o.ASCII {
		return asciiSet
	}
	return unicodeSet
}

// ANSI escape codes. Applied only when Options.Color is set.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
)

func (o Options) paint(s, code string) string {
	if !o.Color || s == "" {
		return s
	}
	return code + s + ansiReset
}

// padRight / padLeft pad by display runes, not bytes; the tree glyphs and
// the µ sign are multi-byte, so fmt's %-*s would misalign columns.
func padRight(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func padLeft(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n >= w {
		return s
	}
	return strings.Repeat(" ", w-n) + s
}

// truncate cuts s to at most w display runes, ending with the ellipsis.
func truncate(s string, w int, ellipsis string) string {
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	ell := utf8.RuneCountInString(ellipsis)
	if w <= ell {
		return string([]rune(s)[:w])
	}
	return string([]rune(s)[:w-ell]) + ellipsis
}

// percent formats part/total as "12.3%"; total 0 yields "0.0%".
func percent(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// plural is the tiny helper every summary line needs.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
