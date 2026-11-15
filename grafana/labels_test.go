//nolint:exhaustruct // test frames intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsMaterializesLabels(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"signal": "GPU", "cluster": "alpha",
				}},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1000]`), json.RawMessage(`[2]`),
			}},
		}}},
	}}

	grafana.ApplyTransformations(result, nil)
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 4)
	assert.Equal(t, "cluster", frame.Schema.Fields[2].Name)
	assert.Equal(t, "signal", frame.Schema.Fields[3].Name)
	assert.JSONEq(t, `["alpha"]`, string(frame.Data.Values[2]))
	assert.JSONEq(t, `["GPU"]`, string(frame.Data.Values[3]))
}
