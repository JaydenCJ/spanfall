// Tests for the OTLP/JSON decoder. The cases mirror the real variance
// between producers: collector output (camelCase, string nanos, numeric
// enums), protojson output (base64 IDs), SDKs that emit enum names, and
// the pre-1.0 instrumentationLibrarySpans field.
package otlp

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/spanfall/internal/model"
)

// doc wraps one span JSON body in a minimal OTLP envelope.
func doc(spanJSON string) string {
	return `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"api"}}]},"scopeSpans":[{"spans":[` + spanJSON + `]}]}]}`
}

const baseSpan = `{"traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","name":"GET /x","kind":2,"startTimeUnixNano":"1770112800000000000","endTimeUnixNano":"1770112800187500000","status":{"code":2,"message":"boom"}}`

func one(t *testing.T, payload string) *model.Span {
	t.Helper()
	spans, err := Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	return spans[0]
}

func TestParseBasicSpanFields(t *testing.T) {
	s := one(t, doc(baseSpan))
	if s.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || s.SpanID != "00f067aa0ba902b7" {
		t.Fatalf("ids wrong: %+v", s)
	}
	if s.Name != "GET /x" || s.Service != "api" || s.Kind != model.KindServer {
		t.Fatalf("fields wrong: %+v", s)
	}
	if s.Start != 1770112800000000000 || s.Duration() != 187500000 {
		t.Fatalf("times wrong: start=%d dur=%d", s.Start, s.Duration())
	}
	if !s.IsError() || s.StatusMsg != "boom" {
		t.Fatalf("status wrong: %+v", s)
	}
}

func TestParseNanosecondPrecisionSurvivesNumericTimestamps(t *testing.T) {
	// 1770112800000000001 exceeds float64's 2^53 integer range: a decoder
	// without UseNumber would round it and corrupt every span offset.
	span := `{"spanId":"00f067aa0ba902b7","startTimeUnixNano":1770112800000000001,"endTimeUnixNano":1770112800000000003}`
	s := one(t, doc(span))
	if s.Start != 1770112800000000001 || s.End != 1770112800000000003 {
		t.Fatalf("precision lost: start=%d end=%d", s.Start, s.End)
	}
}

func TestParseIDNormalizationHexAndBase64(t *testing.T) {
	// protojson encodes bytes fields as base64 (16-byte trace, 8-byte
	// span); the collector emits hex, sometimes uppercase. All must land
	// on the same canonical lowercase hex or parent links break.
	b64 := one(t, doc(`{"traceId":"S/kvNXezTaajzpKdDg5HNg==","spanId":"APBnqgupArc=","startTimeUnixNano":"1","endTimeUnixNano":"2"}`))
	up := one(t, doc(`{"traceId":"4BF92F3577B34DA6A3CE929D0E0E4736","spanId":"00F067AA0BA902B7","startTimeUnixNano":"1","endTimeUnixNano":"2"}`))
	for _, s := range []*model.Span{b64, up} {
		if s.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || s.SpanID != "00f067aa0ba902b7" {
			t.Fatalf("not normalized: trace=%q span=%q", s.TraceID, s.SpanID)
		}
	}
}

func TestParseAllZeroParentMeansRoot(t *testing.T) {
	span := `{"spanId":"00f067aa0ba902b7","parentSpanId":"0000000000000000","startTimeUnixNano":"1","endTimeUnixNano":"2"}`
	if s := one(t, doc(span)); s.ParentID != "" {
		t.Fatalf("all-zero parent should normalize to empty, got %q", s.ParentID)
	}
}

func TestParseEnumStringForms(t *testing.T) {
	// Some SDKs serialize enums as their proto names instead of numbers;
	// a missing status must stay "unset", not become an error or an ok.
	span := `{"spanId":"00f067aa0ba902b7","kind":"SPAN_KIND_CLIENT","startTimeUnixNano":"1","endTimeUnixNano":"2","status":{"code":"STATUS_CODE_ERROR"}}`
	s := one(t, doc(span))
	if s.Kind != model.KindClient || !s.IsError() {
		t.Fatalf("enum strings not accepted: kind=%q status=%q", s.Kind, s.Status)
	}
	noStatus := one(t, doc(`{"spanId":"00f067aa0ba902b7","startTimeUnixNano":"1","endTimeUnixNano":"2"}`))
	if noStatus.Status != model.StatusUnset {
		t.Fatalf("missing status: %q", noStatus.Status)
	}
}

