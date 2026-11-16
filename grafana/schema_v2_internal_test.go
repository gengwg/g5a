//nolint:exhaustruct // internal conversion tests vary relevant fields only
package grafana

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

func TestDashboardNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		host  string
		orgID string
		want  string
	}{
		{name: "default", host: "grafana.example.com", want: "default"},
		{name: "other org", host: "grafana.example.com", orgID: "2", want: "org-2"},
		{name: "cloud org", host: "stack.grafana.net", orgID: "123", want: "stacks-123"},
		{name: "cloud host", host: "stack.grafana.net", want: "stacks-stack"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &Client{orgID: test.orgID, baseURL: &url.URL{Scheme: "https", Host: test.host}}
			if got := client.dashboardNamespace(); got != test.want {
				t.Errorf("namespace = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClassicV2VariableHideModes(t *testing.T) {
	t.Parallel()

	kinds := []string{
		"DatasourceVariable", "QueryVariable", "CustomVariable",
		"ConstantVariable", "TextVariable", "IntervalVariable", v2SwitchVariableKind,
	}
	modes := map[string]int{
		"dontHide": 0, "hideLabel": 1, "hideVariable": 2, "inControlsMenu": 3,
	}
	for _, kind := range kinds {
		for mode, want := range modes {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				t.Parallel()
				source := dashboardV2Variable{
					Kind: kind,
					Spec: json.RawMessage(`{"name":"v","hide":` + string(mustMarshal(mode)) + `}`),
				}
				variable, err := classicV2Variable(source)
				if err != nil {
					t.Fatalf("classicV2Variable: %v", err)
				}
				if variable.Hide != want {
					t.Errorf("hide = %d, want %d", variable.Hide, want)
				}
			})
		}
	}
}

func TestClassicV2SwitchVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		spec         string
		wantOptions  []string
		wantSelected string
	}{
		{
			name: "custom values", spec: `{"name":"switch","current":"on","enabledValue":"on","disabledValue":"off"}`,
			wantOptions: []string{"on", "off"}, wantSelected: "on",
		},
		{
			name: "defaults", spec: `{"name":"switch"}`,
			wantOptions: []string{"true", "false"}, wantSelected: "false",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			variable, err := classicV2Variable(dashboardV2Variable{
				Kind: v2SwitchVariableKind, Spec: json.RawMessage(test.spec),
			})
			if err != nil {
				t.Fatalf("classicV2Variable: %v", err)
			}
			if variable.Type != variableTypeCustom {
				t.Errorf("type = %q, want custom", variable.Type)
			}
			options := parseCustomVariableOptions(variable)
			if len(options) != len(test.wantOptions) {
				t.Fatalf("options = %v, want %v", options, test.wantOptions)
			}
			for index, want := range test.wantOptions {
				if options[index] != want {
					t.Errorf("option %d = %q, want %q", index, options[index], want)
				}
			}
			selected := defaultVariableSelection(
				variable, options, resolveCurrentValue(variable),
			)
			if selected != test.wantSelected {
				t.Errorf("selection = %q, want %q", selected, test.wantSelected)
			}
		})
	}
}

func TestClassicV2VariableArrayOptions(t *testing.T) {
	t.Parallel()

	source := dashboardV2Variable{
		Kind: "CustomVariable",
		Spec: json.RawMessage(`{
			"name":"v",
			"options":[
				{"text":["One","Two"],"value":["1","2"],"selected":true},
				{"text":"Three","value":["3","4"]}
			]
		}`),
	}
	variable, err := classicV2Variable(source)
	if err != nil {
		t.Fatalf("classicV2Variable: %v", err)
	}
	if len(variable.Options) != 4 {
		t.Fatalf("options = %v, want four flattened options", variable.Options)
	}
	want := []TemplateVariableOption{
		{Text: "One", Value: "1", Selected: true},
		{Text: "Two", Value: "2", Selected: true},
		{Text: "Three", Value: "3", Selected: false},
		{Text: "Three", Value: "4", Selected: false},
	}
	for index, option := range want {
		if variable.Options[index] != option {
			t.Errorf("option %d = %#v, want %#v", index, variable.Options[index], option)
		}
	}
}

func TestSetV2PanelDataRejectsNullQuery(t *testing.T) {
	t.Parallel()

	element := dashboardV2Element{}
	element.Spec.Data.Spec.Queries = append(element.Spec.Data.Spec.Queries, dashboardV2Query{})
	element.Spec.Data.Spec.Queries[0].Spec.Query.Spec = json.RawMessage("null")
	panel := Panel{}
	if err := setV2PanelData(&panel, element); !errors.Is(err, errInvalidV2Query) {
		t.Errorf("error = %v, want errInvalidV2Query", err)
	}
}

func TestDashboardRefID(t *testing.T) {
	t.Parallel()

	want := map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA"}
	for index, expected := range want {
		if got := dashboardRefID(index); got != expected {
			t.Errorf("dashboardRefID(%d) = %q, want %q", index, got, expected)
		}
	}
}

func TestQueryResultOptionsIncludeMetricValueAndTimestamp(t *testing.T) {
	t.Parallel()

	frame := DataFrame{}
	frame.Schema.Fields = []DataFrameField{
		{Name: "Time", Type: "time"},
		{Name: "up", Type: "number", Labels: map[string]string{"cluster": "alpha"}},
	}
	frame.Data.Values = []json.RawMessage{json.RawMessage(`[1000]`), json.RawMessage(`[1]`)}
	options := variableOptionsFromFrames(&QueryResult{Results: map[string]QueryResultData{
		"A": {Frames: []DataFrame{frame}},
	}})
	if len(options) != 1 || options[0] != `up{cluster="alpha"} 1 1000` {
		t.Errorf("options = %v, want metric value timestamp", options)
	}
}
