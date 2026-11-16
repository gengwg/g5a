//nolint:exhaustruct // test frames intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsPreservesLabelsWithoutLabelJoin(t *testing.T) {
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
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Value", frame.Schema.Fields[1].Name)
	assert.Equal(t, "alpha", frame.Schema.Fields[1].Labels["cluster"])
	assert.Equal(t, "GPU", frame.Schema.Fields[1].Labels["signal"])
	assert.Len(t, frame.Data.Values, 2)
}

func TestApplyTransformationsRegexFilterDoesNotMaterializeLabels(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"__name__": "requests_total", "host": "node-a", "family": "bridge",
				}},
			},
			`[1000]`, `[1]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "filterFieldsByName", Options: json.RawMessage(`{"include":{"pattern":".*"}}`),
	}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "Time", frame.Schema.Fields[0].Name)
	assert.Equal(t, "Value", frame.Schema.Fields[1].Name)
	assert.Equal(t, "requests_total", frame.Schema.Fields[1].Labels["__name__"])
}

func TestApplyTransformationsRegexFilterSelectsLiteralLabelAlternatives(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "alpha", "signal": "Critical", "__name__": "requests_total",
				}},
			},
			`[1000]`, `[1]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "filterFieldsByName",
		Options: json.RawMessage(
			`{"include":{"pattern":"^(cluster|signal|Value.*)$"}}`,
		),
	}})
	frame := result.Results["A"].Frames[0]
	fieldNames := make([]string, 0, len(frame.Schema.Fields))
	for _, field := range frame.Schema.Fields {
		fieldNames = append(fieldNames, field.Name)
	}
	assert.ElementsMatch(t, []string{"Value", "cluster", "signal"}, fieldNames)
}

func TestApplyTransformationsRegexFilterRejectsCaseFoldedLiteralCandidates(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"CLUSTER": "alpha"}},
			},
			`[1000]`, `[1]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "filterFieldsByName",
		Options: json.RawMessage(
			`{"include":{"pattern":"(?i)^(cluster|Value.*)$"}}`,
		),
	}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 1)
	assert.Equal(t, "Value", frame.Schema.Fields[0].Name)
	assert.Equal(t, "alpha", frame.Schema.Fields[0].Labels["CLUSTER"])
}

func TestApplyTransformationsJoinMaterializesOnlyJoinLabel(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "alpha", "signal": "GPU", "instance": "node-a",
				}},
			},
			`[1000]`, `[1]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "alpha", "signal": "Memory", "instance": "node-b",
				}},
			},
			`[1000]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "joinByField", Options: json.RawMessage(`{"byField":"cluster","mode":"outer"}`),
	}})
	frame := result.Results["A"].Frames[0]
	fieldNames := make([]string, 0, len(frame.Schema.Fields))
	for _, field := range frame.Schema.Fields {
		fieldNames = append(fieldNames, field.Name)
		assert.NotEqual(t, "signal", field.Name)
		assert.NotEqual(t, "instance", field.Name)
	}
	assert.ElementsMatch(t,
		[]string{"cluster", "Time #A", "Value #A", "Time #B", "Value #B"},
		fieldNames,
	)
}

func TestApplyTransformationsOrganizePreservesAllLabelsBeforeValue(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "alpha", "product_name": "PowerEdge R6715", "system_vendor": "Dell Inc.",
				}},
			},
			`[1000]`, `[5]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "organize",
		Options: json.RawMessage(`{
			"excludeByName":{"Time":true},
			"renameByName":{"Value":"nodes","product_name":"model","system_vendor":"vendor"}
		}`),
	}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 4)
	wantNames := []string{"cluster", "model", "vendor", "nodes"}
	wantValues := []string{`["alpha"]`, `["PowerEdge R6715"]`, `["Dell Inc."]`, `[5]`}
	for index, wantName := range wantNames {
		assert.Equal(t, wantName, frame.Schema.Fields[index].Name)
		assert.JSONEq(t, wantValues[index], string(frame.Data.Values[index]))
	}
}

func TestApplyTransformationsOrganizeRenameThenJoinLabel(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"cluster": "alpha"}},
			},
			`[1000]`, `[1]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"cluster": "alpha"}},
			},
			`[1000]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "organize", Options: json.RawMessage(`{"renameByName":{"cluster":"site"}}`)},
		{ID: "joinByField", Options: json.RawMessage(`{"byField":"site","mode":"outer"}`)},
	})
	frame := result.Results["A"].Frames[0]
	assert.Equal(t, "site", frame.Schema.Fields[0].Name)
	assert.JSONEq(t, `["alpha"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[1]`, string(frame.Data.Values[2]))
	assert.JSONEq(t, `[2]`, string(frame.Data.Values[4]))
}

func TestApplyTransformationsRenameCollisionKeepsBothLabelSeries(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"cluster": "alpha"}},
			},
			`[1000]`, `[1]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"site": "beta"}},
			},
			`[1000]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "organize", Options: json.RawMessage(`{"renameByName":{"cluster":"site"}}`)},
		{ID: "joinByField", Options: json.RawMessage(`{"byField":"site","mode":"outer"}`)},
	})
	frame := result.Results["A"].Frames[0]
	assert.Equal(t, "site", frame.Schema.Fields[0].Name)
	assert.JSONEq(t, `["alpha","beta"]`, string(frame.Data.Values[0]))
	valueIndexes := make(map[string]int)
	for index, field := range frame.Schema.Fields {
		valueIndexes[field.Name] = index
	}
	assert.JSONEq(t, `[1,null]`, string(frame.Data.Values[valueIndexes["Value #A"]]))
	assert.JSONEq(t, `[null,2]`, string(frame.Data.Values[valueIndexes["Value #B"]]))
}

func TestApplyTransformationsSortByExplicitLabel(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"cluster": "alpha"}},
			},
			`[1000]`, `[1]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "sortBy", Options: json.RawMessage(`{"sort":[{"field":"cluster"}]}`),
	}})
	frame := result.Results["A"].Frames[0]
	index := -1
	for fieldIndex, field := range frame.Schema.Fields {
		if field.Name == "cluster" {
			index = fieldIndex
		}
	}
	require.NotEqual(t, -1, index)
	assert.JSONEq(t, `["alpha"]`, string(frame.Data.Values[index]))
}