func TestParseAlternateFieldSpellings(t *testing.T) {
	// snake_case keys (protojson input convention) and the pre-1.0
	// instrumentationLibrarySpans field are both still in the wild.
	snake := one(t, `{"resource_spans":[{"resource":{"attributes":[{"key":"service.name","value":{"string_value":"snake"}}]},"scope_spans":[{"spans":[{"span_id":"00f067aa0ba902b7","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","name":"op","start_time_unix_nano":"5","end_time_unix_nano":"9"}]}]}]}`)
	if snake.Service != "snake" || snake.Start != 5 || snake.End != 9 {
		t.Fatalf("snake_case not accepted: %+v", snake)
	}
	legacy := one(t, `{"resourceSpans":[{"resource":{},"instrumentationLibrarySpans":[{"spans":[{"spanId":"00f067aa0ba902b7","name":"old","startTimeUnixNano":"1","endTimeUnixNano":"2"}]}]}]}`)
	if legacy.Name != "old" {
		t.Fatalf("legacy field not read: %+v", legacy)
	}
}

func TestParseMissingServiceNameFallsBackToUnknown(t *testing.T) {
	payload := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"spanId":"00f067aa0ba902b7","startTimeUnixNano":"1","endTimeUnixNano":"2"}]}]}]}`
	if s := one(t, payload); s.Service != "unknown" {
		t.Fatalf("service: %q", s.Service)
	}
}

func TestParseAttributeValueTypes(t *testing.T) {
	span := `{"spanId":"00f067aa0ba902b7","startTimeUnixNano":"1","endTimeUnixNano":"2","attributes":[
		{"key":"s","value":{"stringValue":"hello"}},
		{"key":"i","value":{"intValue":"42"}},
		{"key":"d","value":{"doubleValue":1.5}},
		{"key":"b","value":{"boolValue":true}},
		{"key":"a","value":{"arrayValue":{"values":[{"stringValue":"x"},{"intValue":"7"}]}}},
		{"key":"kv","value":{"kvlistValue":{"values":[{"key":"z","value":{"stringValue":"1"}},{"key":"a","value":{"stringValue":"2"}}]}}}
	]}`
	s := one(t, doc(span))
	want := map[string]string{
		"s": "hello", "i": "42", "d": "1.5", "b": "true",
		"a": "[x, 7]", "kv": "{a=2, z=1}", // kvlist keys sorted for determinism
	}
	for k, v := range want {
		if s.Attrs[k] != v {
			t.Fatalf("attr %s: got %q want %q", k, s.Attrs[k], v)
		}
	}
}

func TestParseEventsCounted(t *testing.T) {
	span := `{"spanId":"00f067aa0ba902b7","startTimeUnixNano":"1","endTimeUnixNano":"2","events":[{"name":"e1"},{"name":"e2"}]}`
	if s := one(t, doc(span)); s.Events != 2 {
		t.Fatalf("events: %d", s.Events)
	}
}

func TestParseSpanWithoutIDDropped(t *testing.T) {
	payload := doc(`{"name":"ghost","startTimeUnixNano":"1","endTimeUnixNano":"2"}`)
	spans, err := Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf("span without ID should be dropped, got %d", len(spans))
	}
}

func TestParseLenientDefaults(t *testing.T) {
	// An in-flight span exported before it ended still renders (zero
	// width), and a nameless span gets a visible placeholder.
	s := one(t, doc(`{"spanId":"00f067aa0ba902b7","startTimeUnixNano":"77"}`))
	if s.End != 77 || s.Duration() != 0 {
		t.Fatalf("end=%d dur=%d", s.End, s.Duration())
	}
	if s.Name != "(unnamed span)" {
		t.Fatalf("name: %q", s.Name)
	}
}

func TestParseRejectsNonOTLPDocuments(t *testing.T) {
	if _, err := Parse([]byte("{nope")); err == nil {
		t.Fatal("want error for invalid JSON")
	}
	_, err := Parse([]byte(`{"hello":"world"}`))
	if err == nil || !strings.Contains(err.Error(), "resourceSpans") {
		t.Fatalf("want resourceSpans error, got %v", err)
	}
}
