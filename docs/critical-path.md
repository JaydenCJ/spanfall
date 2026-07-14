# How the critical path is computed

The critical path is the chain of span segments that actually determined
the trace's end-to-end latency: shorten any segment on the path and the
trace gets faster; shorten anything off it and nothing changes. It is the
first thing to look at in a slow trace, which is why `spanfall render`
highlights it and `spanfall critical` quantifies it.

## The walk

spanfall uses the classic **last-finishing-child** walk, sweeping a cursor
backwards from the end of the trace:

1. Start at the trace envelope `[start, end)` with the root span.
2. At each step, look at the current span's children inside the remaining
   window and pick the one whose **subtree** finishes last — that is what
   the parent was waiting on when the window closed.
3. Time between that child's end and the cursor belongs to the parent
   (its own work, or waiting it must answer for). Emit it as a parent
   segment, then descend into the child and repeat inside its window.
4. When no child overlaps the remaining window, the rest is the span's
   self time.

The result is a list of segments that **tile the trace envelope exactly**:
every nanosecond of wall time is attributed to exactly one span (or to an
explicit gap — see below). Summing each span's segments gives its
**self time on the path**, the number `spanfall critical` reports.

## Design decisions

- **Subtree ends, not span ends.** An async child that outlives its parent
  still causes the trace's tail latency. Selection uses the latest end in
  a child's whole subtree, so the walk follows fire-and-forget work
  instead of writing it off as unexplained time.
- **Deterministic ties.** When two children finish at the same instant,
  the later-starting (tighter) one wins, then the larger span ID. Same
  file in, byte-identical output out — always.
- **Zero-duration spans never claim path time.** They cannot have caused
  any latency.
- **Multiple roots are legal.** Broken traces with several roots are
  treated as children of a virtual envelope span. Wall time no root's
  subtree covers is reported honestly as `(gap: no span active)` instead
  of being assigned to an arbitrary span.
- **Clamped windows.** All child times are clamped to the window under
  consideration, so clock skew cannot produce negative segments or
  double-counted time.

## Reading the numbers

In `spanfall critical` output, *self* is the time that belongs to that
span alone while it was the thing blocking the trace. The percentages sum
to 100% of the trace envelope (gaps included), which is a useful
self-check: if a span you expected to matter shows 2%, your latency lives
elsewhere.

A span appearing twice in the waterfall but once in the breakdown (or
vice versa) usually means retries with identical names — spanfall keeps
every span instance separate, so two `card.authorize` attempts are two
path entries with their own self times.
