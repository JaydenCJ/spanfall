// Tests for adaptive duration and timestamp formatting. These strings end
// up in aligned table columns, so exact output (unit choice, precision)
// is contract, not cosmetics.
package timefmt

import "testing"

func TestDurationAdaptiveUnits(t *testing.T) {
	cases := []struct {
		ns   int64
		want string
	}{
		{0, "0ns"},                    // zero stays in the smallest unit
		{812, "812ns"},                // nanoseconds verbatim
		{24_900, "24.9µs"},            // microseconds, one decimal
		{187_500_000, "187.5ms"},      // milliseconds, one decimal
		{2_350_000_000, "2.35s"},      // seconds, two decimals
		{67_000_000_000, "1m07s"},     // minutes, seconds zero-padded
		{-1_500_000, "-1.5ms"},        // malformed inputs stay visibly negative
		{3_601_000_000_000, "60m01s"}, // no hour unit: minutes keep counting
	}
	for _, c := range cases {
		if got := Duration(c.ns); got != c.want {
			t.Fatalf("Duration(%d) = %q, want %q", c.ns, got, c.want)
		}
	}
}

func TestASCIIDurationReplacesOnlyTheMicroSign(t *testing.T) {
	if got := ASCIIDuration(24_900); got != "24.9us" {
		t.Fatalf("got %q", got)
	}
	// Units that are already ASCII pass through untouched.
	if got := ASCIIDuration(187_500_000); got != "187.5ms" {
		t.Fatalf("got %q", got)
	}
}

func TestTimestampRFC3339UTC(t *testing.T) {
	// 2026-02-03T10:00:00Z in Unix nanoseconds.
	if got := Timestamp(1770112800000000000); got != "2026-02-03T10:00:00Z" {
		t.Fatalf("got %q", got)
	}
}
