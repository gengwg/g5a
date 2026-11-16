//nolint:exhaustruct,testpackage // internal presentation API needs direct tests
package grafanatui

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/gengwg/g5a/grafana"
)

func TestPresentFieldFleetValueMappings(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"defaults": {
			"mappings": [{
				"type": "value",
				"options": {
					"0.5": {"text": "not measured", "color": "blue"},
					"1": {"text": "one"},
					"2": {"text": "Degraded", "color": "orange"},
					"up": {"text": "Healthy", "color": "green"}
				}
			}],
			"thresholds": {
				"mode": "absolute",
				"steps": [
					{"color": "green", "value": null},
					{"color": "red", "value": 1}
				]
			}
		}
	}`)

	tests := []struct {
		name  string
		value any
		want  fieldPresentation
	}{
		{
			name:  "fleet not measured value and color",
			value: 0.5,
			want: fieldPresentation{
				Text: "not measured", DisplayName: "status", Color: "#5794F2",
			},
		},
		{
			name:  "fleet degraded value and color",
			value: 2.0,
			want: fieldPresentation{
				Text: "Degraded", DisplayName: "status", Color: "#FF9830",
			},
		},
		{
			name:  "mapping without color uses threshold",
			value: json.Number("1.0"),
			want: fieldPresentation{
				Text: "one", DisplayName: "status", Color: "#F2495C",
			},
		},
		{
			name:  "string value mapping",
			value: "up",
			want: fieldPresentation{
				Text: "Healthy", DisplayName: "status", Color: "#73BF69",
			},
		},
		{
			name:  "unmapped value uses threshold",
			value: 3.0,
			want: fieldPresentation{
				Text: "3", DisplayName: "status", Color: "#F2495C",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(raw, grafana.DataFrameField{Name: "status"}, tt.value)
			if got != tt.want {
				t.Fatalf("presentField() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPresentFieldOverrides(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"defaults": {
			"displayName": "Default",
			"unit": "short",
			"noValue": "default empty",
			"custom": {"width": 40, "align": "left"}
		},
		"overrides": [
			{
				"matcher": {"id": "byRegexp", "options": "^stat"},
				"properties": [
					{"id": "displayName", "value": "Regexp"},
					{"id": "decimals", "value": 1}
				]
			},
			{
				"matcher": {"id": "byName", "options": "status"},
				"properties": [
					{"id": "displayName", "value": "${__field.labels.host} status"},
					{"id": "unit", "value": "none"},
					{"id": "decimals", "value": 2},
					{"id": "noValue", "value": "offline"},
					{"id": "custom.width", "value": 140},
					{"id": "custom.align", "value": "center"}
				]
			},
			{
				"matcher": {"id": "byName", "options": "other"},
				"properties": [
					{"id": "displayName", "value": "Other"}
				]
			}
		]
	}`)

	tests := []struct {
		name  string
		field grafana.DataFrameField
		value any
		want  fieldPresentation
	}{
		{
			name: "matching overrides apply in order and interpolate labels",
			field: grafana.DataFrameField{
				Name: "status", Labels: map[string]string{"host": "api-1"},
			},
			value: 12.345,
			want: fieldPresentation{
				Text: "12.35", DisplayName: "api-1 status", Width: 140, Align: "center",
			},
		},
		{
			name:  "regexp override applies without later name override",
			field: grafana.DataFrameField{Name: "stat total"},
			value: 12.345,
			want: fieldPresentation{
				Text: "12.3", DisplayName: "Regexp", Width: 40, Align: "left",
			},
		},
		{
			name:  "nonmatching column retains defaults",
			field: grafana.DataFrameField{Name: "requests"},
			value: 1500.0,
			want: fieldPresentation{
				Text: "1.5K", DisplayName: "Default", Width: 40, Align: "left",
			},
		},
		{
			name: "column no value override",
			field: grafana.DataFrameField{
				Name: "status", Labels: map[string]string{"host": "api-1"},
			},
			value: nil,
			want: fieldPresentation{
				Text: "offline", DisplayName: "api-1 status", Width: 140, Align: "center",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(raw, tt.field, tt.value)
			if got != tt.want {
				t.Fatalf("presentField() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPresentFieldValueFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		value any
		want  string
	}{
		{
			name:  "configured decimals with float",
			raw:   `{"defaults":{"unit":"none","decimals":2}}`,
			value: 12.345,
			want:  "12.35",
		},
		{
			name:  "configured decimals with json number",
			raw:   `{"defaults":{"decimals":3}}`,
			value: json.Number("2.5"),
			want:  "2.500",
		},
		{
			name:  "zero decimals",
			raw:   `{"defaults":{"decimals":0}}`,
			value: 2.6,
			want:  "3",
		},
		{
			name:  "default uses existing unit formatting",
			raw:   `{"defaults":{"unit":"bytes"}}`,
			value: 1536.0,
			want:  "1.5 KiB",
		},
		{
			name:  "configured decimals preserve binary byte scaling",
			raw:   `{"defaults":{"unit":"bytes","decimals":2}}`,
			value: 1536.0,
			want:  "1.50 KiB",
		},
		{
			name:  "configured decimals preserve percent unit",
			raw:   `{"defaults":{"unit":"percent","decimals":2}}`,
			value: 12.345,
			want:  "12.35%",
		},
		{
			name:  "configured decimals scale percentunit",
			raw:   `{"defaults":{"unit":"percentunit","decimals":2}}`,
			value: 0.12345,
			want:  "12.35%",
		},
		{
			name:  "configured decimals preserve SI scaling",
			raw:   `{"defaults":{"unit":"short","decimals":2}}`,
			value: 1500.0,
			want:  "1.50K",
		},
		{
			name:  "json number uses existing unit formatting",
			raw:   `{"defaults":{"unit":"short"}}`,
			value: json.Number("1500"),
			want:  "1.5K",
		},
		{
			name:  "nil uses no value",
			raw:   `{"defaults":{"noValue":"N/A"}}`,
			value: nil,
			want:  "N/A",
		},
		{
			name:  "nil defaults to empty",
			raw:   `{}`,
			value: nil,
			want:  "",
		},
		{
			name:  "string displays naturally",
			raw:   `{"defaults":{"decimals":2,"unit":"bytes"}}`,
			value: "healthy",
			want:  "healthy",
		},
		{
			name:  "boolean displays naturally",
			raw:   `{}`,
			value: true,
			want:  "true",
		},
		{
			name:  "invalid json number displays naturally",
			raw:   `{}`,
			value: json.Number("not-a-number"),
			want:  "not-a-number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(
				json.RawMessage(tt.raw),
				grafana.DataFrameField{Name: "value"},
				tt.value,
			)
			if got.Text != tt.want {
				t.Fatalf("presentField().Text = %q, want %q", got.Text, tt.want)
			}
		})
	}
}

func TestPresentFieldDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		field grafana.DataFrameField
		want  string
	}{
		{
			name:  "defaults to field name",
			raw:   `{}`,
			field: grafana.DataFrameField{Name: "status"},
			want:  "status",
		},
		{
			name: "interpolates all referenced labels",
			raw: `{"defaults":{
				"displayName":"${__field.labels.job} on ${__field.labels.instance}"
			}}`,
			field: grafana.DataFrameField{
				Name:   "status",
				Labels: map[string]string{"job": "api", "instance": "api-1:9090"},
			},
			want: "api on api-1:9090",
		},
		{
			name: "unknown label placeholder remains unchanged",
			raw:  `{"defaults":{"displayName":"${__field.labels.missing}"}}`,
			field: grafana.DataFrameField{
				Name: "status", Labels: map[string]string{"job": "api"},
			},
			want: "${__field.labels.missing}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(json.RawMessage(tt.raw), tt.field, 1.0)
			if got.DisplayName != tt.want {
				t.Fatalf(
					"presentField().DisplayName = %q, want %q",
					got.DisplayName, tt.want,
				)
			}
		})
	}
}

func TestPresentFieldThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		raw   string
		value float64
		want  string
	}{
		{
			name: "absolute base",
			raw: `{"defaults":{"thresholds":{"mode":"absolute","steps":[
				{"color":"green","value":null},
				{"color":"orange","value":50},
				{"color":"red","value":80}
			]}}}`,
			value: 10,
			want:  "#73BF69",
		},
		{
			name: "absolute inclusive step",
			raw: `{"defaults":{"thresholds":{"mode":"absolute","steps":[
				{"color":"green","value":null},
				{"color":"orange","value":50},
				{"color":"red","value":80}
			]}}}`,
			value: 50,
			want:  "#FF9830",
		},
		{
			name: "absolute highest step",
			raw: `{"defaults":{"thresholds":{"mode":"absolute","steps":[
				{"color":"green","value":null},
				{"color":"orange","value":50},
				{"color":"red","value":80}
			]}}}`,
			value: 90,
			want:  "#F2495C",
		},
		{
			name: "percentage uses configured min and max",
			raw: `{"defaults":{"min":0,"max":200,"thresholds":{
				"mode":"percentage",
				"steps":[
					{"color":"green","value":null},
					{"color":"yellow","value":25},
					{"color":"orange","value":50},
					{"color":"red","value":75}
				]
			}}}`,
			value: 50,
			want:  "#FADE2A",
		},
		{
			name: "percentage upper step",
			raw: `{"defaults":{"min":0,"max":200,"thresholds":{
				"mode":"percentage",
				"steps":[
					{"color":"green","value":null},
					{"color":"yellow","value":25},
					{"color":"orange","value":50},
					{"color":"red","value":75}
				]
			}}}`,
			value: 150,
			want:  "#F2495C",
		},
		{
			name:  "no thresholds leaves color empty",
			raw:   `{"defaults":{}}`,
			value: 100,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(
				json.RawMessage(tt.raw),
				grafana.DataFrameField{Name: "load"},
				tt.value,
			)
			if got.Color != tt.want {
				t.Fatalf("presentField().Color = %q, want %q", got.Color, tt.want)
			}
		})
	}
}

