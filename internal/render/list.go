// List renders one line per trace in a file: the greppable inventory view
// for files that contain more than one trace.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/JaydenCJ/spanfall/internal/timefmt"
	"github.com/JaydenCJ/spanfall/internal/trace"
)

// List writes the aligned text table of traces.
func List(w io.Writer, traces []*trace.Trace, opt Options) {
	idW := utf8.RuneCountInString("trace id")
	durW := utf8.RuneCountInString("duration")
	rootW := utf8.RuneCountInString("root")
	for _, t := range traces {
		idW = maxInt(idW, utf8.RuneCountInString(displayID(t.ID)))
		durW = maxInt(durW, utf8.RuneCountInString(opt.dur(t.Duration())))
		rootW = maxInt(rootW, utf8.RuneCountInString(t.RootName()))
	}
	header := padRight("trace id", idW) + "  " + padRight("start", 20) + "  " +
		padLeft("duration", durW) + "  spans  services  errors  root"
	fmt.Fprintln(w, opt.paint(header, ansiDim))
	for _, t := range traces {
		fmt.Fprintf(w, "%s  %s  %s  %5d  %8d  %6d  %s\n",
			padRight(displayID(t.ID), idW),
			padRight(timefmt.Timestamp(t.Start), 20),
			padLeft(opt.dur(t.Duration()), durW),
			t.Spans, len(t.Services), t.Errors, t.RootName())
	}
}

// listEntry is the JSON row shape; field order matches the text columns.
type listEntry struct {
	TraceID  string `json:"trace_id"`
	Start    string `json:"start"`
	StartNS  int64  `json:"start_unix_nano"`
	Duration int64  `json:"duration_ns"`
	Spans    int    `json:"spans"`
	Services int    `json:"services"`
	Errors   int    `json:"errors"`
	Root     string `json:"root"`
}

type listDoc struct {
	Tool          string      `json:"tool"`
	SchemaVersion int         `json:"schema_version"`
	Traces        []listEntry `json:"traces"`
}

// ListJSON writes the machine-readable trace inventory.
func ListJSON(w io.Writer, traces []*trace.Trace) error {
	doc := listDoc{Tool: "spanfall", SchemaVersion: 1, Traces: []listEntry{}}
	for _, t := range traces {
		doc.Traces = append(doc.Traces, listEntry{
			TraceID:  displayID(t.ID),
			Start:    timefmt.Timestamp(t.Start),
			StartNS:  t.Start,
			Duration: t.Duration(),
			Spans:    t.Spans,
			Services: len(t.Services),
			Errors:   t.Errors,
			Root:     t.RootName(),
		})
	}
	return writeJSON(w, doc)
}

// displayID keeps a missing trace ID visible instead of printing nothing.
func displayID(id string) string {
	if id == "" {
		return "(no trace id)"
	}
	return id
}

// writeJSON is the shared two-space-indented emitter.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
