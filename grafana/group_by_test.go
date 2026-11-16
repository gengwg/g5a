//nolint:exhaustruct // transformation fixtures vary only the relevant fields
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsGroupByAggregations(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Region", Type: "string"}, {Name: "Value", Type: "number"}},
			`["a","a","a","b","b"]`, `[1,null,3,2,4]`,
		)}},
	}}
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "groupBy",
		Options: json.RawMessage(`{"fields":{
			"Region":{"operation":"groupby","aggregations":[]},
			"Value":{"operation":"aggregate","aggregations":[
				"sum","mean","count","countAll","firstNotNull","lastNotNull","median","distinctCount","changeCount"
			]}
		}}`),
	}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 10)
	assert.Equal(t, "Region", frame.Schema.Fields[0].Name)
	assert.Equal(t, "Value (sum)", frame.Schema.Fields[1].Name)
	assert.Equal(t, "Value (changeCount)", frame.Schema.Fields[9].Name)
	assert.JSONEq(t, `["a","b"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[4,6]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[2,3]`, string(frame.Data.Values[2]))
	assert.JSONEq(t, `[2,2]`, string(frame.Data.Values[3]))
	assert.JSONEq(t, `[3,2]`, string(frame.Data.Values[4]))
	assert.JSONEq(t, `[1,2]`, string(frame.Data.Values[5]))
	assert.JSONEq(t, `[3,4]`, string(frame.Data.Values[6]))
	assert.JSONEq(t, `[2,3]`, string(frame.Data.Values[7]))
	assert.JSONEq(t, `[3,2]`, string(frame.Data.Values[8]))
	assert.JSONEq(t, `[2,1]`, string(frame.Data.Values[9]))
}

func TestApplyTransformationsFormatTimeGroupBySortBy(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Merged", Type: "time"}, {Name: "MR", Type: "string"}},
			`[1700000000000,1700000000000,1700086400000,1700086400000,1700086400000,1700172800000]`,
			`["a","b","a","a","a",null]`,
		)}},
	}}
	grafana.ApplyTransformationsWithTimezone(result, []grafana.Transformation{
		{ID: "formatTime", Options: json.RawMessage(`{
			"timeField":"Merged","outputFormat":"YYYY-MM-DD","timezone":"utc","useTimezone":true
		}`)},
		{ID: "groupBy", Options: json.RawMessage(`{"fields":{
			"MR":{"operation":"aggregate","aggregations":["count"]},
			"Merged":{"operation":"groupby","aggregations":[]}
		}}`)},
		{ID: "sortBy", Options: json.RawMessage(`{"sort":[{"field":"MR (count)","desc":true}]}`)},
	}, "UTC")
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Merged", frame.Schema.Fields[0].Name)
	assert.Equal(t, "string", frame.Schema.Fields[0].Type)
	assert.Equal(t, "MR (count)", frame.Schema.Fields[1].Name)
	assert.Equal(t, "number", frame.Schema.Fields[1].Type)
	assert.JSONEq(t, `["2023-11-15","2023-11-14","2023-11-16"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[3,2,0]`, string(frame.Data.Values[1]))
	assert.Empty(t, result.Warnings)
}

func TestApplyTransformationsGroupByNoOperationOrUnsupportedAggregation(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", transformFields("Key", "Value"), `["a"]`, `[1]`,
		)}},
	}}
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "groupBy", Options: json.RawMessage(`{"fields":{
			"Value":{"operation":"aggregate","aggregations":["sum"]}
		}}`),
	}})
	assert.JSONEq(t, `["a"]`, string(result.Results["A"].Frames[0].Data.Values[0]))

	result = &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", transformFields("Key", "Value"), `["a"]`, `[1]`,
		)}},
	}}
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "groupBy", Options: json.RawMessage(`{"fields":{
			"Key":{"operation":"groupby","aggregations":[]},
			"Value":{"operation":"aggregate","aggregations":["p99"]}
		}}`),
	}})
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "p99")
}