func TestPresentFieldThresholdOverrides(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"defaults": {
			"min": 0,
			"max": 100,
			"thresholds": {
				"mode": "absolute",
				"steps": [
					{"color": "green", "value": null},
					{"color": "yellow", "value": 50}
				]
			}
		},
		"overrides": [{
			"matcher": {"id": "byName", "options": "load"},
			"properties": [
				{"id": "min", "value": 0},
				{"id": "max", "value": 200},
				{"id": "thresholds", "value": {
					"mode": "percentage",
					"steps": [
						{"color": "blue", "value": null},
						{"color": "red", "value": 50}
					]
				}}
			]
		}]
	}`)

	load := presentField(raw, grafana.DataFrameField{Name: "load"}, 100.0)
	if load.Color != "#F2495C" {
		t.Fatalf("overridden threshold color = %q, want #F2495C", load.Color)
	}

	other := presentField(raw, grafana.DataFrameField{Name: "other"}, 100.0)
	if other.Color != "#FADE2A" {
		t.Fatalf("default threshold color = %q, want #FADE2A", other.Color)
	}
}

func TestPresentFieldRangeRegexAndSpecialMappings(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"defaults": {
			"noValue": "No data",
			"mappings": [
				{
					"type": "range",
					"options": {
						"from": 0,
						"to": 10,
						"result": {"text": "Low", "color": "green"}
					}
				},
				{
					"type": "regex",
					"options": {
						"pattern": "^api-[0-9]+$",
						"result": {"text": "API", "color": "purple"}
					}
				},
				{
					"type": "special",
					"options": {
						"match": "null",
						"result": {"text": "Missing", "color": "yellow"}
					}
				},
				{
					"type": "special",
					"options": {
						"match": "nan",
						"result": {"text": "Not numeric", "color": "red"}
					}
				}
			]
		}
	}`)

	tests := []struct {
		name      string
		value     any
		wantText  string
		wantColor string
	}{
		{name: "range", value: 5.0, wantText: "Low", wantColor: "#73BF69"},
		{name: "range upper bound inclusive", value: 10.0, wantText: "Low", wantColor: "#73BF69"},
		{name: "outside range", value: 11.0, wantText: "11", wantColor: ""},
		{name: "regex", value: "api-12", wantText: "API", wantColor: "#B877D9"},
		{name: "regex mismatch", value: "worker-1", wantText: "worker-1", wantColor: ""},
		{name: "null replaces no value", value: nil, wantText: "Missing", wantColor: "#FADE2A"},
		{name: "NaN", value: math.NaN(), wantText: "Not numeric", wantColor: "#F2495C"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(raw, grafana.DataFrameField{Name: "status"}, tt.value)
			if got.Text != tt.wantText || got.Color != tt.wantColor {
				t.Fatalf(
					"presentField() text/color = %q/%q, want %q/%q",
					got.Text, got.Color, tt.wantText, tt.wantColor,
				)
			}
		})
	}
}

