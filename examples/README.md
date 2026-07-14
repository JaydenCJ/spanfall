# spanfall examples

Three realistic trace files, all offline and deterministic (fixed
timestamps, fixed IDs), covering each supported input format.

## checkout-trace.json — OTLP/JSON

One 187.5ms `GET /api/checkout` request across four services (gateway,
pricing, cart, payments), 12 spans, including parallel fan-out, a failed
`card.authorize` attempt with a retry, and attributes of every OTLP value
type. This is the trace on the README and in the smoke test.

```bash
spanfall render examples/checkout-trace.json
spanfall critical examples/checkout-trace.json
spanfall render --fail-on-error examples/checkout-trace.json; echo "exit: $?"
```

## collector.jsonl — OTLP JSON Lines

Two exports as written by the OpenTelemetry Collector `file` exporter:
a 3.2ms health check and a 1.85s cron job. Shows multi-trace handling.

```bash
spanfall list examples/collector.jsonl
spanfall render --trace 9d4c examples/collector.jsonl
spanfall render --all examples/collector.jsonl
```

## jaeger-login.json — Jaeger UI export

A `POST /login` trace as downloaded from Jaeger's UI: microsecond
timestamps, a process table for service names, `CHILD_OF` references, and
`span.kind` tags. Renders identically to the OTLP inputs.

```bash
spanfall render examples/jaeger-login.json
spanfall stats examples/jaeger-login.json
```
