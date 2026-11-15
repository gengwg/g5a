//nolint:exhaustruct // calculation fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsCalculateFieldBinary(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "SLI", Type: "number"},
				{Name: "Target", Type: "number"},
			},
			`[1000,2000]`, `[10,20]`, `[4,25]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"binary",
			"alias":"Budget left",
			"replaceFields":true,
			"binary":{
				"left":{"matcher":{"id":"byName","options":"SLI"}},
				"operator":"-",
				"right":{"matcher":{"id":"byName","options":"Target"}}
			}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Time", frame.Schema.Fields[0].Name)
	assert.Equal(t, "Budget left", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `{"displayName":"Budget left"}`, string(frame.Schema.Fields[1].Config))
	assert.JSONEq(t, `[6,-5]`, string(frame.Data.Values[1]))
	assert.Empty(t, result.Warnings)
}

func TestApplyTransformationsCalculateFieldLegacyConstant(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[0.5,0.25]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"binary",
			"alias":"Budget left",
			"replaceFields":true,
			"binary":{"left":"Value","operator":"-","right":"1"}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Budget left", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `[-0.5,-0.75]`, string(frame.Data.Values[1]))
	assert.Empty(t, result.Warnings)
}

func TestApplyTransformationsCalculateFieldJoinsQueriesByTime(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[10,20]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[2,4]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"binary",
			"replaceFields":true,
			"binary":{"left":"Value #A","operator":"/","right":"Value #B"}
		}`),
	}})

	require.Len(t, result.Results, 1)
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Time", frame.Schema.Fields[0].Name)
	assert.Equal(t, "Value #A / Value #B", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `[5,5]`, string(frame.Data.Values[1]))
	assert.Empty(t, result.Warnings)
}

func TestApplyTransformationsCalculateFieldDoesNotJoinInconsistentFrames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		leftTime  string
		rightTime string
	}{
		{name: "different time field names", leftTime: "Time", rightTime: "timestamp"},
		{name: "table frame without time", leftTime: "Time", rightTime: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			leftFields := []grafana.DataFrameField{{Name: test.leftTime, Type: "time"}, {Name: "Left", Type: "number"}}
			if test.leftTime == "" {
				leftFields[0].Type = "string"
			}
			rightFields := []grafana.DataFrameField{{Name: test.rightTime, Type: "time"}, {Name: "Right", Type: "number"}}
			if test.rightTime == "" {
				rightFields[0].Type = "string"
				rightFields[0].Name = "Key"
			}
			result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{newTransformFrame("A", leftFields, `[1000]`, `[10]`)}},
				"B": {Frames: []grafana.DataFrame{newTransformFrame("B", rightFields, `[1000]`, `[2]`)}},
			}}

			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "calculateField",
				Options: json.RawMessage(`{
					"mode":"binary", "replaceFields":false,
					"binary":{"left":"Left","operator":"/","right":"Right"}
				}`),
			}})

			require.Len(t, result.Results, 2)
			require.Len(t, result.Results["A"].Frames, 1)
			require.Len(t, result.Results["B"].Frames, 1)
			assert.Equal(t, "Left", result.Results["A"].Frames[0].Schema.Fields[1].Name)
			assert.Equal(t, "Right", result.Results["B"].Frames[0].Schema.Fields[1].Name)
			assert.Contains(t, result.Warnings, "calculateField matched no fields")
		})
	}
}

func TestApplyTransformationsCalculateFieldReduceRowLastPreservesNull(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "A", Type: "number"},
				{Name: "B", Type: "number"},
			},
			`[1000]`, `[1]`, `[null]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"reduceRow", "replaceFields":true,
			"reduce":{"include":["A","B"],"reducer":"last"}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.JSONEq(t, `[null]`, string(frame.Data.Values[1]))
}

func TestApplyTransformationsCalculateFieldReduceRow(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "A", Type: "number"},
				{Name: "B", Type: "number"},
				{Name: "Label", Type: "string"},
			},
			`[1000,2000]`, `[1,3]`, `[3,5]`, `["x","y"]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"reduceRow",
			"replaceFields":true,
			"reduce":{"include":["A","B"],"reducer":"mean"}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Mean", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `[2,4]`, string(frame.Data.Values[1]))
	assert.Empty(t, result.Warnings)
}

func TestApplyTransformationsCalculateFieldUnsupportedMode(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{}}
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField", Options: json.RawMessage(`{"mode":"windowFunctions"}`),
	}})
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "windowFunctions")
}
