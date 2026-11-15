//nolint:exhaustruct // internal conversion tests vary relevant fields only
package grafana

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestClassicV2VariableHideModes(t *testing.T) {
	t.Parallel()

	kinds := []string{
		"DatasourceVariable", "QueryVariable", "CustomVariable",
		"ConstantVariable", "TextVariable", "IntervalVariable",
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
