//nolint:exhaustruct,testpackage // internal legend calculations need direct tests
package grafanatui

import (
	"math"
	"testing"

	"github.com/lovromazgon/grafana-tui/grafana"
)

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
