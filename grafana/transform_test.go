//nolint:exhaustruct // transformation fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsJoinByField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		byField     string
		results     map[string]grafana.QueryResultData
		wantResult  string
		wantFields  []string
		wantColumns []string
	}{
		{
			name:    "outer join across refIDs with duplicate names and missing cells",
			byField: "Time",
			results: map[string]grafana.QueryResultData{
				"B": {Frames: []grafana.DataFrame{newTransformFrame(
					"B", transformFields("Time", "Value", "Status"),
					`[2000,3000]`, `[20,30]`, `["x","y"]`,
				)}},
				"A": {Frames: []grafana.DataFrame{newTransformFrame(
					"A", transformFields("Time", "Value", "Unit"),
					`[1000,2000]`, `[1,2]`, `["a","b"]`,
				)}},
			},
			wantResult: "A",
			wantFields: []string{
				"Time", "Value #A", "Unit", "Value #B", "Status",
			},
			wantColumns: []string{
				`[1000,2000,3000]`,
				`[1,2,null]`,
				`["a","b",null]`,
				`[null,20,30]`,
				`[null,"x","y"]`,
			},
		},
		{
			name:    "string keys and nulls retain first seen row order",
			byField: "Key",
			results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{newTransformFrame(
					"A", transformFields("Key", "Value"),
					`["b",null,"a"]`, `[1,2,3]`,
				)}},
				"B": {Frames: []grafana.DataFrame{newTransformFrame(
					"B", transformFields("Key", "Value"),
					`[null,"c","b"]`, `[20,30,40]`,
				)}},
			},
			wantResult: "A",
			wantFields: []string{"Key", "Value #A", "Value #B"},
			wantColumns: []string{
				`["b",null,"a","c"]`,
				`[1,2,3,null]`,
				`[40,20,null,30]`,
			},
		},
		{
			name:    "multiple frames in one result are all joined",
			byField: "Key",
			results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{
					newTransformFrame(
						"A", transformFields("Key", "First"),
						`["a","b"]`, `[1,2]`,
					),
					newTransformFrame(
						"A", transformFields("Key", "Second"),
						`["b","c"]`, `[20,30]`,
					),
				}},
			},
			wantResult: "A",
			wantFields: []string{"Key", "First", "Second"},
			wantColumns: []string{
				`["a","b","c"]`, `[1,2,null]`, `[null,20,30]`,
			},
		},
		{
			name:    "empty and missing join fields are safe",
			byField: "Key",
			results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{
					newTransformFrame("A", nil),
				}},
				"B": {Frames: []grafana.DataFrame{newTransformFrame(
					"B", transformFields("Other"), `[1]`,
				)}},
				"C": {Frames: []grafana.DataFrame{newTransformFrame(
					"C", transformFields("Key", "Value"), `[]`, `[]`,
				)}},
			},
			wantResult:  "A",
			wantFields:  []string{"Key", "Value"},
			wantColumns: []string{`[]`, `[]`},
		},
		{
			name:    "duplicate names fall back to result map keys",
			byField: "Key",
			results: map[string]grafana.QueryResultData{
				"Z": {Frames: []grafana.DataFrame{newTransformFrame(
					"", transformFields("Key", "Value"), `["z"]`, `[1]`,
				)}},
				"Y": {Frames: []grafana.DataFrame{newTransformFrame(
					"", transformFields("Key", "Value"), `["y"]`, `[2]`,
				)}},
			},
			wantResult: "Y",
			wantFields: []string{"Key", "Value #Y", "Value #Z"},
			wantColumns: []string{
				`["y","z"]`, `[2,null]`, `[null,1]`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := &grafana.QueryResult{Results: tt.results}
			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "joinByField",
				Options: json.RawMessage(
					`{"byField":"` + tt.byField + `","mode":"outer"}`,
				),
			}})

			require.Len(t, result.Results, 1)
			joined, ok := result.Results[tt.wantResult]
			require.True(t, ok)
			require.Len(t, joined.Frames, 1)

			frame := joined.Frames[0]
			require.Len(t, frame.Schema.Fields, len(tt.wantFields))
			for i, want := range tt.wantFields {
				assert.Equal(t, want, frame.Schema.Fields[i].Name)
			}

			require.Len(t, frame.Data.Values, len(tt.wantColumns))
			for i, want := range tt.wantColumns {
				assert.JSONEq(t, want, string(frame.Data.Values[i]))
			}
		})
	}
}

