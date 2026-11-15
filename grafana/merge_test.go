//nolint:exhaustruct // test frames intentionally vary relevant fields only
package grafana_test

import (
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
