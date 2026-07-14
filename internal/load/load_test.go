// Tests for format detection and dispatch: OTLP objects, Jaeger exports,
// collector JSON Lines, top-level arrays, and the error messages users see
// when they pipe in something else entirely.
package load

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const otlpDoc = `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},"scopeSpans":[{"spans":[{"traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","name":"op","startTimeUnixNano":"1","endTimeUnixNano":"2"}]}]}]}`

const jaegerDoc = `{"data":[{"traceID":"c3d4e5f6a7b8091a2b3c4d5e6f708192","spans":[{"spanID":"0102030405060708","operationName":"op","startTime":1,"duration":1,"processID":"p1"}],"processes":{"p1":{"serviceName":"web"}}}]}`

func TestDetectFormats(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Format
	}{
		{"otlp", otlpDoc, FormatOTLP},
		{"otlp snake_case", `{"resource_spans":[]}`, FormatOTLP},
		{"jaeger", jaegerDoc, FormatJaeger},
		{"jsonl", otlpDoc + "\n" + otlpDoc + "\n", FormatJSONL},
		{"array", "[" + otlpDoc + "," + jaegerDoc + "]", FormatArray},
		{"garbage", "not json at all", FormatUnknown},
		{"empty", "  \n ", FormatUnknown},
	}
	for _, c := range cases {
		if got := Detect([]byte(c.in)); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseJSONLMergesLines(t *testing.T) {
	// Blank lines between exports are common when files are concatenated.
	payload := otlpDoc + "\n\n" + otlpDoc + "\n"
	spans, err := Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(spans))
	}
}

func TestParseJSONLReportsFailingLineNumber(t *testing.T) {
	payload := otlpDoc + "\n" + `{"broken":` + "\n" + otlpDoc
	_, err := Parse([]byte(payload))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want line-2 error, got %v", err)
	}
}

func TestParseArrayMixesFormats(t *testing.T) {
	spans, err := Parse([]byte("[" + otlpDoc + "," + jaegerDoc + "]"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("want 2 spans, got %d", len(spans))
	}
	if spans[0].Service != "api" || spans[1].Service != "web" {
		t.Fatalf("wrong services: %q %q", spans[0].Service, spans[1].Service)
	}
}

func TestParseEmptyInputExplains(t *testing.T) {
	_, err := Parse([]byte("  \n "))
	if err == nil || !strings.Contains(err.Error(), "empty input") {
		t.Fatalf("got %v", err)
	}
}

func TestParseUnrecognizedJSONExplainsExpectedShapes(t *testing.T) {
	_, err := Parse([]byte(`{"metrics":[1,2,3]}`))
	if err == nil || !strings.Contains(err.Error(), "resourceSpans") || !strings.Contains(err.Error(), "Jaeger") {
		t.Fatalf("error should name the expected shapes, got %v", err)
	}
}

func TestReadFileFromDiskAndMissingPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.json")
	if err := os.WriteFile(path, []byte(otlpDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	spans, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	if _, err := ReadFile(filepath.Join(dir, "nope.json")); err == nil {
		t.Fatal("want error for missing file")
	}
}
