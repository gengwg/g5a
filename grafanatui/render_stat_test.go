//nolint:testpackage // internal reducer needs direct tests
package grafanatui

import (
	"math"
	"testing"
)

func TestReduceStatColumnAllNaNExtremes(t *testing.T) {
	t.Parallel()

	for _, calculation := range []string{calcMin, calcMax} {
		t.Run(calculation, func(t *testing.T) {
			t.Parallel()

			got := reduceStatColumn(
				[]float64{math.NaN(), math.NaN()},
				[]string{calculation},
			)
			if !math.IsNaN(got) {
				t.Fatalf("reduceStatColumn(%q) over all NaN = %v, want NaN", calculation, got)
			}
		})
	}
}

//nolint:exhaustruct // test cases intentionally omit zero-valued expectations
func TestReduceStatColumnFirstNullSemantics(t *testing.T) {
	t.Parallel()

	values := []float64{math.NaN(), 2}
	tests := []struct {
		name        string
		calculation string
		want        float64
		wantNaN     bool
	}{
		{name: "first preserves null", calculation: "first", wantNaN: true},
		{name: "firstNotNull skips null", calculation: "firstNotNull", want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := reduceStatColumn(values, []string{tt.calculation})
			if tt.wantNaN && !math.IsNaN(got) {
				t.Fatalf("got %v, want NaN", got)
			}
			if !tt.wantNaN && got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
