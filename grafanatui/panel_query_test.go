//nolint:exhaustruct,testpackage // internal query helper and selection fixture
package grafanatui

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
)

func TestResolvedVariableOptionsDriveAllInterpolation(t *testing.T) {
	t.Parallel()

	variables := []grafana.TemplateVariable{{
		Name: "cluster", Type: "query", Multi: true, IncludeAll: true,
		Current: grafana.TemplateVariableCurrent{Value: json.RawMessage(`"$__all"`)},
	}}
	resolved := withResolvedVariableOptions(variables, map[string][]string{
		"cluster": {"$__all", "alpha", "beta"},
	})
	got := grafana.ResolveVariables(
		`up{cluster=~"$cluster"}`,
		resolved,
		map[string]string{"cluster": "$__all"},
	)
	want := `up{cluster=~"(alpha|beta)"}`
	if got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
	if len(variables[0].Options) != 0 {
		t.Error("saved dashboard options were mutated")
	}
}
