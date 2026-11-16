//nolint:exhaustruct // calculation fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsCalculateFieldUnaryOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		operator string
		value    string
		want     string
	}{
		{operator: "abs", value: "-2", want: "2"},
		{operator: "exp", value: "1", want: "2.718281828459045"},
		{operator: "ln", value: "1", want: "0"},
		{operator: "round", value: "-1.5", want: "-1"},
		{operator: "floor", value: "1.9", want: "1"},
		{operator: "ceil", value: "1.1", want: "2"},
	}
	for _, test := range tests {
		t.Run(test.operator, func(t *testing.T) {
			t.Parallel()

			result := calculationModeResult(`[1000]`, `[`+test.value+`]`)
			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "calculateField",
				Options: json.RawMessage(`{
					"mode":"unary", "replaceFields":true,
					"unary":{"fieldName":"Value","operator":"` + test.operator + `"}
				}`),
			}})

			frame := result.Results["A"].Frames[0]
			require.Len(t, frame.Schema.Fields, 2)
			assert.Equal(t, test.operator+"(Value)", frame.Schema.Fields[1].Name)
			assert.JSONEq(t, `[`+test.want+`]`, string(frame.Data.Values[1]))
			assert.Empty(t, result.Warnings)
		})
	}
}

func TestApplyTransformationsCalculateFieldUnaryPercent(t *testing.T) {
	t.Parallel()

	result := calculationModeResult(`[1000,2000,3000,4000]`, `[1,2,3,4]`)
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"unary", "replaceFields":true,
			"unary":{"fieldName":"Value","operator":"percent"}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	assert.JSONEq(t, `[0.1,0.2,0.3,0.4]`, string(frame.Data.Values[1]))
}

func TestApplyTransformationsCalculateFieldIndexPercentile(t *testing.T) {
	t.Parallel()

	result := calculationModeResult(`[1000,2000,3000,4000]`, `[1,2,3,4]`)
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"index", "replaceFields":true,
			"index":{"asPercentile":true}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 1)
	assert.Equal(t, "Row", frame.Schema.Fields[0].Name)
	assert.JSONEq(t, `{"unit":"percentunit"}`, string(frame.Schema.Fields[0].Config))
	assert.JSONEq(t, `[0,0.25,0.5,0.75]`, string(frame.Data.Values[0]))
}

func TestApplyTransformationsCalculateFieldCumulative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reducer string
		want    string
	}{
		{reducer: "sum", want: `[1,1,3,6]`},
		{reducer: "mean", want: `[1,0.5,1,1.5]`},
	}
	for _, test := range tests {
		t.Run(test.reducer, func(t *testing.T) {
			t.Parallel()

			result := calculationModeResult(`[1000,2000,3000,4000]`, `[1,null,2,3]`)
			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "calculateField",
				Options: json.RawMessage(`{
					"mode":"cumulativeFunctions", "replaceFields":true,
					"cumulative":{"field":"Value","reducer":"` + test.reducer + `"}
				}`),
			}})

			frame := result.Results["A"].Frames[0]
			assert.Equal(t, "cumulative "+test.reducer+"(Value)", frame.Schema.Fields[1].Name)
			assert.JSONEq(t, test.want, string(frame.Data.Values[1]))
		})
	}
}

func TestApplyTransformationsCalculateFieldTrailingWindowMean(t *testing.T) {
	t.Parallel()

	result := calculationModeResult(`[1000,2000,3000,4000,5000]`, `[1,null,3,4,5]`)
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"windowFunctions", "replaceFields":true,
			"window":{
				"field":"Value","reducer":"mean",
				"windowSize":2,"windowSizeMode":"fixed","windowAlignment":"trailing"
			}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	assert.Equal(t, "trailing moving mean(Value)", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `[1,1,3,3.5,4.5]`, string(frame.Data.Values[1]))
}

