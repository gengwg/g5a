//nolint:exhaustruct // sort fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
)

func TestApplyTransformations_SortBy(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "cluster", Type: "string"},
				{Name: "Nodes", Type: "number"},
			},
			`["beta","alpha","gamma"]`, `[1,3,2]`,
		)}},
	}}

	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "sortBy", Options: json.RawMessage(`{"sort":[{"field":"Nodes","desc":true}]}`),
	}})

	frame := result.Results["A"].Frames[0]
	assert.JSONEq(t, `["alpha","gamma","beta"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[3,2,1]`, string(frame.Data.Values[1]))
}
