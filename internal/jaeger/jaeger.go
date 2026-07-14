// Package jaeger decodes the JSON that Jaeger's UI "Download JSON" button
// and its /api/traces endpoint produce, and normalizes it into model spans.
// Timestamps are microseconds there, span kind and status live in tags, and
// service names are resolved through the per-trace process table.
package jaeger

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JaydenCJ/spanfall/internal/model"
	"github.com/JaydenCJ/spanfall/internal/otlp"
)

type document struct {
	Data []traceJSON `json:"data"`
}

type traceJSON struct {
	TraceID   string                 `json:"traceID"`
	Spans     []spanJSON             `json:"spans"`
	Processes map[string]processJSON `json:"processes"`
}

type spanJSON struct {
	TraceID       string    `json:"traceID"`
	SpanID        string    `json:"spanID"`
	ParentSpanID  string    `json:"parentSpanID"` // some exporters inline it
	OperationName string    `json:"operationName"`
	References    []refJSON `json:"references"`
	StartTime     int64     `json:"startTime"` // Unix microseconds
	Duration      int64     `json:"duration"`  // microseconds
	Tags          []tagJSON `json:"tags"`
	ProcessID     string    `json:"processID"`
}

type refJSON struct {
	RefType string `json:"refType"`
	SpanID  string `json:"spanID"`
}

type tagJSON struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type processJSON struct {
	ServiceName string `json:"serviceName"`
}

// Parse decodes a Jaeger JSON export. Every trace under "data" contributes
// its spans; the caller groups them back by trace ID.
func Parse(data []byte) ([]*model.Span, error) {
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("jaeger: invalid JSON: %w", err)
	}
	if doc.Data == nil {
		return nil, fmt.Errorf("jaeger: no data array in document")
	}
	var spans []*model.Span
	for _, tr := range doc.Data {
		for _, js := range tr.Spans {
			if s := convert(js, tr); s != nil {
				spans = append(spans, s)
			}
		}
	}
	return spans, nil
}

func convert(js spanJSON, tr traceJSON) *model.Span {
	spanID := otlp.ID(js.SpanID)
	if spanID == "" {
		return nil
	}
	traceID := js.TraceID
	if traceID == "" {
		traceID = tr.TraceID
	}
	s := &model.Span{
		TraceID:  otlp.ID(traceID),
		SpanID:   spanID,
		ParentID: parentID(js),
		Name:     js.OperationName,
		Service:  "unknown",
		Kind:     model.KindUnspecified,
		Start:    js.StartTime * 1000,
		End:      (js.StartTime + js.Duration) * 1000,
		Status:   model.StatusUnset,
	}
	if p, ok := tr.Processes[js.ProcessID]; ok && p.ServiceName != "" {
		s.Service = p.ServiceName
	}
	if s.Name == "" {
		s.Name = "(unnamed span)"
	}
	applyTags(s, js.Tags)
	return s
}

// parentID prefers a CHILD_OF reference, falls back to FOLLOWS_FROM (still
// a causal parent for tree purposes), then the inline parentSpanID field.
func parentID(js spanJSON) string {
	for _, r := range js.References {
		if strings.EqualFold(r.RefType, "CHILD_OF") {
			return otlp.ID(r.SpanID)
		}
	}
	for _, r := range js.References {
		if strings.EqualFold(r.RefType, "FOLLOWS_FROM") {
			return otlp.ID(r.SpanID)
		}
	}
	return otlp.ID(js.ParentSpanID)
}

// applyTags folds Jaeger tags into attributes and lifts the well-known ones
// (span.kind, error, otel.status_code/description) into typed fields.
func applyTags(s *model.Span, tags []tagJSON) {
	for _, t := range tags {
		val := tagString(t.Value)
		switch t.Key {
		case "span.kind":
			s.Kind = strings.ToLower(val)
		case "error":
			if val == "true" {
				s.Status = model.StatusError
			}
		case "otel.status_code":
			switch strings.ToUpper(val) {
			case "ERROR":
				s.Status = model.StatusError
			case "OK":
				if s.Status != model.StatusError {
					s.Status = model.StatusOK
				}
			}
		case "otel.status_description":
			s.StatusMsg = val
		default:
			if s.Attrs == nil {
				s.Attrs = make(map[string]string)
			}
			s.Attrs[t.Key] = val
		}
	}
}

func tagString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
