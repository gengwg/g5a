//nolint:exhaustruct // test frames intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergePreservesDuplicateNumericRefIDs(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000]`, `[1]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
			`[1000]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "merge"},
		{
			ID: "organize",
			Options: json.RawMessage(`{
				"excludeByName":{"Time":true},
				"renameByName":{"Value #A":"CPU req","Value #B":"Mem req"}
			}`),
		},
	})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 2)
	assert.Equal(t, "CPU req", frame.Schema.Fields[0].Name)
	assert.Equal(t, "Mem req", frame.Schema.Fields[1].Name)
	assert.JSONEq(t, `[1,null]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[null,2]`, string(frame.Data.Values[1]))
}

func TestMergeHandlesSameRefIDFramesAndOddQueryCounts(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{
			newTransformFrame(
				"A",
				[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
				`[1000]`, `[1]`,
			),
			newTransformFrame(
				"A",
				[]grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}},
				`[1000]`, `[2]`,
			),
		}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B", []grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}}, `[1000]`, `[3]`,
		)}},
		"C": {Frames: []grafana.DataFrame{newTransformFrame(
			"C", []grafana.DataFrameField{{Name: "Time", Type: "time"}, {Name: "Value", Type: "number"}}, `[1000]`, `[4]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{ID: "merge"}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 4)
	assert.Equal(t, "Value #A", frame.Schema.Fields[1].Name)
	assert.Equal(t, "Value #B", frame.Schema.Fields[2].Name)
	assert.Equal(t, "Value #C", frame.Schema.Fields[3].Name)
	assert.JSONEq(t, `[1,2,null,null]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[null,null,3,null]`, string(frame.Data.Values[2]))
	assert.JSONEq(t, `[null,null,null,4]`, string(frame.Data.Values[3]))
}

func TestMergeCoalescesMixedTypeDuplicateNames(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", []grafana.DataFrameField{{Name: "Value", Type: "string"}}, `["text"]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B", []grafana.DataFrameField{{Name: "Value", Type: "number"}}, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{ID: "merge"}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 1)
	assert.Equal(t, "Value", frame.Schema.Fields[0].Name)
}

func TestApplyTransformations_Merge(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{{Name: "cluster", Type: "string"}, {Name: "Value", Type: "number"}},
			`["alpha"]`, `[1]`,
		)}},
		"B": {Frames: []grafana.DataFrame{newTransformFrame(
			"B",
			[]grafana.DataFrameField{{Name: "cluster", Type: "string"}, {Name: "Other", Type: "number"}},
			`["beta"]`, `[2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{ID: "merge"}})
	frame := result.Results["A"].Frames[0]
	require.Len(t, frame.Schema.Fields, 3)
	assert.JSONEq(t, `["alpha","beta"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[1,null]`, string(frame.Data.Values[1]))
	assert.JSONEq(t, `[null,2]`, string(frame.Data.Values[2]))
}
