// StatsText and StatsJSON present the per-operation aggregate table,
// ordered by summed self time — the "where does the time actually go" view
// across every trace in the file.
package render

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/JaydenCJ/spanfall/internal/stats"
)

// StatsText writes the aligned aggregate table.
func StatsText(w io.Writer, rows []stats.Row, opt Options) {
	cs := opt.charset()
	nameW := utf8.RuneCountInString("operation")
	svcW := utf8.RuneCountInString("service")
	for _, r := range rows {
		nameW = maxInt(nameW, utf8.RuneCountInString(r.Name))
		svcW = maxInt(svcW, utf8.RuneCountInString(r.Service))
	}
	nameW = minInt(nameW, maxLabelW)
	svcW = minInt(svcW, maxSvcW)

	header := padRight("operation", nameW) + "  " + padRight("service", svcW) +
		"  count  errors  " + padLeft("total", 9) + "  " + padLeft("self", 9) + "  " + padLeft("max", 9)
	fmt.Fprintln(w, opt.paint(header, ansiDim))
	for _, r := range rows {
		line := fmt.Sprintf("%s  %s  %5d  %6d  %s  %s  %s",
			padRight(truncate(r.Name, nameW, cs.ellipsis), nameW),
			opt.paint(padRight(r.Service, svcW), ansiDim),
			r.Count, r.Errors,
			padLeft(opt.dur(r.Total), 9),
			padLeft(opt.dur(r.Self), 9),
			padLeft(opt.dur(r.Max), 9))
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}

type statsEntry struct {
	Operation string `json:"operation"`
	Service   string `json:"service"`
	Count     int    `json:"count"`
	Errors    int    `json:"errors"`
	TotalNS   int64  `json:"total_ns"`
	SelfNS    int64  `json:"self_ns"`
	MaxNS     int64  `json:"max_ns"`
}

type statsDoc struct {
	Tool          string       `json:"tool"`
	SchemaVersion int          `json:"schema_version"`
	Operations    []statsEntry `json:"operations"`
}

// StatsJSON writes the machine-readable aggregate.
func StatsJSON(w io.Writer, rows []stats.Row) error {
	doc := statsDoc{Tool: "spanfall", SchemaVersion: 1, Operations: []statsEntry{}}
	for _, r := range rows {
		doc.Operations = append(doc.Operations, statsEntry{
			Operation: r.Name,
			Service:   r.Service,
			Count:     r.Count,
			Errors:    r.Errors,
			TotalNS:   r.Total,
			SelfNS:    r.Self,
			MaxNS:     r.Max,
		})
	}
	return writeJSON(w, doc)
}
