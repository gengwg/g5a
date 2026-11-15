//nolint:exhaustruct,testpackage // internal legend calculations need direct tests
package grafanatui

import (
	"math"
	"strings"
	"testing"

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