func TestPresentFieldNullAndNaNMapping(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"defaults":{"mappings":[{
		"type":"special",
		"options":{
			"match":"null+nan",
			"result":{"text":"Unavailable","color":"orange"}
		}
	}]}}`)

	for _, value := range []any{nil, math.NaN()} {
		got := presentField(raw, grafana.DataFrameField{Name: "status"}, value)
		if got.Text != "Unavailable" || got.Color != "#FF9830" {
			t.Fatalf(
				"presentField(%v) text/color = %q/%q, want Unavailable/#FF9830",
				value, got.Text, got.Color,
			)
		}
	}
}

func TestPresentFieldRangeMappingFallsBackToThresholdColor(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"defaults":{
		"mappings":[{
			"type":"range",
			"options":{"from":0,"to":10,"result":{"text":"In range"}}
		}],
		"thresholds":{"steps":[{"color":"green","value":null}]}
	}}`)

	got := presentField(raw, grafana.DataFrameField{Name: "value"}, 5.0)
	if got.Text != "In range" || got.Color != "#73BF69" {
		t.Fatalf(
			"presentField() text/color = %q/%q, want In range/#73BF69",
			got.Text, got.Color,
		)
	}
}

func TestPresentFieldCellOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		raw            string
		wantBackground bool
		wantWidth      int
		wantAlign      string
	}{
		{
			name: "color background with basic mode width and align",
			raw: `{"defaults":{"custom":{
				"cellOptions":{"type":"color-background","mode":"basic"},
				"width":120,
				"align":"right"
			}}}`,
			wantBackground: true,
			wantWidth:      120,
			wantAlign:      "right",
		},
		{
			name: "nested basic color background",
			raw: `{"defaults":{"custom":{"cellOptions":{
				"mode":{"basic":{"type":"color-background"}}
			}}}}`,
			wantBackground: true,
		},
		{
			name: "other cell type",
			raw: `{"defaults":{"custom":{
				"cellOptions":{"type":"gradient-gauge"},
				"width":80,
				"align":"center"
			}}}`,
			wantBackground: false,
			wantWidth:      80,
			wantAlign:      "center",
		},
		{
			name: "later override resolves cell options",
			raw: `{
				"defaults":{"custom":{
					"cellOptions":{"type":"color-background"},
					"width":100
				}},
				"overrides":[{
					"matcher":{"id":"byName","options":"status"},
					"properties":[
						{"id":"custom.cellOptions","value":{"type":"auto"}},
						{"id":"custom.align","value":"center"}
					]
				}]
			}`,
			wantBackground: false,
			wantWidth:      100,
			wantAlign:      "center",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(
				json.RawMessage(tt.raw),
				grafana.DataFrameField{Name: "status"},
				1.0,
			)
			if got.Background != tt.wantBackground ||
				got.Width != tt.wantWidth || got.Align != tt.wantAlign {
				t.Fatalf(
					"presentField() background/width/align = %v/%d/%q, want %v/%d/%q",
					got.Background, got.Width, got.Align,
					tt.wantBackground, tt.wantWidth, tt.wantAlign,
				)
			}
		})
	}
}

func TestPresentFieldInvalidConfigFallsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want fieldPresentation
	}{
		{
			name: "invalid JSON",
			raw:  `{invalid`,
			want: fieldPresentation{Text: "1.50", DisplayName: "status"},
		},
		{
			name: "invalid override regexp is ignored",
			raw: `{
				"defaults":{"unit":"none"},
				"overrides":[{
					"matcher":{"id":"byRegexp","options":"["},
					"properties":[{"id":"decimals","value":0}]
				}]
			}`,
			want: fieldPresentation{Text: "1.50", DisplayName: "status"},
		},
		{
			name: "malformed mapping does not panic",
			raw: `{"defaults":{"mappings":[{
				"type":"range",
				"options":{"from":"bad","to":10}
			}]}}`,
			want: fieldPresentation{Text: "1.50", DisplayName: "status"},
		},
		{
			name: "invalid property type is ignored",
			raw: `{
				"defaults":{"unit":"none","decimals":1},
				"overrides":[{
					"matcher":{"id":"byName","options":"status"},
					"properties":[{"id":"decimals","value":"many"}]
				}]
			}`,
			want: fieldPresentation{Text: "1.5", DisplayName: "status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := presentField(
				json.RawMessage(tt.raw),
				grafana.DataFrameField{Name: "status"},
				1.5,
			)
			if got != tt.want {
				t.Fatalf("presentField() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPresentFieldDataFrameConfig(t *testing.T) {
	t.Parallel()

	field := grafana.DataFrameField{
		Name:   "value",
		Labels: map[string]string{"host": "api-1"},
		Config: json.RawMessage(`{
			"displayName":"Response ${__field.labels.host}",
			"unit":"bytes",
			"decimals":2,
			"mappings":[{"type":"value","options":{
				"2048":{"text":"two kibibytes","color":"orange"}
			}}],
			"thresholds":{"mode":"absolute","steps":[
				{"color":"green","value":null},
				{"color":"red","value":1000}
			]}
		}`),
	}

	got := presentField(nil, field, 1536.0)
	want := fieldPresentation{
		Text: "1.50 KiB", DisplayName: "Response api-1", Color: "#F2495C",
	}
	if got != want {
		t.Fatalf("presentField() with response config = %#v, want %#v", got, want)
	}

	mapped := presentField(nil, field, 2048.0)
	if mapped.Text != "two kibibytes" || mapped.Color != "#FF9830" {
		t.Fatalf(
			"response mapping text/color = %q/%q, want two kibibytes/#FF9830",
			mapped.Text, mapped.Color,
		)
	}

	defaulted := presentField(
		json.RawMessage(`{"defaults":{"displayName":"Panel","unit":"percent","decimals":1}}`),
		field,
		1536.0,
	)
	if defaulted.Text != "1536.0%" || defaulted.DisplayName != "Panel" {
		t.Fatalf(
			"panel defaults should override response config, got text/name %q/%q",
			defaulted.Text, defaulted.DisplayName,
		)
	}

	overridden := presentField(json.RawMessage(`{
		"defaults":{"displayName":"Panel","unit":"percent","decimals":1},
		"overrides":[
			{
				"matcher":{"id":"byRegexp","options":"^value$"},
				"properties":[
					{"id":"displayName","value":"Regexp"},
					{"id":"unit","value":"decbytes"},
					{"id":"decimals","value":0}
				]
			},
			{
				"matcher":{"id":"byName","options":"value"},
				"properties":[
					{"id":"displayName","value":"Override"},
					{"id":"unit","value":"bytes"},
					{"id":"decimals","value":2}
				]
			}
		]
	}`), field, 1536.0)
	if overridden.Text != "1.50 KiB" || overridden.DisplayName != "Override" {
		t.Fatalf(
			"ordered overrides should follow response config and defaults, got text/name %q/%q",
			overridden.Text, overridden.DisplayName,
		)
	}
}

func TestPresentFieldDatasourceDisplayName(t *testing.T) {
	t.Parallel()

	field := grafana.DataFrameField{
		Name:   "Value",
		Config: json.RawMessage(`{"displayNameFromDS":"{cluster=\"alpha\"}"}`),
	}
	got := presentField(nil, field, 1.0)
	if got.DisplayName != "alpha" {
		t.Errorf("display name = %q, want compact label value", got.DisplayName)
	}

	metric := field
	metric.Config = json.RawMessage(`{
		"displayNameFromDS":"namespace_workload:cpu_usage:sum{namespace=\"argo-rollouts\"}"
	}`)
	if got := presentField(nil, metric, 1.0); got.DisplayName != "namespace_workload:cpu_usage:sum" {
		t.Errorf("display name = %q, want metric name without labels", got.DisplayName)
	}

	quoted := field
	quoted.Config = json.RawMessage(`{"displayNameFromDS":"{vendor=\"Dell, Inc.\"}"}`)
	if got := presentField(nil, quoted, 1.0); got.DisplayName != "Dell, Inc." {
		t.Errorf("display name = %q, want quoted comma value preserved", got.DisplayName)
	}

	overridden := presentField(
		json.RawMessage(`{"defaults":{"displayName":"custom"}}`),
		field,
		1.0,
	)
	if overridden.DisplayName != "custom" {
		t.Errorf("panel display name should take precedence, got %q", overridden.DisplayName)
	}

	explicit := field
	explicit.Config = json.RawMessage(`{
		"displayName":"Response name",
		"displayNameFromDS":"Datasource name"
	}`)
	if got := presentField(nil, explicit, 1.0); got.DisplayName != "Response name" {
		t.Errorf("explicit response display name should take precedence, got %q", got.DisplayName)
	}

	renamed := field
	renamed.Name = "Renamed"
	renamed.Config = json.RawMessage(`{"unit":"bytes"}`)
	if got := presentField(nil, renamed, 1.0); got.DisplayName != "Renamed" {
		t.Errorf("organize rename should be used after datasource name removal, got %q", got.DisplayName)
	}
}
