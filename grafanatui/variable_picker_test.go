//nolint:exhaustruct,testpackage // internal picker state needs direct tests
package grafanatui

import (
	"testing"

	"github.com/gengwg/g5a/grafana"
)

func TestVariablePickerMultiToggle(t *testing.T) {
	t.Parallel()

	variables := []grafana.TemplateVariable{{
		Name: "cluster", Multi: true, IncludeAll: true,
	}}
	picker := newVariablePickerView(
		variables,
		map[string][]string{"cluster": {"a", "b", "c"}},
		map[string]string{"cluster": "$__all"},
	)
	picker.editing = true

	picker.toggleOption("a")
	picker.toggleOption("b")
	if got := picker.values["cluster"]; got != "a,b" {
		t.Fatalf("values = %q, want a,b", got)
	}
	if !picker.optionSelected(variables[0], "a") || !picker.optionSelected(variables[0], "b") {
		t.Fatal("toggled values should be selected")
	}

	picker.toggleOption("a")
	if got := picker.values["cluster"]; got != "b" {
		t.Fatalf("values after second toggle = %q, want b", got)
	}
	picker.toggleOption(allOptionValue)
	if got := picker.values["cluster"]; got != allOptionValue {
		t.Fatalf("All selection = %q, want %q", got, allOptionValue)
	}
	if !picker.optionSelected(variables[0], allOptionValue) {
		t.Fatal("All should be selected")
	}
}

func TestVariablePickerAllIsExclusive(t *testing.T) {
	t.Parallel()

	variables := []grafana.TemplateVariable{{Name: "cluster", Multi: true, IncludeAll: true}}
	picker := newVariablePickerView(
		variables,
		map[string][]string{"cluster": {"a", "b"}},
		map[string]string{"cluster": "a,b"},
	)
	if !picker.optionSelected(variables[0], "a") {
		t.Fatal("saved multi-selection should be retained")
	}
	picker.toggleOption(allOptionValue)
	if values := variableSelectedValues(picker.values["cluster"]); len(values) != 0 {
		t.Fatalf("All should clear concrete selections, got %v", values)
	}
}
