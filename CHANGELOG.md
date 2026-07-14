# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- OTLP/JSON trace parsing tolerant of real producer variance: camelCase
  and snake_case keys, the pre-1.0 `instrumentationLibrarySpans` field,
  hex and base64 IDs (normalized to lowercase hex), string and numeric
  timestamps decoded without float64 rounding, numeric and name-form
  enums, and every OTLP attribute value type.
- Jaeger JSON parsing (UI "Download JSON" / `/api/traces`): microsecond
  conversion, process-table service resolution, `CHILD_OF` /
  `FOLLOWS_FROM` references, and `span.kind` / `error` /
  `otel.status_code` / `otel.status_description` tag lifting.
- Automatic format detection covering single OTLP objects, Jaeger
  exports, OpenTelemetry Collector `file`-exporter JSON Lines (with
  per-line error reporting), and mixed top-level arrays.
- Per-trace tree assembly that survives incident-file damage: orphaned
  spans, duplicate span IDs, self-parents, and parent cycles all degrade
  to extra roots deterministically instead of dropping data.
- Critical-path engine (last-finishing-child walk over subtree ends) whose
  segments provably tile the trace envelope, with honest `(gap)` reporting
  for multi-root traces and async children charged for tail latency.
- `render` waterfall: aligned span tree with proportional bars, solid
  fill plus red color for on-path spans, error markers, `--width`,
  `--max-depth`, `--min-duration`, `--trace` prefix selection, `--all`,
  and a `--fail-on-error` CI gate (exit 1).
- `critical`, `list`, and `stats` subcommands with aligned text tables
  and stable JSON (`schema_version: 1`); `stats` aggregates per-operation
  self time via interval-union math.
- `--ascii` mode producing strictly 7-bit output and `--color
  auto|always|never` with tty detection.
- Runnable example traces for all three formats (`examples/`), format and
  critical-path design docs (`docs/`).
- 89 deterministic offline tests (unit + in-process CLI integration
  against the shipped examples) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/spanfall/releases/tag/v0.1.0
