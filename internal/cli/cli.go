// Package cli implements the spanfall command-line interface. Run takes
// argv and two writers and returns an exit code, so the whole surface is
// testable in-process without building a binary.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/JaydenCJ/spanfall/internal/load"
	"github.com/JaydenCJ/spanfall/internal/render"
	"github.com/JaydenCJ/spanfall/internal/trace"
	"github.com/JaydenCJ/spanfall/internal/version"
)

// Exit codes. Documented in the README; --fail-on-error uses ExitFail as
// its machine-readable verdict.
const (
	ExitOK      = 0
	ExitFail    = 1
	ExitUsage   = 2
	ExitRuntime = 3
)

// Run dispatches argv and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runRender(nil, stdout, stderr)
	}
	switch args[0] {
	case "render":
		return runRender(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "critical":
		return runCritical(args[1:], stdout, stderr)
	case "stats":
		return runStats(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "spanfall %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	default:
		if strings.HasPrefix(args[0], "-") {
			// Bare flags: treat as `render` with flags (spanfall --width 80 f.json).
			return runRender(args, stdout, stderr)
		}
		// Bare path: treat as `render <path>` — but a word that is neither a
		// subcommand nor a file on disk is almost certainly a typo, so say
		// so instead of surfacing a bare "no such file" from the OS.
		if _, err := os.Stat(args[0]); err != nil && os.IsNotExist(err) {
			fmt.Fprintf(stderr, "spanfall: %q is neither a spanfall command nor an existing file (run 'spanfall --help' for usage)\n", args[0])
			return ExitUsage
		}
		return runRender(args, stdout, stderr)
	}
}

// displayFlags are shared by every subcommand that prints for humans.
type displayFlags struct {
	width int
	color string
	ascii bool
}

func (d *displayFlags) register(fs *flag.FlagSet) {
	fs.IntVar(&d.width, "width", render.DefaultWidth, "total output width in columns")
	fs.StringVar(&d.color, "color", "auto", "colorize output: auto, always, or never")
	fs.BoolVar(&d.ascii, "ascii", false, "restrict output to 7-bit ASCII characters")
}

// toOptions validates the display flags; the returned error is a usage error.
func (d *displayFlags) toOptions(stdout io.Writer) (render.Options, error) {
	opt := render.Options{Width: d.width, ASCII: d.ascii}
	switch d.color {
	case "always":
		opt.Color = true
	case "never":
		opt.Color = false
	case "auto":
		opt.Color = isTerminal(stdout)
	default:
		return opt, fmt.Errorf("unknown --color %q (want auto, always, or never)", d.color)
	}
	if d.width != 0 && d.width < 60 {
		return opt, fmt.Errorf("--width %d is too narrow (minimum 60)", d.width)
	}
	return opt, nil
}

// isTerminal reports whether w is an interactive terminal, so --color auto
// stays plain when output is piped into grep or a file.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// onePath extracts the optional single positional file argument;
// no argument and "-" both mean stdin.
func onePath(rest []string, stderr io.Writer) (string, int) {
	switch len(rest) {
	case 0:
		return "-", ExitOK
	case 1:
		return rest[0], ExitOK
	default:
		fmt.Fprintf(stderr, "spanfall: expected at most one file argument, got %d\n", len(rest))
		return "", ExitUsage
	}
}

// loadTraces reads and parses the file into trace trees.
func loadTraces(path string, stderr io.Writer) ([]*trace.Trace, int) {
	spans, err := load.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "spanfall: %v\n", err)
		return nil, ExitRuntime
	}
	traces := trace.Build(spans)
	if len(traces) == 0 {
		fmt.Fprintf(stderr, "spanfall: no spans found in input\n")
		return nil, ExitRuntime
	}
	return traces, ExitOK
}

// selectTraces applies a --trace ID prefix filter. An empty prefix keeps
// everything; an ambiguous prefix is a usage error listing the candidates;
// no match is a runtime error.
func selectTraces(traces []*trace.Trace, prefix string, stderr io.Writer) ([]*trace.Trace, int) {
	if prefix == "" {
		return traces, ExitOK
	}
	prefix = strings.ToLower(prefix)
	var matched []*trace.Trace
	for _, t := range traces {
		if strings.HasPrefix(t.ID, prefix) {
			matched = append(matched, t)
		}
	}
	switch len(matched) {
	case 0:
		fmt.Fprintf(stderr, "spanfall: no trace matches --trace %s (file has %d)\n", prefix, len(traces))
		return nil, ExitRuntime
	case 1:
		return matched, ExitOK
	default:
		fmt.Fprintf(stderr, "spanfall: --trace %s is ambiguous; matches:\n", prefix)
		for _, t := range matched {
			fmt.Fprintf(stderr, "  %s  %s\n", t.ID, t.RootName())
		}
		return nil, ExitUsage
	}
}

// parseMinDuration accepts Go duration syntax ("5ms", "1.5s").
func parseMinDuration(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid --min-duration %q: want e.g. 5ms, 1.5s", s)
	}
	return int64(d), nil
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `spanfall %s — trace waterfalls in your terminal, no backend required

Usage:
  spanfall [render] [flags] [file]    waterfall view with critical path (default)
  spanfall critical [flags] [file]    critical-path breakdown with self times
  spanfall list     [flags] [file]    one line per trace in the file
  spanfall stats    [flags] [file]    per-operation totals, self time, errors
  spanfall version                    print the version

file is OTLP JSON, Jaeger JSON, or OTLP JSON Lines; "-" or no file reads stdin.

Display flags (all subcommands):
  --width N              output width in columns (default 100, minimum 60)
  --color MODE           auto (default), always, or never
  --ascii                7-bit ASCII output for legacy pipelines

Render flags:
  --trace ID             select one trace by ID prefix
  --all                  render every trace in the file
  --max-depth N          hide spans nested deeper than N levels
  --min-duration DUR     hide spans shorter than DUR (e.g. 5ms)
  --fail-on-error        exit 1 if the rendered trace contains error spans

Critical / list / stats flags:
  --format FORMAT        text (default) or json
  --trace ID             select one trace by ID prefix (critical, stats)

Exit codes: 0 ok · 1 --fail-on-error breach · 2 usage error · 3 runtime error
`, version.Version)
}
