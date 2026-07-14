// Package model defines the format-neutral span representation every other
// package works with. OTLP and Jaeger inputs are both normalized into it,
// so the tree builder, critical-path engine, and renderers never need to
// know where a span came from.
package model

// Span kinds, normalized to lowercase words regardless of whether the input
// encoded them as protobuf enum numbers or SPAN_KIND_* strings.
const (
	KindUnspecified = "unspecified"
	KindInternal    = "internal"
	KindServer      = "server"
	KindClient      = "client"
	KindProducer    = "producer"
	KindConsumer    = "consumer"
)

// Status codes, normalized the same way.
const (
	StatusUnset = "unset"
	StatusOK    = "ok"
	StatusError = "error"
)

// Span is one span, with IDs canonicalized to lowercase hex and times in
// Unix nanoseconds. ParentID is "" for root spans (all-zero parent IDs are
// normalized to "" by the parsers).
type Span struct {
	TraceID   string
	SpanID    string
	ParentID  string
	Name      string
	Service   string
	Kind      string
	Start     int64 // Unix nanoseconds
	End       int64 // Unix nanoseconds
	Status    string
	StatusMsg string
	Attrs     map[string]string
	Events    int
}

// Duration returns End-Start, clamped at zero so a malformed span with
// End < Start cannot poison downstream math with negative widths.
func (s *Span) Duration() int64 {
	if s.End < s.Start {
		return 0
	}
	return s.End - s.Start
}

// IsError reports whether the span's status is the error code.
func (s *Span) IsError() bool { return s.Status == StatusError }

// KindFromEnum maps the OTLP SpanKind enum number to its normalized name.
func KindFromEnum(n int64) string {
	switch n {
	case 1:
		return KindInternal
	case 2:
		return KindServer
	case 3:
		return KindClient
	case 4:
		return KindProducer
	case 5:
		return KindConsumer
	default:
		return KindUnspecified
	}
}

// StatusFromEnum maps the OTLP StatusCode enum number to its normalized name.
func StatusFromEnum(n int64) string {
	switch n {
	case 1:
		return StatusOK
	case 2:
		return StatusError
	default:
		return StatusUnset
	}
}
