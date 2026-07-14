// The four data subcommands: render, list, critical, stats. Each parses its
// flags, loads traces, and hands off to the render package.
package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/JaydenCJ/spanfall/internal/critical"
	"github.com/JaydenCJ/spanfall/internal/render"
	"github.com/JaydenCJ/spanfall/internal/stats"
)

func runRender(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var df displayFlags
	df.register(fs)
	traceID := fs.String("trace", "", "select one trace by ID prefix")
	all := fs.Bool("all", false, "render every trace in the file")
	maxDepth := fs.Int("max-depth", 0, "hide spans nested deeper than this (0 = unlimited)")
	minDur := fs.String("min-duration", "", "hide spans shorter than this, e.g. 5ms")
	failOnError := fs.Bool("fail-on-error", false, "exit 1 if the rendered trace has error spans")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	opt, err := df.toOptions(stdout)
	if err != nil {
		fmt.Fprintf(stderr, "spanfall: %v\n", err)
		return ExitUsage
	}
	opt.MaxDepth = *maxDepth
	if opt.MinDur, err = parseMinDuration(*minDur); err != nil {
		fmt.Fprintf(stderr, "spanfall: %v\n", err)
		return ExitUsage
	}
	if *all && *traceID != "" {
		fmt.Fprintf(stderr, "spanfall: --all conflicts with --trace\n")
		return ExitUsage
	}
	path, code := onePath(fs.Args(), stderr)
	if code != ExitOK {
		return code
	}
	traces, code := loadTraces(path, stderr)
	if code != ExitOK {
		return code
	}
	selected, code := selectTraces(traces, *traceID, stderr)
	if code != ExitOK {
		return code
	}
	if !*all && *traceID == "" && len(selected) > 1 {
		fmt.Fprintf(stderr, "spanfall: file contains %d traces; rendering the first — use --trace or --all\n", len(selected))
		selected = selected[:1]
	}

	errCount := 0
	for i, t := range selected {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		cp := critical.Compute(t)
		render.Waterfall(stdout, t, cp, opt)
		errCount += t.Errors
	}
	if *failOnError && errCount > 0 {
		noun := "spans"
		if errCount == 1 {
			noun = "span"
		}
		fmt.Fprintf(stderr, "spanfall: %d error %s present (--fail-on-error)\n", errCount, noun)
		return ExitFail
	}
	return ExitOK
}

func runList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var df displayFlags
	df.register(fs)
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	opt, code := formatOptions(&df, *format, stdout, stderr)
	if code != ExitOK {
		return code
	}
	path, code := onePath(fs.Args(), stderr)
	if code != ExitOK {
		return code
	}
	traces, code := loadTraces(path, stderr)
	if code != ExitOK {
		return code
	}
	if *format == "json" {
		return emitJSON(render.ListJSON(stdout, traces), stderr)
	}
	render.List(stdout, traces, opt)
	return ExitOK
}

func runCritical(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("critical", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var df displayFlags
	df.register(fs)
	format := fs.String("format", "text", "output format: text or json")
	traceID := fs.String("trace", "", "select one trace by ID prefix")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	opt, code := formatOptions(&df, *format, stdout, stderr)
	if code != ExitOK {
		return code
	}
	path, code := onePath(fs.Args(), stderr)
	if code != ExitOK {
		return code
	}
	traces, code := loadTraces(path, stderr)
	if code != ExitOK {
		return code
	}
	selected, code := selectTraces(traces, *traceID, stderr)
	if code != ExitOK {
		return code
	}
	if *traceID == "" && len(selected) > 1 {
		fmt.Fprintf(stderr, "spanfall: file contains %d traces; using the first — pass --trace to pick one\n", len(selected))
		selected = selected[:1]
	}
	t := selected[0]
	cp := critical.Compute(t)
	if *format == "json" {
		return emitJSON(render.CriticalJSON(stdout, t, cp), stderr)
	}
	render.CriticalText(stdout, t, cp, opt)
	return ExitOK
}

func runStats(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var df displayFlags
	df.register(fs)
	format := fs.String("format", "text", "output format: text or json")
	traceID := fs.String("trace", "", "select one trace by ID prefix")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	opt, code := formatOptions(&df, *format, stdout, stderr)
	if code != ExitOK {
		return code
	}
	path, code := onePath(fs.Args(), stderr)
	if code != ExitOK {
		return code
	}
	traces, code := loadTraces(path, stderr)
	if code != ExitOK {
		return code
	}
	selected, code := selectTraces(traces, *traceID, stderr)
	if code != ExitOK {
		return code
	}
	rows := stats.Aggregate(selected)
	if *format == "json" {
		return emitJSON(render.StatsJSON(stdout, rows), stderr)
	}
	render.StatsText(stdout, rows, opt)
	return ExitOK
}

// formatOptions validates --format plus the display flags in one place.
func formatOptions(df *displayFlags, format string, stdout io.Writer, stderr io.Writer) (render.Options, int) {
	if format != "text" && format != "json" {
		fmt.Fprintf(stderr, "spanfall: unknown --format %q (want text or json)\n", format)
		return render.Options{}, ExitUsage
	}
	opt, err := df.toOptions(stdout)
	if err != nil {
		fmt.Fprintf(stderr, "spanfall: %v\n", err)
		return opt, ExitUsage
	}
	return opt, ExitOK
}

func emitJSON(err error, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintf(stderr, "spanfall: %v\n", err)
		return ExitRuntime
	}
	return ExitOK
}
