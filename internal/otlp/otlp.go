// Package otlp decodes OTLP/JSON trace payloads (ExportTraceServiceRequest)
// into model spans. It is deliberately lenient about the encoding details
// that vary between producers: camelCase vs snake_case keys, hex vs base64
// IDs, string vs number timestamps and enums, and the pre-1.0
// instrumentationLibrarySpans field name.
package otlp

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/JaydenCJ/spanfall/internal/model"
)

// Parse decodes one OTLP/JSON export payload into spans. It returns an
// error only when the document is not JSON or has no resourceSpans at all;
// individual malformed spans are skipped rather than failing the file.
func Parse(data []byte) ([]*model.Span, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// UseNumber matters: Unix-nanosecond timestamps exceed 2^53, so decoding
	// them through float64 would silently corrupt span offsets.
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("otlp: invalid JSON: %w", err)
	}
	rss := arr(doc, "resourceSpans", "resource_spans")
	if rss == nil {
		return nil, fmt.Errorf("otlp: no resourceSpans in document")
	}
	var spans []*model.Span
	for _, rsv := range rss {
		rs, ok := rsv.(map[string]any)
		if !ok {
			continue
		}
		service := serviceName(obj(rs, "resource"))
		scopes := arr(rs, "scopeSpans", "scope_spans",
			"instrumentationLibrarySpans", "instrumentation_library_spans")
		for _, ssv := range scopes {
			ss, ok := ssv.(map[string]any)
			if !ok {
				continue
			}
			for _, spv := range arr(ss, "spans") {
				sp, ok := spv.(map[string]any)
				if !ok {
					continue
				}
				if s := parseSpan(sp, service); s != nil {
					spans = append(spans, s)
				}
			}
		}
	}
	return spans, nil
}

// parseSpan converts one span object; nil means the span lacks the minimum
// viable fields (an ID and a start time) and is dropped.
func parseSpan(sp map[string]any, service string) *model.Span {
	s := &model.Span{
		TraceID:  ID(str(sp, "traceId", "trace_id")),
		SpanID:   ID(str(sp, "spanId", "span_id")),
		ParentID: ID(str(sp, "parentSpanId", "parent_span_id")),
		Name:     str(sp, "name"),
		Service:  service,
		Attrs:    attrMap(arr(sp, "attributes")),
		Events:   len(arr(sp, "events")),
	}
	if s.SpanID == "" {
		return nil
	}
	start, okStart := i64(sp, "startTimeUnixNano", "start_time_unix_nano")
	end, okEnd := i64(sp, "endTimeUnixNano", "end_time_unix_nano")
	if !okStart {
		return nil
	}
	if !okEnd {
		end = start
	}
	s.Start, s.End = start, end
	s.Kind = kind(sp["kind"])
	s.Status, s.StatusMsg = status(obj(sp, "status"))
	if s.Name == "" {
		s.Name = "(unnamed span)"
	}
	return s
}

// ID canonicalizes a trace or span ID to lowercase hex. OTLP/JSON specifies
// hex, but protojson-based producers emit base64 for bytes fields, so both
// are accepted. All-zero IDs (the wire form of "no parent") become "".
func ID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if isHex(raw) && (len(raw) == 32 || len(raw) == 16) {
		low := strings.ToLower(raw)
		if strings.Trim(low, "0") == "" {
			return ""
		}
		return low
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && (len(b) == 16 || len(b) == 8) {
		if allZero(b) {
			return ""
		}
		return hex.EncodeToString(b)
	}
	// Unknown shape: keep it verbatim (lowercased) so relationships still
	// resolve as long as the producer is self-consistent.
	return strings.ToLower(raw)
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return len(s) > 0
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// kind accepts the enum as a JSON number or a SPAN_KIND_* string.
func kind(v any) string {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return model.KindFromEnum(n)
		}
	case string:
		return strings.ToLower(strings.TrimPrefix(t, "SPAN_KIND_"))
	}
	return model.KindUnspecified
}

// status accepts the code as a JSON number or a STATUS_CODE_* string.
func status(st map[string]any) (code, msg string) {
	if st == nil {
		return model.StatusUnset, ""
	}
	msg = str(st, "message")
	switch t := st["code"].(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return model.StatusFromEnum(n), msg
		}
	case string:
		return strings.ToLower(strings.TrimPrefix(t, "STATUS_CODE_")), msg
	}
	return model.StatusUnset, msg
}

// serviceName pulls resource attribute service.name; unknown stays visible
// rather than silently blank.
func serviceName(resource map[string]any) string {
	attrs := attrMap(arr(resource, "attributes"))
	if v, ok := attrs["service.name"]; ok && v != "" {
		return v
	}
	return "unknown"
}

// attrMap flattens an OTLP KeyValue list into string->string. Values keep a
// readable rendering of their type (arrays and kvlists are summarized).
func attrMap(kvs []any) map[string]string {
	if len(kvs) == 0 {
		return nil
	}
	out := make(map[string]string, len(kvs))
	for _, kvv := range kvs {
		kv, ok := kvv.(map[string]any)
		if !ok {
			continue
		}
		key := str(kv, "key")
		if key == "" {
			continue
		}
		out[key] = anyValue(obj(kv, "value"))
	}
	return out
}

// anyValue renders an OTLP AnyValue as a string.
func anyValue(v map[string]any) string {
	if v == nil {
		return ""
	}
	if s, ok := v["stringValue"].(string); ok {
		return s
	}
	if s, ok := v["string_value"].(string); ok {
		return s
	}
	for _, k := range []string{"intValue", "int_value"} {
		switch t := v[k].(type) {
		case json.Number:
			return t.String()
		case string:
			return t
		}
	}
	for _, k := range []string{"doubleValue", "double_value"} {
		switch t := v[k].(type) {
		case json.Number:
			return t.String()
		case string:
			return t
		}
	}
	for _, k := range []string{"boolValue", "bool_value"} {
		if b, ok := v[k].(bool); ok {
			return strconv.FormatBool(b)
		}
	}
	for _, k := range []string{"bytesValue", "bytes_value"} {
		if s, ok := v[k].(string); ok {
			return s
		}
	}
	if av := obj(v, "arrayValue", "array_value"); av != nil {
		var parts []string
		for _, ev := range arr(av, "values") {
			if em, ok := ev.(map[string]any); ok {
				parts = append(parts, anyValue(em))
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	if kl := obj(v, "kvlistValue", "kvlist_value"); kl != nil {
		m := attrMap(arr(kl, "values"))
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+m[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

// --- lenient map accessors -------------------------------------------------

func arr(m map[string]any, keys ...string) []any {
	for _, k := range keys {
		if v, ok := m[k].([]any); ok {
			return v
		}
	}
	return nil
}

func obj(m map[string]any, keys ...string) map[string]any {
	if m == nil {
		return nil
	}
	for _, k := range keys {
		if v, ok := m[k].(map[string]any); ok {
			return v
		}
	}
	return nil
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

// i64 reads an integer that OTLP/JSON may encode as a string ("176...")
// or a JSON number; ok is false when the field is absent or unparseable.
func i64(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		switch t := m[k].(type) {
		case string:
			if n, err := strconv.ParseInt(t, 10, 64); err == nil {
				return n, true
			}
		case json.Number:
			if n, err := t.Int64(); err == nil {
				return n, true
			}
			if f, err := t.Float64(); err == nil {
				return int64(f), true
			}
		}
	}
	return 0, false
}
