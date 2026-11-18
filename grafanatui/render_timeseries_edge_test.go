//nolint:exhaustruct // test frames intentionally vary relevant fields only
package grafanatui_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
)

func TestTimeSeriesRenderer_DegenerateInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
		values []float64
	}{
		{name: "tiny panel", width: 5, height: 1, values: []float64{1}},
		{name: "single point", width: 20, height: 4, values: []float64{100}},
		{name: "constant positive", width: 30, height: 6, values: []float64{100, 100, 100}},
		{name: "constant negative", width: 40, height: 8, values: []float64{-100, -100, -100}},
		{name: "near constant percentages", width: 40, height: 8, values: []float64{100, 99.999, 99.998}},
		{name: "mixed sign values", width: 40, height: 8, values: []float64{-798.3, -351.7, 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{{
					Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
						{Name: "Time", Type: "time"},
						{Name: "Value", Type: "number"},
					}},
					Data: grafana.DataFrameData{Values: []json.RawMessage{
						mustMarshalJSON([]int64{1000, 2000, 3000}),
						mustMarshalJSON(tt.values),
					}},
				}}},
			}}
			output := grafanatui.RendererForPanel("timeseries").Render(
				result,
				grafana.Panel{},
				tt.width,
				tt.height,
				nil,
			)
			if strings.Contains(output, "chart rendering error") {
				t.Fatalf("renderer failed for %dx%d: %s", tt.width, tt.height, output)
			}
		})
	}
}

// The top Y label is often the widest ("100.0%" above "85.7%") and must not
// lose characters at any panel height.
func TestTimeSeriesRenderer_TopYLabelNotClipped(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number"},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				mustMarshalJSON([]int64{1000, 2000, 3000}),
				mustMarshalJSON([]float64{0, 0.5, 1}),
			}},
		}}},
	}}
	panel := grafana.Panel{FieldConfig: json.RawMessage(`{"defaults":{"unit":"percentunit"}}`)}

	for height := 6; height <= 15; height++ {
		output := grafanatui.RendererForPanel("timeseries").Render(result, panel, 40, height, nil)
		if !strings.Contains(output, "100.0%") {
			t.Errorf("height %d: top Y label clipped:\n%s", height, output)
		}
	}
}
