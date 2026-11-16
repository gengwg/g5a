package grafana

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			got := resolveBuiltinVariables("$__rate_interval", test.intervalMs, 15000, 0, 0, 0)
			if got != test.want {
				t.Errorf("rate interval = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBuildDSQueryTargetHonorsMinStepAndExplicitFields(t *testing.T) {
	t.Parallel()

	target := Target{
		RefID:      "A",
		Datasource: &DatasourceRef{UID: "prom1", Type: "prometheus"},
		raw:        json.RawMessage(`{"expr":"rate(x[$__rate_interval])","interval":"5m","maxDataPoints":42}`),
	}
	raw, err := buildDSQueryTarget(
		target, target.Datasource, nil, nil,
		1000, 15000, 0, 3600000, 3600000, 100, "timeseries",
	)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["intervalMs"] != float64(300000) {
		t.Errorf("intervalMs = %v, want 300000", got["intervalMs"])
	}
	if got["maxDataPoints"] != float64(42) {
		t.Errorf("maxDataPoints = %v, want 42", got["maxDataPoints"])
	}
	if got["expr"] != "rate(x[20m])" {
		t.Errorf("rate interval = %v, want 20m", got["expr"])
	}
}

func TestResolveBuiltinVariablesSupportsBracedAndMillisecondForms(t *testing.T) {
	t.Parallel()

	expression := "${__interval}|$__interval|${__rate_interval}|$__rate_interval|${__rate_interval_ms}|$__from|$__to"
	got := resolveBuiltinVariables(expression, 15000, 15000, 1000, 2000, 1000)
	want := "15s|15s|1m|1m|60000|1000|2000"
	if got != want {
		t.Errorf("built-ins = %q, want %q", got, want)
	}
}

func TestResolveBuiltinVariablesRateIntervalUsesDatasourceScrapeInterval(t *testing.T) {
	t.Parallel()

	if got := resolveBuiltinVariables("$__rate_interval", 15000, 60000, 0, 0, 0); got != "4m" {
		t.Errorf("rate interval = %q, want 4m", got)
	}
}

func TestInterpolateTransformationsPreservesOriginalOptions(t *testing.T) {
	t.Parallel()

	var variables []TemplateVariable
	require.NoError(t, json.Unmarshal([]byte(`[{"name":"env","current":{"value":"prod"}}]`), &variables))
	transformations := []Transformation{{
		ID: "filterByValue",
		Options: json.RawMessage(`{
			"filters":[{"fieldName":"Time","config":{"id":"between","options":{
				"from":"${__from}","to":"$__to","value":"$env"
			}}}]
		}`),
	}}
	original := string(transformations[0].Options)
	resolved := interpolateTransformations(transformations, variables, nil, 15000, 1000, 2000)
	require.Len(t, resolved, 1)
	assert.JSONEq(t, `{
		"filters":[{"fieldName":"Time","config":{"id":"between","options":{
			"from":"1000","to":"2000","value":"prod"
		}}}]
	}`, string(resolved[0].Options))
	assert.Equal(t, original, string(transformations[0].Options))
}

func TestResolveTimeRangeAtHonorsTimezoneWeekStartAndDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)
	from, to := resolveTimeRangeAt(TimeRange{
		From: "now/d", To: "now/d", Timezone: "UTC", WeekStart: "", NowDelay: "",
	}, now)
	wantFrom := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	wantTo := time.Date(2026, 11, 15, 23, 59, 59, 999*int(time.Millisecond), time.UTC).UnixMilli()
	if from != wantFrom || to != wantTo {
		t.Errorf("UTC range = %d/%d, want %d/%d", from, to, wantFrom, wantTo)
	}

	from, _ = resolveTimeRangeAt(TimeRange{
		From: "now/d", To: "now/d", Timezone: "America/Los_Angeles", WeekStart: "", NowDelay: "",
	}, now)
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	wantFrom = time.Date(2026, 11, 14, 0, 0, 0, 0, location).UnixMilli()
	if from != wantFrom {
		t.Errorf("Los Angeles day start = %d, want %d", from, wantFrom)
	}

	from, _ = resolveTimeRangeAt(TimeRange{
		From: "now/w", To: "now/w", WeekStart: "monday", Timezone: "UTC", NowDelay: "",
	}, now)
	wantFrom = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC).UnixMilli()
	if from != wantFrom {
		t.Errorf("Monday week start = %d, want %d", from, wantFrom)
	}

	from, _ = resolveTimeRangeAt(TimeRange{From: "now", To: "now", NowDelay: "1h", Timezone: "UTC", WeekStart: ""}, now)
	wantFrom = now.Add(-time.Hour).UnixMilli()
	if from != wantFrom {
		t.Errorf("now delay = %d, want %d", from, wantFrom)
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
				From: "now/" + test.unit, To: "now/" + test.unit,
				Timezone: "", WeekStart: "", NowDelay: "",
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
	from, to := resolveTimeRangeAt(TimeRange{
		From: "now-24h/h", To: "now/h", Timezone: "", WeekStart: "", NowDelay: "",
	}, now)
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
	from, to := resolveTimeRangeAt(TimeRange{From: "now/h", To: "now/h", Timezone: "", WeekStart: "", NowDelay: ""}, now)
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
