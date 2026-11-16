//nolint:exhaustruct // presentation fixtures intentionally vary relevant fields only
package grafanatui_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
)

func TestStatRenderer_ValueMappingAndDisplayName(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{{
				Name: "Value", Type: "number", Labels: map[string]string{"site": "Test site"},
			}}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{json.RawMessage(`[2]`)}},
		}}},
	}}
	panel := grafana.Panel{
		FieldConfig: json.RawMessage(`{"defaults":{
			"displayName":"${__field.labels.site}",
			"mappings":[{"type":"value","options":{"2":{"text":"Degraded","color":"orange"}}}]
		}}`),
		Options: json.RawMessage(`{"colorMode":"background","textMode":"value_and_name"}`),
	}

	output := grafanatui.RendererForPanel("stat").Render(result, panel, 60, 8, nil)
	for _, expected := range []string{"Test site", "Degraded"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestTableRenderer_OverridesAndSorting(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "cluster", Type: "string"},
				{Name: "signal", Type: "string"},
				{Name: "Value", Type: "number"},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`["alpha","beta"]`),
				json.RawMessage(`["GPU","GPU"]`),
				json.RawMessage(`[1,2]`),
			}},
		}}},
	}}
	panel := grafana.Panel{
		FieldConfig: json.RawMessage(`{
			"defaults":{},
			"overrides":[{"matcher":{"id":"byName","options":"Value"},"properties":[
				{"id":"displayName","value":"severity"},
				{"id":"mappings","value":[{"type":"value","options":{
					"1":{"text":"Notice","color":"yellow"},
					"2":{"text":"Degraded","color":"orange"}
				}}]},
				{"id":"custom.cellOptions","value":{"type":"color-background","mode":"basic"}}
			]}]
		}`),
		Options: json.RawMessage(`{"showHeader":true,"sortBy":[{"displayName":"severity","desc":true}]}`),
	}

	output := grafanatui.RendererForPanel("table").Render(result, panel, 80, 8, nil)
	for _, expected := range []string{"severity", "Notice", "Degraded"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Index(output, "Degraded") > strings.Index(output, "Notice") {
		t.Errorf("higher numeric severity should sort first:\n%s", output)
	}
}

func TestTableRenderer_JoinsSeriesFramesByTime(t *testing.T) {
	t.Parallel()

	newSeries := func(name string, values string) grafana.DataFrame {
		return grafana.DataFrame{
			Schema: grafana.DataFrameSchema{RefID: "A", Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Config: json.RawMessage(
					`{"displayNameFromDS":"` + name + `"}`,
				)},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1000,2000]`), json.RawMessage(values),
			}},
		}
	}
	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{newSeries("gpu-0", `[0,111]`), newSeries("gpu-1", `[2,333]`)}},
	}}

	output := grafanatui.RendererForPanel("table").Render(result, grafana.Panel{}, 100, 6, nil)
	for _, expected := range []string{"gpu-0", "gpu-1"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain series header %q:\n%s", expected, output)
		}
	}
	foundJoinedRow := false
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "111") && strings.Contains(line, "333") {
			foundJoinedRow = true
		}
	}
	if !foundJoinedRow {
		t.Errorf("second timestamp does not contain both series values:\n%s", output)
	}
}

func TestTableRenderer_PreservesRepeatedAndNullTimestamps(t *testing.T) {
	t.Parallel()

	newSeries := func(name string, values string) grafana.DataFrame {
		return grafana.DataFrame{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Config: json.RawMessage(
					`{"displayNameFromDS":"` + name + `"}`,
				)},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1000,1000,null]`), json.RawMessage(values),
			}},
		}
	}
	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{
			newSeries("gpu-0", `[111,112,113]`),
			newSeries("gpu-1", `[211,212,213]`),
		}},
	}}

	output := grafanatui.RendererForPanel("table").Render(result, grafana.Panel{}, 100, 6, nil)
	for _, pair := range [][2]string{{"111", "211"}, {"112", "212"}, {"113", "213"}} {
		found := false
		for line := range strings.SplitSeq(output, "\n") {
			if strings.Contains(line, pair[0]) && strings.Contains(line, pair[1]) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing repeated/null timestamp row %v:\n%s", pair, output)
		}
	}
}

