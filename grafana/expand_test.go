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

func TestExpandPanels_ExpandedRowRepeat(t *testing.T) {
	t.Parallel()

	variables := []grafana.TemplateVariable{{
		Name: "cluster", Multi: true, IncludeAll: true,
	}}
	panels := []grafana.Panel{
		{ID: 1, Type: "row", Repeat: "cluster", GridPos: grafana.GridPos{Y: 2, H: 1}},
		{ID: 10, Type: "stat", Title: "$cluster one", GridPos: grafana.GridPos{Y: 3, H: 4, W: 12}},
		{ID: 11, Type: "table", Title: "$cluster two", GridPos: grafana.GridPos{Y: 7, H: 3, W: 24}},
		{ID: 2, Type: "row", GridPos: grafana.GridPos{Y: 10, H: 1}},
		{ID: 12, Type: "stat", Title: "Following", GridPos: grafana.GridPos{Y: 11, H: 4, W: 24}},
	}

	instances := grafana.ExpandPanels(
		panels, variables,
		map[string][]string{"cluster": {"a", "b"}},
		map[string]string{"cluster": "$__all"},
	)
	if len(instances) != 5 {
		t.Fatalf("got %d instances, want 5", len(instances))
	}
	wantTitles := []string{"a one", "a two", "b one", "b two", "Following"}
	wantY := []int{3, 7, 11, 15, 19}
	for index, instance := range instances {
		if instance.Panel.Title != wantTitles[index] {
			t.Errorf("panel %d title = %q, want %q", index, instance.Panel.Title, wantTitles[index])
		}
		if instance.Panel.GridPos.Y != wantY[index] {
			t.Errorf("panel %d Y = %d, want %d", index, instance.Panel.GridPos.Y, wantY[index])
		}
	}
	assertUniqueInstanceKeys(t, instances)
}

func TestExpandPanels_CollapsedRowAndPanelRepeats(t *testing.T) {
	t.Parallel()

	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	panels := []grafana.Panel{
		{
			ID: 1, Type: "row", Repeat: "cluster", Collapsed: true, GridPos: grafana.GridPos{Y: 2, H: 1},
			Panels: []grafana.Panel{
				{ID: 21, Type: "stat", Title: "$cluster first", GridPos: grafana.GridPos{Y: 3, H: 4, W: 24}},
				{
					ID: 22, Type: "stat", Title: "$cluster $node",
					Repeat: "node", RepeatDirection: "v", GridPos: grafana.GridPos{Y: 7, H: 3, W: 24},
				},
			},
		},
		{ID: 23, Type: "stat", Title: "Following", GridPos: grafana.GridPos{Y: 10, H: 4, W: 24}},
	}

	instances := grafana.ExpandPanels(
		panels, variables,
		map[string][]string{"cluster": {"a", "b"}, "node": {"x", "y"}},
		map[string]string{"cluster": "$__all", "node": "$__all"},
	)
	if len(instances) != 7 {
		t.Fatalf("got %d instances, want 7", len(instances))
	}
	wantTitles := []string{"a first", "a x", "a y", "b first", "b x", "b y", "Following"}
	wantY := []int{3, 7, 10, 14, 18, 21, 24}
	for index, instance := range instances {
		if instance.Panel.Title != wantTitles[index] {
			t.Errorf("panel %d title = %q, want %q", index, instance.Panel.Title, wantTitles[index])
		}
		if instance.Panel.GridPos.Y != wantY[index] {
			t.Errorf("panel %d Y = %d, want %d", index, instance.Panel.GridPos.Y, wantY[index])
		}
	}
	assertUniqueInstanceKeys(t, instances)
}

func TestExpandPanels_RowRepeatPreservesIDLessPanelKeys(t *testing.T) {
	t.Parallel()

	panels := []grafana.Panel{
		{ID: 0, Type: "stat", Title: "Before", GridPos: grafana.GridPos{Y: 0, H: 2, W: 24}},
		{
			ID: 1, Type: "row", Repeat: "cluster", Collapsed: true,
			GridPos: grafana.GridPos{Y: 2, H: 1},
			Panels: []grafana.Panel{{
				ID: 2, Type: "stat", Title: "$cluster", GridPos: grafana.GridPos{Y: 3, H: 2, W: 24},
			}},
		},
		{ID: 0, Type: "stat", Title: "After", GridPos: grafana.GridPos{Y: 5, H: 2, W: 24}},
	}
	variables := make([]grafana.TemplateVariable, 1)
	variables[0].Name = "cluster"
	variables[0].Multi = true
	variables[0].IncludeAll = true

	instances := grafana.ExpandPanels(
		panels, variables,
		map[string][]string{"cluster": {"a", "b"}},
		map[string]string{"cluster": "$__all"},
	)
	if len(instances) != 4 {
		t.Fatalf("got %d instances, want 4", len(instances))
	}
	assertUniqueInstanceKeys(t, instances)
}

func TestExpandPanels_EmptyCollapsedRowDoesNotOwnFollowingPanel(t *testing.T) {
	t.Parallel()

	panels := []grafana.Panel{
		{ID: 1, Type: "row", Repeat: "cluster", Collapsed: true, GridPos: grafana.GridPos{Y: 0, H: 1}},
		{ID: 2, Type: "stat", Title: "Outside", GridPos: grafana.GridPos{Y: 1, H: 2, W: 24}},
	}
	variables := make([]grafana.TemplateVariable, 1)
	variables[0].Name = "cluster"
	variables[0].Multi = true
	variables[0].IncludeAll = true

	instances := grafana.ExpandPanels(
		panels, variables,
		map[string][]string{"cluster": {"a", "b"}},
		map[string]string{"cluster": "$__all"},
	)
	if len(instances) != 1 || instances[0].Panel.Title != "Outside" {
		t.Fatalf("empty collapsed row claimed the following panel: %+v", instances)
	}
}

func assertUniqueInstanceKeys(t *testing.T, instances []grafana.PanelInstance) {
	t.Helper()
	keys := make(map[string]bool, len(instances))
	for _, instance := range instances {
		if keys[instance.Key] {
			t.Errorf("duplicate instance key %q", instance.Key)
		}
		keys[instance.Key] = true
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
