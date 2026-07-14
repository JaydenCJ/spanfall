// Package load reads a trace file (or stdin), detects its format, and
// returns normalized spans. Three shapes are recognized:
//
//   - OTLP/JSON: one ExportTraceServiceRequest object ({"resourceSpans": …})
//   - Jaeger JSON: a UI export or /api/traces response ({"data": [ … ]})
//   - OTLP JSON Lines: one export object per line, as written by the
//     OpenTelemetry Collector file exporter
//
// A top-level JSON array of any mix of the first two is also accepted.
package load

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/JaydenCJ/spanfall/internal/jaeger"
	"github.com/JaydenCJ/spanfall/internal/model"
	"github.com/JaydenCJ/spanfall/internal/otlp"
)

// Format identifies a detected input shape.
type Format string

const (
	FormatOTLP    Format = "otlp"
	FormatJaeger  Format = "jaeger"
	FormatJSONL   Format = "jsonl"
	FormatArray   Format = "array"
	FormatUnknown Format = "unknown"
)

// probe mirrors just the discriminating top-level keys.
type probe struct {
	ResourceSpans      json.RawMessage `json:"resourceSpans"`
	ResourceSpansSnake json.RawMessage `json:"resource_spans"`
	Data               json.RawMessage `json:"data"`
}

// Detect classifies raw bytes without fully parsing spans.
func Detect(data []byte) Format {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return FormatUnknown
	}
	if trimmed[0] == '[' {
		if json.Valid(trimmed) {
			return FormatArray
		}
		return FormatUnknown
	}
	var p probe
	if err := json.Unmarshal(trimmed, &p); err == nil {
		switch {
		case p.ResourceSpans != nil || p.ResourceSpansSnake != nil:
			return FormatOTLP
		case p.Data != nil:
			return FormatJaeger
		default:
			return FormatUnknown
		}
	}
	// Not one JSON document: JSONL if every non-blank line is an object.
	if looksLikeJSONL(trimmed) {
		return FormatJSONL
	}
	return FormatUnknown
}

func looksLikeJSONL(data []byte) bool {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	lines := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' {
			return false
		}
		lines++
	}
	return lines > 1
}

// Parse detects the format and returns normalized spans.
func Parse(data []byte) ([]*model.Span, error) {
	switch Detect(data) {
	case FormatOTLP:
		return otlp.Parse(data)
	case FormatJaeger:
		return jaeger.Parse(data)
	case FormatArray:
		return parseArray(data)
	case FormatJSONL:
		return parseJSONL(data)
	default:
		if len(bytes.TrimSpace(data)) == 0 {
			return nil, fmt.Errorf("empty input: expected OTLP JSON, Jaeger JSON, or OTLP JSON Lines")
		}
		return nil, fmt.Errorf("unrecognized input: expected OTLP JSON (resourceSpans), Jaeger JSON (data), or OTLP JSON Lines")
	}
}

func parseArray(data []byte) ([]*model.Span, error) {
	var elems []json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &elems); err != nil {
		return nil, fmt.Errorf("invalid JSON array: %w", err)
	}
	var spans []*model.Span
	for i, el := range elems {
		got, err := parseObject(el)
		if err != nil {
			return nil, fmt.Errorf("array element %d: %w", i+1, err)
		}
		spans = append(spans, got...)
	}
	return spans, nil
}

func parseJSONL(data []byte) ([]*model.Span, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	var spans []*model.Span
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		got, err := parseObject(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		spans = append(spans, got...)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

// parseObject handles one object that must be either OTLP or Jaeger shaped.
func parseObject(data []byte) ([]*model.Span, error) {
	var p probe
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if p.ResourceSpans != nil || p.ResourceSpansSnake != nil {
		return otlp.Parse(data)
	}
	if p.Data != nil {
		return jaeger.Parse(data)
	}
	return nil, fmt.Errorf("object has neither resourceSpans nor data")
}

// ReadFile loads spans from a path, with "-" meaning stdin.
func ReadFile(path string) ([]*model.Span, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}
