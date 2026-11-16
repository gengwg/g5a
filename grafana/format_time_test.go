//nolint:exhaustruct // transformation fixtures vary only the relevant fields
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTransformationsFormatTimeUpstreamFixtures(t *testing.T) {
	t.Parallel()

	values := `[1612939600000,1689192000000,1682025600000,1690328089000,1691011200000]`
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{
			name: "year month", format: "YYYY-MM",
			want: `["2021-02","2023-07","2023-04","2023-07","2023-08"]`,
		},
		{
			name: "twelve hour clock", format: "YYYY-MM h:mm:ss a",
			want: `["2021-02 6:46:40 am","2023-07 8:00:00 pm","2023-04 9:20:00 pm",` +
				`"2023-07 11:34:49 pm","2023-08 9:20:00 pm"]`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{newTransformFrame(
					"A", []grafana.DataFrameField{{Name: "time", Type: "time"}}, values,
				)}},
			}}
			grafana.ApplyTransformationsWithTimezone(result, []grafana.Transformation{{
				ID: "formatTime",
				Options: json.RawMessage(
					`{"timeField":"time","outputFormat":"` + test.format + `","timezone":"utc"}`,
				),
			}}, "")
			frame := result.Results["A"].Frames[0]
			assert.Equal(t, "string", frame.Schema.Fields[0].Type)
			assert.Equal(t, "time", frame.Schema.Fields[0].Name)
			assert.JSONEq(t, test.want, string(frame.Data.Values[0]))
			assert.Empty(t, result.Warnings)
		})
	}
}

func TestApplyTransformationsFormatTimeWeekdayTokens(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		format string
		want   string
	}{
		{format: "dd", want: "Tu"},
		{format: "ddd", want: "Tue"},
		{format: "dddd", want: "Tuesday"},
		{format: "DD", want: "14"},
		{format: "SSS", want: "000"},
		{format: "YYYYMMDD", want: "20231114"},
	} {
		t.Run(test.format, func(t *testing.T) {
			t.Parallel()
			result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{newTransformFrame(
					"A", []grafana.DataFrameField{{Name: "time", Type: "time"}}, `[1700000000000]`,
				)}},
			}}
			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "formatTime", Options: json.RawMessage(
					`{"timeField":"time","outputFormat":"` + test.format + `","timezone":"utc"}`,
				),
			}})
			assert.JSONEq(t, `["`+test.want+`"]`, string(result.Results["A"].Frames[0].Data.Values[0]))
			assert.Empty(t, result.Warnings)
		})
	}
}

func TestApplyTransformationsFormatTimeWarnsOnUnsupportedTokens(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"Do", "DDD", "DDDD", "YYYYYY", "LL"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
				"A": {Frames: []grafana.DataFrame{newTransformFrame(
					"A", []grafana.DataFrameField{{Name: "time", Type: "time"}}, `[1700000000000]`,
				)}},
			}}
			grafana.ApplyTransformations(result, []grafana.Transformation{{
				ID: "formatTime", Options: json.RawMessage(
					`{"timeField":"time","outputFormat":"` + format + `"}`),
			}})
			assert.JSONEq(t, `["`+format+`"]`, string(result.Results["A"].Frames[0].Data.Values[0]))
			require.Len(t, result.Warnings, 1)
			assert.Contains(t, result.Warnings[0], "unsupported Moment tokens")
		})
	}
}

func TestApplyTransformationsFormatTimeNullInvalidAndStringInputs(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", []grafana.DataFrameField{{Name: "time", Type: "time"}},
			`[0,null,"not a date","2023-11-14T22:13:20Z","2023-11-14 17:13:20"]`,
		)}},
	}}
	grafana.ApplyTransformationsWithTimezone(result, []grafana.Transformation{{
		ID: "formatTime",
		Options: json.RawMessage(`{
			"timeField":"time","outputFormat":"YYYY-MM-DD HH:mm:ss","timezone":"America/New_York"
		}`),
	}}, "")
	frame := result.Results["A"].Frames[0]
	assert.JSONEq(t, `[
		"1969-12-31 19:00:00","Invalid date","Invalid date",
		"2023-11-14 17:13:20","2023-11-14 17:13:20"
	]`, string(frame.Data.Values[0]))
}

func TestApplyTransformationsFormatTimeMatchesDisplayName(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A",
			[]grafana.DataFrameField{
				{Name: "created", Type: "time", Config: json.RawMessage(`{"displayName":"Created"}`)},
				{Name: "Value", Type: "number"},
			},
			`[1700000000000]`, `[1]`,
		)}},
	}}
	grafana.ApplyTransformations(result, []grafana.Transformation{{
		ID: "formatTime", Options: json.RawMessage(`{"timeField":"Created","outputFormat":"YYYY"}`),
	}})
	frame := result.Results["A"].Frames[0]
	assert.Equal(t, "string", frame.Schema.Fields[0].Type)
	assert.Equal(t, "number", frame.Schema.Fields[1].Type)
	assert.JSONEq(t, `{"displayName":"Created"}`, string(frame.Schema.Fields[0].Config))
	assert.JSONEq(t, `["2023"]`, string(frame.Data.Values[0]))
	assert.JSONEq(t, `[1]`, string(frame.Data.Values[1]))
}

func TestApplyTransformationsFormatTimeEmptyAndLiteralFormats(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", []grafana.DataFrameField{{Name: "time", Type: "time"}}, `[1700000000000]`,
		)}},
	}}
	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "formatTime", Options: json.RawMessage(`{"timeField":"time","outputFormat":""}`)},
	})
	assert.JSONEq(t, `[""]`, string(result.Results["A"].Frames[0].Data.Values[0]))

	result = &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newTransformFrame(
			"A", []grafana.DataFrameField{{Name: "time", Type: "time"}}, `[1700000000000]`,
		)}},
	}}
	grafana.ApplyTransformations(result, []grafana.Transformation{
		{ID: "formatTime", Options: json.RawMessage(`{"timeField":"time","outputFormat":"[Day] D [Month] M"}`)},
	})
	assert.JSONEq(t, `["Day 14 Month 11"]`, string(result.Results["A"].Frames[0].Data.Values[0]))
}
