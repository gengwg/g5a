//nolint:exhaustruct,testpackage // internal legend calculations need direct tests
package grafanatui

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

func TestCompactLegendName(t *testing.T) {
	t.Parallel()

	got := compactLegendName("https://service.example.com/healthz", 20)
	if strings.Contains(got, "://") {
		t.Errorf("scheme was not removed: %q", got)
	}
	if !strings.Contains(got, "…") || !strings.HasSuffix(got, "healthz") {
		t.Errorf("compacted name does not preserve suffix: %q", got)
	}
	if lipgloss.Width(got) > 20 {
		t.Errorf("compacted name exceeds width: %q", got)
	}
	if short := compactLegendName("cpu", 20); short != "cpu" {
		t.Errorf("short name changed: %q", short)
	}
}

func TestCompactLegendNameWithVantagePrefix(t *testing.T) {
	t.Parallel()

	got := compactLegendName("Frankfurt HTTPS://service.example.com/healthz", 24)
	if strings.Contains(got, "://") {
		t.Errorf("embedded scheme was not removed: %q", got)
	}
	if !strings.Contains(got, "Frankfurt") || !strings.HasSuffix(got, "healthz") {
		t.Errorf("compacted name lost identifying context: %q", got)
	}
	if lipgloss.Width(got) > 24 {
		t.Errorf("compacted name exceeds width: %q", got)
	}
}

func TestCompactLegendNameWithUnicodePrefix(t *testing.T) {
	t.Parallel()

	got := compactLegendName("İstanbul HTTPS://service.example.com/healthz", 30)
	if strings.Contains(got, "://") {
		t.Errorf("scheme after a Unicode prefix was not removed: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("compaction produced invalid UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, "İstanbul") {
		t.Errorf("Unicode prefix was not preserved: %q", got)
	}
}

func TestBottomLegendSharesWidthBetweenSeries(t *testing.T) {
	t.Parallel()

	first := "Frankfurt https://service.example.com/healthz"
	second := "NorthVirginia https://other.example.com/healthz"
	data := map[string]timeSeriesEntry{
		first:  {values: []float64{1}},
		second: {values: []float64{2}},
	}
	output := buildLegendWithCalculations([]string{first, second}, data, 40, nil)
	if strings.Count(output, "■") != 2 {
		t.Errorf("one series was crowded out of the legend: %q", output)
	}
	if strings.Contains(output, "://") {
		t.Errorf("embedded URL schemes were not removed: %q", output)
	}
	if lipgloss.Width(output) > 40 {
		t.Errorf("legend exceeds width 40: %q", output)
	}
}

func TestBottomLegendWrapsWideCalculations(t *testing.T) {
	t.Parallel()

	data := map[string]timeSeriesEntry{
		"cpu":    {values: []float64{1}},
		"memory": {values: []float64{2}},
	}
	output := buildLegendWithCalculations(
		[]string{"cpu", "memory"}, data, 40, []string{"median"},
	)
	if strings.Count(output, "median=unsupported") != 2 {
		t.Errorf("calculations were truncated or lost: %q", output)
	}
	if strings.Count(output, "■") != 2 {
		t.Errorf("a series was crowded out of the legend: %q", output)
	}
	if lipgloss.Height(output) != 2 {
		t.Errorf("wrapped legend has %d lines, want 2", lipgloss.Height(output))
	}
	for line := range strings.SplitSeq(output, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Errorf("legend line exceeds width 40: %q", line)
		}
	}
}

func TestBottomLegendBoundsVeryNarrowCalculations(t *testing.T) {
	t.Parallel()

	data := map[string]timeSeriesEntry{
		"cpu":    {values: []float64{1}},
		"memory": {values: []float64{2}},
	}
	output := buildLegendWithCalculations(
		[]string{"cpu", "memory"}, data, 8, []string{"median"},
	)
	if lipgloss.Height(output) > 2 {
		t.Errorf("narrow legend exceeds two lines: %q", output)
	}
	for line := range strings.SplitSeq(output, "\n") {
		if lipgloss.Width(line) > 8 {
			t.Errorf("narrow legend line exceeds width 8: %q", line)
		}
	}
}

func TestCalculatedLegendPreservesValuesAfterCompactingName(t *testing.T) {
	t.Parallel()

	name := "https://service.example.com/healthz"
	data := map[string]timeSeriesEntry{
		name: {values: []float64{1, 2}, field: grafana.DataFrameField{Name: "Value"}},
	}
	output := buildCalculatedLegend([]string{name}, data, 30, 2, []string{"lastNotNull"})
	if !strings.Contains(output, "2") {
		t.Errorf("calculation value was truncated: %q", output)
	}
	if !strings.Contains(output, "…") {
		t.Errorf("long series name was not compacted: %q", output)
	}
}

func TestLegendCalculationsUseSharedReducer(t *testing.T) {
	t.Parallel()

	entry := timeSeriesEntry{
		values: []float64{2, math.NaN(), 4, 6},
		field:  grafana.DataFrameField{Name: "value"},
	}
	tests := []struct {
		calculation string
		want        string
	}{
		{calculation: "sum", want: "  Sum=12"},
		{calculation: "first", want: "  First=2"},
		{calculation: "count", want: "  Count=3"},
		{calculation: "median", want: "  median=unsupported"},
	}

	for _, tt := range tests {
		t.Run(tt.calculation, func(t *testing.T) {
			t.Parallel()

			got := legendCalculations(entry, []string{tt.calculation})
			if got != tt.want {
				t.Fatalf("legendCalculations(%q) = %q, want %q", tt.calculation, got, tt.want)
			}
		})
	}
}
