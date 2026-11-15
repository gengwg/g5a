package grafana

import (
	"testing"
	"time"
)

func TestResolveBuiltinVariablesRateInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		intervalMs int
		want       string
	}{
		{name: "adds default scrape interval", intervalMs: 60000, want: "75s"},
		{name: "applies sixty second floor", intervalMs: 5000, want: "1m"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := resolveBuiltinVariables("$__rate_interval", test.intervalMs, 0)
			if got != test.want {
				t.Errorf("rate interval = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveTimeRangeAtRounding(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.May, 15, 14, 35, 42, 123456789, time.UTC)
	tests := []struct {
		unit  string
		start time.Time
		next  time.Time
	}{
		{
			unit:  "s",
			start: time.Date(2024, time.May, 15, 14, 35, 42, 0, time.UTC),
			next:  time.Date(2024, time.May, 15, 14, 35, 43, 0, time.UTC),
		},
		{
			unit:  "m",
			start: time.Date(2024, time.May, 15, 14, 35, 0, 0, time.UTC),
			next:  time.Date(2024, time.May, 15, 14, 36, 0, 0, time.UTC),
		},
		{
			unit:  "h",
			start: time.Date(2024, time.May, 15, 14, 0, 0, 0, time.UTC),
			next:  time.Date(2024, time.May, 15, 15, 0, 0, 0, time.UTC),
		},
		{
			unit:  "d",
			start: time.Date(2024, time.May, 15, 0, 0, 0, 0, time.UTC),
			next:  time.Date(2024, time.May, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			unit:  "w",
			start: time.Date(2024, time.May, 12, 0, 0, 0, 0, time.UTC),
			next:  time.Date(2024, time.May, 19, 0, 0, 0, 0, time.UTC),
		},
		{
			unit:  "M",
			start: time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC),
			next:  time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			unit:  "y",
			start: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			next:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.unit, func(t *testing.T) {
			t.Parallel()
			from, to := resolveTimeRangeAt(TimeRange{
				From: "now/" + test.unit,
				To:   "now/" + test.unit,
			}, now)
			if want := test.start.UnixMilli(); from != want {
				t.Errorf("from = %d, want %d", from, want)
			}
			if want := test.next.Add(-time.Millisecond).UnixMilli(); to != want {
				t.Errorf("to = %d, want %d", to, want)
			}
		})
	}
}

func TestResolveTimeRangeAtRelativeRounding(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.May, 15, 14, 35, 42, 123456789, time.UTC)
	from, to := resolveTimeRangeAt(TimeRange{From: "now-24h/h", To: "now/h"}, now)
	wantFrom := time.Date(2024, time.May, 14, 14, 0, 0, 0, time.UTC).UnixMilli()
	wantTo := time.Date(2024, time.May, 15, 15, 0, 0, 0, time.UTC).
		Add(-time.Millisecond).UnixMilli()
	if from != wantFrom {
		t.Errorf("from = %d, want %d", from, wantFrom)
	}
	if to != wantTo {
		t.Errorf("to = %d, want %d", to, wantTo)
	}
}

func TestResolveTimeRangeAtAlignedRounding(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.May, 15, 14, 0, 0, 0, time.UTC)
	from, to := resolveTimeRangeAt(TimeRange{From: "now/h", To: "now/h"}, now)
	if from != now.UnixMilli() {
		t.Errorf("from = %d, want aligned hour %d", from, now.UnixMilli())
	}
	if want := now.Add(time.Hour - time.Millisecond).UnixMilli(); to != want {
		t.Errorf("to = %d, want end of hour %d", to, want)
	}
}

func TestResolveTimeToMsAtUnsupportedRounding(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, time.May, 15, 14, 35, 42, 123456789, time.UTC)
	for _, value := range []string{"now/Q", "now/2h", "now+1h/h", "now-1h/"} {
		if got := resolveTimeToMsAt(value, now); got != now.UnixMilli() {
			t.Errorf("resolveTimeToMsAt(%q) = %d, want safe fallback %d", value, got, now.UnixMilli())
		}
	}
}
