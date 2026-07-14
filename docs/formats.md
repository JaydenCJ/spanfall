# Input formats

spanfall reads trace files, not trace backends. Three shapes are accepted,
and the format is detected automatically — there is no `--format-in` flag
to get wrong at 3 a.m.

## OTLP/JSON

One `ExportTraceServiceRequest` object, the JSON encoding of the OTLP
protobuf. This is what the OpenTelemetry Collector's `file` exporter emits
per line, what `otel-cli` produces, and what most SDK debug exporters dump.

```json
{ "resourceSpans": [ { "resource": { … }, "scopeSpans": [ { "spans": [ … ] } ] } ] }
```

Accepted variance (all seen in the wild):

| Detail | Accepted forms |
|---|---|
| Key style | `resourceSpans` and `resource_spans` (protojson input convention) |
| Scope field | `scopeSpans`, plus pre-1.0 `instrumentationLibrarySpans` |
| Trace/span IDs | lowercase/uppercase hex, or base64 (protojson bytes encoding) |
| Timestamps | `startTimeUnixNano` as a string or a number — decoded without float64 rounding |
| Enums | `kind`/`status.code` as numbers (`2`) or names (`"SPAN_KIND_SERVER"`) |
| Parent | absent, empty, or all-zero IDs all mean "root span" |

Spans missing an ID are dropped; a missing end time renders as a
zero-width span rather than failing the file.

## OTLP JSON Lines

One OTLP export object per line — exactly what the Collector `file`
exporter writes over time. All lines are merged, then regrouped by trace
ID, so a file covering many requests becomes many traces (`spanfall list`
shows them all). Blank lines are ignored; a malformed line is reported
with its line number.

## Jaeger JSON

The document produced by Jaeger UI's "Download JSON" button and the
`/api/traces` endpoint:

```json
{ "data": [ { "traceID": "…", "spans": [ … ], "processes": { "p1": { "serviceName": "…" } } } ] }
```

Normalization applied:

| Jaeger detail | Becomes |
|---|---|
| `startTime` / `duration` (microseconds) | Unix nanoseconds |
| `processID` → `processes` table | the span's service name |
| `CHILD_OF` reference (else `FOLLOWS_FROM`) | the parent span |
| `span.kind` tag | span kind |
| `error=true` or `otel.status_code=ERROR` tag | error status |
| `otel.status_description` tag | status message |
| all other tags | attributes |

## Top-level arrays

A JSON array whose elements are OTLP and/or Jaeger objects is also
accepted, since people concatenate exports when collecting evidence.

## Malformed data policy

Trace files shared mid-incident are rarely pristine. The tree builder
keeps every span visible no matter what:

- **Orphans** (parent sampled away or in another file) become extra roots.
- **Duplicate span IDs** are all kept; the first occurrence wins parent
  lookups.
- **Self-parents and parent cycles** are broken deterministically into
  roots — never an infinite loop, never a dropped span.
- **Children that overflow their parent** (clock skew, async work) are
  clamped in self-time math and followed by the critical path.
