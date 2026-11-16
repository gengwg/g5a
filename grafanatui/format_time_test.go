//nolint:exhaustruct // test panel and frame use only the relevant fields
package grafanatui_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableRendererFormatTimeThenOrganizeIsNotReformatted(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number"},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1700000000000,null]`), json.RawMessage(`[1,2]`),
			}},
		}}},
	}}
	grafana.ApplyTransformationsWithTimezone(result, []grafana.Transformation{
		{ID: "formatTime", Options: json.RawMessage(`{
			"timeField":"Time","outputFormat":"YYYY-MM-DD HH:mm:ss","timezone":"utc"
		}`)},
		{ID: "organize", Options: json.RawMessage(`{
			"renameByName":{"Time":"When"},"indexByName":{"Value":0,"When":1}
		}`)},
	}, "UTC")
	frame := result.Results["A"].Frames[0]
	require.Equal(t, "When", frame.Schema.Fields[1].Name)
	require.Equal(t, "string", frame.Schema.Fields[1].Type)

	output := grafanatui.RendererForPanel("table").Render(result, grafana.Panel{
		Timezone:    "America/New_York",
		FieldConfig: json.RawMessage(`{"defaults":{"noValue":"-"}}`),
	}, 100, 10, nil)
	assert.Contains(t, output, "When")
	assert.Contains(t, output, "2023-11-14 22:13:20")
	assert.Contains(t, output, "Invalid date")
	assert.NotContains(t, output, "2023-11-14 17:13:20")
	assert.NotContains(t, output, "\n-")
}
