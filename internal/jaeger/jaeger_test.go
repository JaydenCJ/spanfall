// Tests for the Jaeger JSON decoder: microsecond→nanosecond conversion,
// process table resolution, references, and the tag conventions that carry
// kind and status in Jaeger land.
package jaeger

import (
	"testing"

	"github.com/JaydenCJ/spanfall/internal/model"
)

// export builds a one-trace Jaeger document around the given span bodies.
func export(spans string) string {
	return `{"data":[{"traceID":"c3d4e5f6a7b8091a2b3c4d5e6f708192","spans":[` + spans + `],"processes":{"p1":{"serviceName":"web"},"p2":{"serviceName":"auth"}}}]}`
}

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

func TestParseBasicSpanAndMicrosecondConversion(t *testing.T) {
	s := one(t, export(`{"spanID":"0102030405060708","operationName":"POST /login","startTime":1771104600000000,"duration":95000,"processID":"p1"}`))
	if s.Name != "POST /login" || s.Service != "web" {
		t.Fatalf("fields: %+v", s)
	}
	if s.TraceID != "c3d4e5f6a7b8091a2b3c4d5e6f708192" {
		t.Fatalf("traceID should come from the enclosing trace: %q", s.TraceID)
	}
	// Jaeger times are microseconds; the model is nanoseconds.
	if s.Start != 1771104600000000000 || s.Duration() != 95000000 {
		t.Fatalf("times: start=%d dur=%d", s.Start, s.Duration())
	}
}

func TestParseChildOfReferenceBecomesParent(t *testing.T) {
	s := one(t, export(`{"spanID":"1112131415161718","references":[{"refType":"CHILD_OF","spanID":"0102030405060708"}],"startTime":1,"duration":1,"processID":"p2"}`))
	if s.ParentID != "0102030405060708" {
		t.Fatalf("parent: %q", s.ParentID)
	}
}

func TestParseFollowsFromUsedWhenNoChildOf(t *testing.T) {
	// FOLLOWS_FROM is still a causal parent; without it the span would
	// float as a second root and wreck the waterfall.
	s := one(t, export(`{"spanID":"1112131415161718","references":[{"refType":"FOLLOWS_FROM","spanID":"0102030405060708"}],"startTime":1,"duration":1,"processID":"p2"}`))
	if s.ParentID != "0102030405060708" {
		t.Fatalf("parent: %q", s.ParentID)
	}
}

func TestParseUnknownProcessFallsBackToUnknown(t *testing.T) {
	s := one(t, export(`{"spanID":"0102030405060708","startTime":1,"duration":1,"processID":"p9"}`))
	if s.Service != "unknown" {
		t.Fatalf("service: %q", s.Service)
	}
}

func TestParseStatusTags(t *testing.T) {
	// error=true (classic Jaeger) and otel.status_code=ERROR (OTel bridge)
	// both mean error; a weird exporter emitting both error=true and
	// otel.status_code=OK must stay an error regardless of tag order.
	boolErr := one(t, export(`{"spanID":"0102030405060708","startTime":1,"duration":1,"processID":"p1","tags":[{"key":"error","type":"bool","value":true}]}`))
	if !boolErr.IsError() {
		t.Fatalf("error tag: %q", boolErr.Status)
	}
	otelErr := one(t, export(`{"spanID":"0102030405060708","startTime":1,"duration":1,"processID":"p1","tags":[{"key":"otel.status_code","type":"string","value":"ERROR"},{"key":"otel.status_description","type":"string","value":"timeout"}]}`))
	if !otelErr.IsError() || otelErr.StatusMsg != "timeout" {
		t.Fatalf("otel tags: %q msg %q", otelErr.Status, otelErr.StatusMsg)
	}
	conflict := one(t, export(`{"spanID":"0102030405060708","startTime":1,"duration":1,"processID":"p1","tags":[{"key":"error","type":"bool","value":true},{"key":"otel.status_code","type":"string","value":"OK"}]}`))
	if !conflict.IsError() {
		t.Fatalf("OK must not override error: %q", conflict.Status)
	}
}

func TestParseTagsLiftKindAndKeepAttrs(t *testing.T) {
	s := one(t, export(`{"spanID":"0102030405060708","startTime":1,"duration":1,"processID":"p1","tags":[{"key":"span.kind","type":"string","value":"client"},{"key":"http.status_code","type":"int64","value":200},{"key":"sampler.param","type":"float64","value":0.5}]}`))
	if s.Kind != model.KindClient {
		t.Fatalf("kind: %q", s.Kind)
	}
	if s.Attrs["http.status_code"] != "200" || s.Attrs["sampler.param"] != "0.5" {
		t.Fatalf("tags: %+v", s.Attrs)
	}
	if _, leaked := s.Attrs["span.kind"]; leaked {
		t.Fatal("span.kind must be lifted, not duplicated into attrs")
	}
}

func TestParseMultipleTracesInOneDocument(t *testing.T) {
	payload := `{"data":[
		{"traceID":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","spans":[{"spanID":"0102030405060708","startTime":1,"duration":1,"processID":"p1"}],"processes":{"p1":{"serviceName":"a"}}},
		{"traceID":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","spans":[{"spanID":"1112131415161718","startTime":2,"duration":1,"processID":"p1"}],"processes":{"p1":{"serviceName":"b"}}}
	]}`
	spans, err := Parse([]byte(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(spans) != 2 || spans[0].TraceID == spans[1].TraceID {
		t.Fatalf("got %d spans: %+v", len(spans), spans)
	}
	if spans[0].Service != "a" || spans[1].Service != "b" {
		t.Fatal("per-trace process tables must not leak across traces")
	}
}

func TestParseMissingDataFails(t *testing.T) {
	if _, err := Parse([]byte(`{"resourceSpans":[]}`)); err == nil {
		t.Fatal("want error when data array is absent")
	}
}
