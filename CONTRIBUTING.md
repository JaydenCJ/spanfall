# Contributing to spanfall

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else — no services, no databases, no network.

```bash
git clone https://github.com/JaydenCJ/spanfall && cd spanfall
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary and drives every subcommand against
the shipped example traces (OTLP, JSONL, Jaeger, stdin), asserting on real
CLI output and exit codes; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (89 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (parsers, tree building, path math, and rendering never touch
   the filesystem — only `load.ReadFile` and the CLI do).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in the PR.
- No network calls, ever, and no telemetry — spanfall reads a local file
  and writes to stdout. That is the whole contract.
- Determinism first: identical input must produce byte-identical output,
  including all orderings and tie-breaks. New comparison logic needs an
  explicit, tested tie-break.
- Input leniency is a feature: new producer quirks (field spellings, ID
  encodings, enum forms) belong in the parsers with a test reproducing the
  real payload shape, and a row in `docs/formats.md`.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `spanfall version`, the full command you ran, and —
this matters most — the smallest trace file that reproduces the problem
(redact span names and attribute values if needed; the timing and tree
structure are what the renderer and path math actually consume).

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
