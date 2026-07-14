// Package timefmt formats nanosecond durations and timestamps the way trace
// UIs do: adaptive units, fixed precision per unit, so columns line up and
// output is byte-stable across runs.
package timefmt

import (
	"fmt"
	"time"
)

// Duration renders ns with an adaptive unit: 812ns, 24.9µs, 187.5ms, 2.35s,
// 1m12s. One decimal for µs/ms and two for seconds keeps table columns
// narrow but still meaningful at trace scale.
func Duration(ns int64) string {
	neg := ""
	if ns < 0 {
		neg = "-"
		ns = -ns
	}
	switch {
	case ns < 1_000:
		return fmt.Sprintf("%s%dns", neg, ns)
	case ns < 1_000_000:
		return fmt.Sprintf("%s%.1fµs", neg, float64(ns)/1e3)
	case ns < 1_000_000_000:
		return fmt.Sprintf("%s%.1fms", neg, float64(ns)/1e6)
	case ns < 60*1_000_000_000:
		return fmt.Sprintf("%s%.2fs", neg, float64(ns)/1e9)
	default:
		m := ns / (60 * 1_000_000_000)
		s := (ns % (60 * 1_000_000_000)) / 1_000_000_000
		return fmt.Sprintf("%s%dm%02ds", neg, m, s)
	}
}

// ASCIIDuration is Duration with "us" instead of "µs", for --ascii output
// that must survive 7-bit pipelines.
func ASCIIDuration(ns int64) string {
	out := Duration(ns)
	// Only the microsecond unit contains a non-ASCII rune.
	return replaceMicro(out)
}

func replaceMicro(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		if r == 'µ' {
			return string(runes[:i]) + "u" + string(runes[i+1:])
		}
	}
	return s
}

// Timestamp renders a Unix-nanosecond instant as RFC 3339 UTC, the least
// ambiguous form for incident timelines shared across timezones.
func Timestamp(ns int64) string {
	return time.Unix(0, ns).UTC().Format(time.RFC3339)
}
