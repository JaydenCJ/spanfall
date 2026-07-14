// In-process integration tests for the CLI: Run(argv, stdout, stderr) is
// exercised exactly like the binary, against the shipped example files and
// small fabricated fixtures, asserting on real output and exit codes.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkout / collector / jaegerLogin are the shipped example files, so the
// test suite doubles as a guarantee that the examples stay valid.
const (
	checkout    = "../../examples/checkout-trace.json"
	collector   = "../../examples/collector.jsonl"
	jaegerLogin = "../../examples/jaeger-login.json"
)

// run executes the CLI in-process and captures both streams.
func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// writeFixture drops a payload into a temp file and returns its path.
func writeFixture(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.json")
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// twoTraces share the prefix "aa" so --trace disambiguation is testable.
const twoTraces = `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},"scopeSpans":[{"spans":[
	{"traceId":"aa000000000000000000000000000001","spanId":"0000000000000001","name":"first","startTimeUnixNano":"1000","endTimeUnixNano":"2000"},
	{"traceId":"aa000000000000000000000000000002","spanId":"0000000000000002","name":"second","startTimeUnixNano":"3000","endTimeUnixNano":"4000"}
]}]}]}`

func TestVersionCommand(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || out != "spanfall 0.1.0\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code2, out2, _ := run(t, "--version")
	if code2 != ExitOK || out2 != out {
		t.Fatalf("--version differs: %q", out2)
	}
}

func TestHelpListsSubcommandsAndExitCodes(t *testing.T) {
	code, out, _ := run(t, "help")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"render", "critical", "list", "stats", "Exit codes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestRenderCheckoutExample(t *testing.T) {
	code, out, _ := run(t, "render", checkout)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{
		"trace 4bf92f3577b34da6a3ce929d0e0e4736",
		"GET /api/checkout",
		"187.5ms",
		"└─ payment.charge",
		"critical path: 9 of 12 spans",
		"✗", // the failed card.authorize attempt
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestBarePathDefaultsToRender(t *testing.T) {
	code, out, _ := run(t, checkout)
	if code != ExitOK || !strings.Contains(out, "critical path:") {
		t.Fatalf("code=%d out:\n%s", code, out)
	}
}

func TestRenderJaegerExample(t *testing.T) {
	code, out, _ := run(t, "render", jaegerLogin)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "POST /login") || !strings.Contains(out, "2 services") {
		t.Fatalf("jaeger render wrong:\n%s", out)
	}
}

func TestRenderMultiTraceWarnsAndRendersFirst(t *testing.T) {
	code, out, errOut := run(t, "render", collector)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(errOut, "2 traces") {
		t.Fatalf("stderr should mention trace count: %q", errOut)
	}
	if !strings.Contains(out, "GET /healthz") || strings.Contains(out, "cron.digest") {
		t.Fatalf("should render only the first trace:\n%s", out)
	}
}

func TestRenderAllRendersEveryTrace(t *testing.T) {
	code, out, _ := run(t, "render", "--all", collector)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "GET /healthz") || !strings.Contains(out, "cron.digest") {
		t.Fatalf("--all missing a trace:\n%s", out)
	}
}

func TestRenderTraceSelectionByPrefix(t *testing.T) {
	path := writeFixture(t, twoTraces)
	// An unambiguous prefix selects exactly one trace.
	code, out, _ := run(t, "render", "--trace", "aa000000000000000000000000000002", path)
	if code != ExitOK || !strings.Contains(out, "second") || strings.Contains(out, "first") {
		t.Fatalf("code=%d out:\n%s", code, out)
	}
	// An ambiguous prefix is a usage error that lists the candidates.
	code, _, errOut := run(t, "render", "--trace", "aa", path)
	if code != ExitUsage || !strings.Contains(errOut, "ambiguous") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	// A prefix matching nothing is a runtime error.
	code, _, errOut = run(t, "render", "--trace", "ffff", path)
	if code != ExitRuntime || !strings.Contains(errOut, "no trace matches") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestRenderFiltersMinDurationAndDepth(t *testing.T) {
	code, out, _ := run(t, "render", "--min-duration", "30ms", checkout)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(out, "cache.get") { // 1.7ms, must be hidden
		t.Fatalf("--min-duration leaked a short span:\n%s", out)
	}
	if !strings.Contains(out, "hidden") {
		t.Fatalf("hidden note missing:\n%s", out)
	}
	code, out, _ = run(t, "render", "--max-depth", "1", checkout)
	if code != ExitOK || strings.Contains(out, "fraud.screen") {
		t.Fatalf("--max-depth leaked a deep span:\n%s", out)
	}
}

func TestFailOnErrorGatesExitCode(t *testing.T) {
	code, _, errOut := run(t, "render", "--fail-on-error", checkout)
	if code != ExitFail || !strings.Contains(errOut, "error span") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	// A clean trace passes the same gate.
	code, _, _ = run(t, "render", "--fail-on-error", jaegerLogin)
	if code != ExitOK {
		t.Fatalf("clean trace should pass: code=%d", code)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := [][]string{
		{"render", "--color", "sometimes", checkout},
		{"render", "--width", "10", checkout},
		{"render", "--min-duration", "fast", checkout},
		{"render", "--all", "--trace", "aa", checkout},
		{"list", "--format", "yaml", collector},
		{"render", checkout, "extra-arg"},
	}
	for _, args := range cases {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Fatalf("%v: code=%d, want %d", args, code, ExitUsage)
		}
	}
}

// A bare word that is neither a subcommand nor a file is almost always a
// typo'd command; the CLI must say so and point at --help instead of
// surfacing a raw OS "no such file" error.
func TestUnknownCommandSuggestsHelp(t *testing.T) {
	code, _, errOut := run(t, "rendr")
	if code != ExitUsage {
		t.Fatalf("code=%d, want %d", code, ExitUsage)
	}
	if !strings.Contains(errOut, "rendr") || !strings.Contains(errOut, "--help") {
		t.Fatalf("stderr should name the typo and suggest --help: %q", errOut)
	}
	// An existing bare path must still render (covered fully elsewhere).
	if code, _, _ := run(t, checkout); code != ExitOK {
		t.Fatalf("bare existing path regressed: code=%d", code)
	}
}

func TestMissingAndMalformedFilesExitThree(t *testing.T) {
	code, _, errOut := run(t, "render", filepath.Join(t.TempDir(), "nope.json"))
	if code != ExitRuntime || errOut == "" {
		t.Fatalf("missing file: code=%d stderr=%q", code, errOut)
	}
	bad := writeFixture(t, `{"metrics":[]}`)
	code, _, errOut = run(t, "render", bad)
	if code != ExitRuntime || !strings.Contains(errOut, "resourceSpans") {
		t.Fatalf("malformed file: code=%d stderr=%q", code, errOut)
	}
}

func TestListAndStatsSubcommands(t *testing.T) {
	code, out, _ := run(t, "list", collector)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "0af7651916cd43dd8448eb211c80319c") ||
		!strings.Contains(out, "9d4c2b1a0f8e7d6c5b4a39281706f5e4") {
		t.Fatalf("list missing traces:\n%s", out)
	}
	code, out, _ = run(t, "stats", checkout)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "card.authorize") || !strings.Contains(out, "91.2ms") {
		t.Fatalf("stats missing aggregate row:\n%s", out)
	}
}

func TestCriticalJSONIsMachineReadable(t *testing.T) {
	code, out, _ := run(t, "critical", "--format", "json", checkout)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var doc struct {
		Tool      string `json:"tool"`
		TraceID   string `json:"trace_id"`
		PathSpans int    `json:"path_span_count"`
		Path      []struct {
			Name   string `json:"name"`
			SelfNS int64  `json:"self_ns"`
		} `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Tool != "spanfall" || doc.PathSpans != 9 || len(doc.Path) != 9 {
		t.Fatalf("doc: %+v", doc)
	}
	var total int64
	for _, e := range doc.Path {
		total += e.SelfNS
	}
	if total != 187_500_000 { // self times must tile the whole trace
		t.Fatalf("path self sum: %d", total)
	}
}