func TestTableRenderer_LabelFieldsSurviveFiltering(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{RefID: "A", Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{
					"cluster": "neutral", "signal": "Critical XID GPUs",
				}},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1000]`), json.RawMessage(`[2]`),
			}},
		}}},
	}}
	panel := grafana.Panel{
		FieldConfig: json.RawMessage(`{"defaults":{},"overrides":[{
			"matcher":{"id":"byName","options":"Value"},
			"properties":[{"id":"displayName","value":"severity"}]
		}]}`),
		Options: json.RawMessage(`{"showHeader":true}`),
		Transformations: []grafana.Transformation{
			{ID: "filterFieldsByName", Options: json.RawMessage(
				`{"include":{"pattern":"^(cluster|signal|Value.*)$"}}`,
			)},
			{ID: "organize", Options: json.RawMessage(
				`{"indexByName":{"cluster":0,"signal":1,"Value":2}}`,
			)},
		},
	}
	grafana.ApplyTransformations(result, panel.Transformations)
	output := grafanatui.RendererForPanel("table").Render(result, panel, 100, 8, nil)
	for _, expected := range []string{"cluster", "signal", "severity", "neutral", "Critical XID GPUs"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestTableRenderer_JoinAndOrganizeClearsDatasourceNames(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{}}
	for _, refID := range []string{"A", "B", "C", "D"} {
		result.Results[refID] = grafana.QueryResultData{Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{RefID: refID, Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{
					Name: "Value", Type: "number", Labels: map[string]string{"cluster": "alpha"},
					Config: json.RawMessage(`{"displayNameFromDS":"{cluster=\"alpha\"}"}`),
				},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1000]`), json.RawMessage(`[2]`),
			}},
		}}}
	}
	result.Results["A"].Frames[0].Schema.Fields[1].Labels["signal"] = "GPU"

	panel := grafana.Panel{
		FieldConfig: json.RawMessage(`{"defaults":{"noValue":"-"}}`),
		Options:     json.RawMessage(`{"showHeader":true}`),
		Transformations: []grafana.Transformation{
			{ID: "joinByField", Options: json.RawMessage(`{"byField":"cluster","mode":"outer"}`)},
			{ID: "filterFieldsByName", Options: json.RawMessage(
				`{"include":{"pattern":"^(cluster|Value #.*)$"}}`,
			)},
			{ID: "organize", Options: json.RawMessage(`{
				"indexByName":{"cluster":0,"Value #A":1,"Value #B":2,"Value #C":3,"Value #D":4},
				"renameByName":{"Value #A":"NotReady","Value #B":"Cordoned, Ready",
					"Value #C":"Not allocatable, Ready","Value #D":"Total"}
			}`)},
		},
	}
	grafana.ApplyTransformations(result, panel.Transformations)
	output := grafanatui.RendererForPanel("table").Render(result, panel, 120, 8, nil)
	for _, expected := range []string{"cluster", "NotReady", "Cordoned, Ready", "Not allocatable, Ready", "Total"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "{cluster") {
		t.Errorf("datasource display name leaked into headers:\n%s", output)
	}
}

func TestTableRenderer_FormatsTimeFields(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[1700000000000]`),
			}},
		}}},
	}}
	output := grafanatui.RendererForPanel("table").Render(result, grafana.Panel{}, 60, 5, nil)
	//nolint:gosmopolitan // expected timestamp uses the same local timezone as the renderer
	want := time.UnixMilli(1700000000000).In(time.Local).Format("2006-01-02 15:04:05")
	if !strings.Contains(output, want) {
		t.Errorf("output does not contain formatted timestamp %q:\n%s", want, output)
	}
	output = grafanatui.RendererForPanel("table").Render(
		result, grafana.Panel{Timezone: "UTC"}, 60, 5, nil,
	)
	want = time.UnixMilli(1700000000000).UTC().Format("2006-01-02 15:04:05")
	if !strings.Contains(output, want) {
		t.Errorf("UTC output does not contain formatted timestamp %q:\n%s", want, output)
	}
}

func TestTextRenderer(t *testing.T) {
	t.Parallel()

	textPanel := grafana.Panel{Options: json.RawMessage(`{"content":"Read this first"}`)}
	textOutput := grafanatui.RendererForPanel("text").Render(nil, textPanel, 60, 5, nil)
	if !strings.Contains(textOutput, "Read this first") {
		t.Errorf("text content missing: %q", textOutput)
	}
}

func TestTimeSeriesRenderer_LegendCalculations(t *testing.T) {
	t.Parallel()

	result := fixtureMultiSeriesResult([]string{"cpu"}, 5)
	panel := grafana.Panel{
		Options: json.RawMessage(`{"legend":{"calcs":["lastNotNull","max"],"displayMode":"table","placement":"right"}}`),
	}
	output := grafanatui.RendererForPanel("timeseries").Render(result, panel, 100, 16, nil)
	for _, expected := range []string{"Last*", "Max"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
}
