//nolint:exhaustruct // test frames intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformations_GroupingToMatrix(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{
			newTransformFrame("A", []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "alpha", "probe_target": "dns",
				}},
			}, `[1000]`, `[0.98]`),
			newTransformFrame("A", []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "alpha", "probe_target": "http",
				}},
			}, `[1000]`, `[0.97]`),
			newTransformFrame("A", []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "beta", "probe_target": "http",
				}},
			}, `[1000]`, `[0.89]`),
		}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "groupingToMatrix",
		Options: json.RawMessage(`{
			"rowField":"cluster","columnField":"probe_target",
			"valueField":"Value","emptyValue":"empty"
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 3)
	assert.Equal(t, `cluster\probe_target`, frame.Schema.Fields[0].Name)
	assert.Equal(t, "dns", frame.Schema.Fields[1].Name)
	assert.Equal(t, "http", frame.Schema.Fields[2].Name)
	assert.JSONEq(t, `["alpha","beta"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[0.98,""]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[0.97,0.89]`, string(frame.Data.Values[2]))
}
