//nolint:cyclop,exhaustruct // table-driven fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"testing"

	"github.com/gengwg/g5a/grafana"
)

func TestExpandPanels_HorizontalRepeat(t *testing.T) {
	t.Parallel()

	variables := []grafana.TemplateVariable{{
		Name:       "cluster",
		Type:       "query",
		Multi:      true,
		IncludeAll: true,
		Current: grafana.TemplateVariableCurrent{
			Text:  json.RawMessage(`"All"`),
			Value: json.RawMessage(`"$__all"`),
		},
	}}
	panels := []grafana.Panel{
		{
			ID: 7, Type: "stat", Title: "$cluster", Repeat: "cluster",
			RepeatDirection: "h", MaxPerRow: 2,
			GridPos: grafana.GridPos{X: 0, Y: 3, W: 24, H: 4},
		},
		{
			ID: 8, Type: "table", Title: "Following",
			GridPos: grafana.GridPos{X: 0, Y: 7, W: 24, H: 8},
		},
	}

	instances := grafana.ExpandPanels(
		panels,
		variables,
		map[string][]string{"cluster": {"a", "b", "c"}},
		map[string]string{"cluster": "$__all"},
	)
	if len(instances) != 4 {
		t.Fatalf("got %d instances, want 4", len(instances))
	}

	for i, wantTitle := range []string{"a", "b", "c"} {
		if instances[i].Panel.Title != wantTitle {
			t.Errorf("panel %d title = %q, want %q", i, instances[i].Panel.Title, wantTitle)
		}
		if instances[i].Variables["cluster"] != wantTitle {
			t.Errorf("panel %d scoped cluster = %q, want %q", i, instances[i].Variables["cluster"], wantTitle)
		}
	}
	if instances[0].Panel.GridPos.Y != 3 || instances[1].Panel.GridPos.Y != 3 {
		t.Error("first repeated row should retain its Y position")
	}
	if instances[2].Panel.GridPos.Y != 7 {
		t.Errorf("second repeated row Y = %d, want 7", instances[2].Panel.GridPos.Y)
	}
	if instances[2].RepeatIndex != 0 || instances[2].RepeatLength != 2 {
		t.Errorf("second row repeat metadata = %d/%d, want 0/2", instances[2].RepeatIndex, instances[2].RepeatLength)
	}
	if instances[3].Panel.GridPos.Y != 11 {
		t.Errorf("following panel Y = %d, want 11", instances[3].Panel.GridPos.Y)
	}
}

func TestRepeatValues(t *testing.T) {
	t.Parallel()

	variable := grafana.TemplateVariable{IncludeAll: true, Multi: true}
	tests := []struct {
		name     string
		selected string
		want     []string
	}{
		{name: "all token", selected: "$__all", want: []string{"a", "b"}},
		{name: "all regex", selected: ".*", want: []string{"a", "b"}},
		{name: "single", selected: "a", want: []string{"a"}},
		{name: "multiple", selected: "a,b", want: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := grafana.RepeatValues(variable, []string{"a", "b"}, tt.selected)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("value %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