func TestApplyTransformationsCalculateFieldCenteredWindow(t *testing.T) {
	t.Parallel()

	result := calculationModeResult(`[1000,2000,3000]`, `[1,2,3]`)
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"windowFunctions", "replaceFields":true,
			"window":{
				"field":"Value","reducer":"stdDev",
				"windowSize":1,"windowSizeMode":"percentage","windowAlignment":"centered"
			}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	var values []float64
	require.NoError(t, json.Unmarshal(frame.Data.Values[1], &values))
	require.Len(t, values, 3)
	assert.InDelta(t, 0.5, values[0], 0.000001)
	assert.InDelta(t, math.Sqrt(2.0/3.0), values[1], 0.000001)
	assert.InDelta(t, 0.5, values[2], 0.000001)
}

func TestApplyTransformationsCalculateFieldRejectsInvalidWindow(t *testing.T) {
	t.Parallel()

	result := calculationModeResult(`[1000]`, `[1]`)
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"windowFunctions", "window":{"windowSize":0}
		}`),
	}})
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "larger than 0")
}

func TestApplyTransformationsCalculateFieldBinaryAllNumbers(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "B", Type: "number"},
				{Name: "C", Type: "number"},
				{Name: "D", Type: "string"},
			},
			`[1000,2000]`, `[2,200]`, `[3,300]`, `["first","second"]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"binary", "replaceFields":true,
			"binary":{
				"left":{"matcher":{"id":"byType","options":"number"}},
				"operator":"+","right":"2"
			}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 3)
	assert.Equal(t, "Time", frame.Schema.Fields[0].Name)
	assert.Equal(t, "B + 2", frame.Schema.Fields[1].Name)
	assert.Equal(t, "C + 2", frame.Schema.Fields[2].Name)
	assert.JSONEq(t, `[4,202]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[5,302]`, string(frame.Data.Values[2]))
	assert.Empty(t, result.Warnings)
}

func TestApplyTransformationsCalculateFieldReduceRowJoinsQueries(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[1,2]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[3,4]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"reduceRow", "replaceFields":true,
			"reduce":{"include":["Value #A","Value #B"],"reducer":"sum"}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.JSONEq(t, `[4,6]`, string(frame.Data.Values[1]))
}

func TestApplyTransformationsCalculateFieldUnaryRequiresFieldName(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "First", Type: "number"},
				{Name: "Second", Type: "number"},
			},
			`[1000]`, `[1]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"unary", "replaceFields":false,
			"unary":{"fieldName":"","operator":"abs"}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 3)
	assert.Equal(t, "First", frame.Schema.Fields[1].Name)
	assert.Equal(t, "Second", frame.Schema.Fields[2].Name)
	assert.Contains(t, result.Warnings, "calculateField matched no fields")
}

func TestApplyTransformationsCalculateFieldIndexJoinsFrames(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[1,2]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000,2000]`, `[3,4]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"index", "replaceFields":true,
			"index":{"asPercentile":true}
		}`),
	}})

	require.Len(t, result.Results, 1)
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 1)
	assert.Equal(t, "Row", frame.Schema.Fields[0].Name)
	assert.JSONEq(t, `[0,0.5]`, string(frame.Data.Values[0]))
}

func TestApplyTransformationsCalculateFieldBinaryAllNumbersInterleaves(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "B", Type: "number"},
				{Name: "C", Type: "number"},
				{Name: "D", Type: "string"},
			},
			`[1000,2000]`, `[2,200]`, `[3,300]`, `["first","second"]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "calculateField",
		Options: json.RawMessage(`{
			"mode":"binary", "replaceFields":false,
			"binary":{
				"left":{"matcher":{"id":"byType","options":"number"}},
				"operator":"+","right":"2"
			}
		}`),
	}})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 6)
	names := make([]string, 0, len(frame.Schema.Fields))
	for _, field := range frame.Schema.Fields {
		names = append(names, field.Name)
	}
	assert.Equal(t, []string{"Time", "B", "B + 2", "C", "C + 2", "D"}, names)
	assert.Empty(t, result.Warnings)
}

func calculationModeResult(times, values string) *grafana.QueryResult {
	return &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			times, values,
		)}},
	}}
}