func TestApplyTransformationsJoinByFieldThenFilterByValue(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", transformFields("Key", "Value"), `["a","b"]`, `[1,2]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B", transformFields("Key", "Value"), `["b","c"]`, `[10,30]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{
			ID:      "joinByField",
			Options: json.RawMessage(`{"byField":"Key","mode":"outer"}`),
		},
		{
			ID: "filterByValue",
			Options: json.RawMessage(`{
				"type":"include",
				"match":"any",
				"filters":[{
					"fieldName":"Value #B",
					"config":{"id":"greater","options":{"value":15}}
				}]
			}`),
		},
	})

	require.Len(t, result.Results, 1)
	require.Len(t, result.Results["A"].Frames, 1)
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Data.Values, 3)
	assert.JSONEq(t, `["c"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[null]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[30]`, string(frame.Data.Values[2]))
}

func TestApplyTransformationsOrganize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		options     string
		wantFields  []string
		wantColumns []string
	}{
		{
			name: "exclude rename then index",
			options: `{
				"excludeByName":{"Time":true},
				"renameByName":{"Value #B":"Value","Status":"State"},
				"indexByName":{"State":0,"Value #A":1,"Value":2}
			}`,
			wantFields: []string{"State", "Value #A", "Value"},
			wantColumns: []string{
				`["up","down"]`, `[10,20]`, `[100,200]`,
			},
		},
		{
			name:        "unspecified fields follow indexed fields in prior order",
			options:     `{"indexByName":{"Value #B":4,"Time":2}}`,
			wantFields:  []string{"Time", "Value #B", "Value #A", "Status"},
			wantColumns: []string{`[1,2]`, `[100,200]`, `[10,20]`, `["up","down"]`},
		},
		{
			name:        "equal indexes retain prior relative order",
			options:     `{"indexByName":{"Status":1,"Value #A":1}}`,
			wantFields:  []string{"Value #A", "Status", "Time", "Value #B"},
			wantColumns: []string{`[10,20]`, `["up","down"]`, `[1,2]`, `[100,200]`},
		},
		{
			name: "rename and exclude without indexes retain existing behavior",
			options: `{
				"excludeByName":{"Value #A":true},
				"renameByName":{"Status":"State"}
			}`,
			wantFields:  []string{"Time", "Value #B", "State"},
			wantColumns: []string{`[1,2]`, `[100,200]`, `["up","down"]`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{newTransformFrame(
					"A",
					transformFields("Time", "Value #A", "Value #B", "Status"),
					`[1,2]`, `[10,20]`, `[100,200]`, `["up","down"]`,
				)}},
			}}

			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "organize", Options: json.RawMessage(tt.options),
			}})

			frame := result.Results["A"].Frames[0]
			require.Len(t, frame.Schema.Fields, len(tt.wantFields))
			for i, want := range tt.wantFields {
				assert.Equal(t, want, frame.Schema.Fields[i].Name)
			}

			require.Len(t, frame.Data.Values, len(tt.wantColumns))
			for i, want := range tt.wantColumns {
				assert.JSONEq(t, want, string(frame.Data.Values[i]))
			}
		})
	}
}

func TestApplyTransformations_JoinByLabel(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"job": "api"}},
			},
			`[1000]`, `[1]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"job": "api"}},
			},
			`[1000]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "joinByField", Options: json.RawMessage(`{"byField":"job","mode":"outer"}`)},
		{ID: "filterFieldsByName", Options: json.RawMessage(
			`{"include":{"pattern":"^(job|Value #.*)$"}}`,
		)},
	})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 3)
	assert.Equal(t, "job", frame.Schema.Fields[0].Name)
	assert.Equal(t, "Value #A", frame.Schema.Fields[1].Name)
	assert.Equal(t, "Value #B", frame.Schema.Fields[2].Name)
	assert.JSONEq(t, `["api"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[1]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[2]`, string(frame.Data.Values[2]))
}

func TestApplyTransformations_JoinWideFrameByLabel(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"job": "api"}},
				{Name: "Value", Type: "number", Labels: map[string]string{"job": "web"}},
			},
			`[1000]`, `[1]`, `[2]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"job": "api"}},
				{Name: "Value", Type: "number", Labels: map[string]string{"job": "web"}},
			},
			`[1000]`, `[10]`, `[20]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "joinByField", Options: json.RawMessage(`{"byField":"job","mode":"outer"}`)},
		{ID: "filterFieldsByName", Options: json.RawMessage(
			`{"include":{"pattern":"^(job|Value.*)$"}}`,
		)},
	})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 3)
	assert.Equal(t, "Value #A", frame.Schema.Fields[1].Name)
	assert.Equal(t, "Value #B", frame.Schema.Fields[2].Name)
	assert.JSONEq(t, `["api","web"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[1,2]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[10,20]`, string(frame.Data.Values[2]))
}

func TestApplyTransformations_OrganizeRenamesAllDuplicateFields(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"cluster": "alpha"}},
				{Name: "Value", Type: "number", Labels: map[string]string{"cluster": "beta"}},
			},
			`[1000]`, `[1]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "joinByField", Options: json.RawMessage(`{"byField":"cluster","mode":"outer"}`)},
		{ID: "filterFieldsByName", Options: json.RawMessage(
			`{"include":{"pattern":"^(cluster|Value #A)$"}}`,
		)},
		{ID: "organize", Options: json.RawMessage(
			`{"renameByName":{"Value #A":"NotReady"}}`,
		)},
	})

	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "NotReady", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `[1,2]`, string(frame.Data.Values[1]))
}

func TestApplyTransformations_UnsupportedWarns(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{}}
	grafana.ApplyTransformations(result, []grafana.Transformation{{ID: "unknown"}})
	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one unsupported-transformation warning", result.Warnings)
	}
}

func newTransformFrame(
	refID string, fields []grafana.DataFrameField, values ...string,
) grafana.DataFrame {
	rawValues := make([]json.RawMessage, len(values))
	for i, value := range values {
		rawValues[i] = json.RawMessage(value)
	}

	return grafana.DataFrame{
		Schema: grafana.DataFrameSchema{
			RefID:  refID,
			Meta:   grafana.DataFrameMeta{Type: "", TypeVersion: nil},
			Fields: fields,
		},
		Data: grafana.DataFrameData{Values: rawValues},
	}
}

func transformFields(names ...string) []grafana.DataFrameField {
	fields := make([]grafana.DataFrameField, len(names))
	for i, name := range names {
		fields[i] = grafana.DataFrameField{
			Name: name, Type: "string", Labels: nil, Config: nil,
		}
	}

	return fields
}
