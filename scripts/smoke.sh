#!/usr/bin/env bash
# End-to-end smoke test for spanfall: builds the binary and drives every
# subcommand against the shipped example files plus stdin, asserting on real
# CLI output and exit codes. No network, idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/spanfall"
CHECKOUT="$ROOT/examples/checkout-trace.json"
JSONL="$ROOT/examples/collector.jsonl"
JAEGER="$ROOT/examples/jaeger-login.json"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/spanfall) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" --version | grep -qx "spanfall 0.1.0" || fail "--version mismatch"

echo "3. waterfall renders the OTLP example with a critical path"
OUT="$("$BIN" render "$CHECKOUT")"
echo "$OUT" | grep -q "trace 4bf92f3577b34da6a3ce929d0e0e4736" || fail "trace header missing"
echo "$OUT" | grep -q "187.5ms" || fail "trace duration missing"
echo "$OUT" | grep -q "└─ payment.charge" || fail "tree glyphs missing"
echo "$OUT" | grep -q "█" || fail "critical bars missing"
echo "$OUT" | grep -q "✗" || fail "error marker missing"
echo "$OUT" | grep -q "critical path: 9 of 12 spans" || fail "critical footer wrong"

echo "4. --ascii output is pure 7-bit"
AOUT="$("$BIN" render --ascii "$CHECKOUT")"
echo "$AOUT" | LC_ALL=C grep -q '[^ -~]' && fail "--ascii emitted non-ASCII bytes"
echo "$AOUT" | grep -q "#" || fail "ASCII bars missing"

echo "5. critical breakdown sums to the whole trace"
COUT="$("$BIN" critical "$CHECKOUT")"
echo "$COUT" | grep -q "card.authorize" || fail "path span missing"
echo "$COUT" | grep -q "26.2%" || fail "self percent wrong"
echo "$COUT" | grep -q "accounts for 100.0%" || fail "path does not tile the trace"

echo "6. critical --format json is machine-readable"
CJSON="$("$BIN" critical --format json "$CHECKOUT")"
echo "$CJSON" | grep -q '"tool": "spanfall"' || fail "json envelope missing"
echo "$CJSON" | grep -q '"path_span_count": 9' || fail "json path count wrong"
echo "$CJSON" | grep -q '"self_ns": 49200000' || fail "json self time wrong"

echo "7. list inventories a collector JSONL file"
LOUT="$("$BIN" list "$JSONL")"
echo "$LOUT" | grep -q "0af7651916cd43dd8448eb211c80319c" || fail "first trace missing"
echo "$LOUT" | grep -q "cron.digest" || fail "second trace root missing"
echo "$LOUT" | grep -c "^" | grep -qx "3" || fail "want header + 2 rows"

echo "8. jaeger export renders and stats aggregate"
"$BIN" render "$JAEGER" | grep -q "POST /login" || fail "jaeger render failed"
"$BIN" stats "$CHECKOUT" | grep -q "card.authorize" || fail "stats row missing"
"$BIN" stats "$CHECKOUT" | grep -q "91.2ms" || fail "stats aggregate wrong"

echo "9. stdin works with '-'"
"$BIN" render - < "$CHECKOUT" | grep -q "critical path:" || fail "stdin render failed"

echo "10. --trace selects and --fail-on-error gates"
"$BIN" render --trace 9d4c "$JSONL" | grep -q "cron.digest" || fail "--trace selection failed"
if "$BIN" render --fail-on-error "$CHECKOUT" >/dev/null 2>&1; then
  fail "--fail-on-error should exit 1 on the checkout trace"
fi
"$BIN" render --fail-on-error "$JAEGER" >/dev/null 2>&1 \
  || fail "--fail-on-error should pass on a clean trace"

echo "11. usage errors exit 2, runtime errors exit 3"
set +e
"$BIN" render --color sometimes "$CHECKOUT" >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --color should exit 2"
"$BIN" render "$WORKDIR/missing.json" >/dev/null 2>&1
[ $? -eq 3 ] || fail "missing file should exit 3"
set -e

echo "SMOKE OK"
