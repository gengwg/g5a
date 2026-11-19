//nolint:exhaustruct // schema-v2 fixtures vary only the relevant fields
package grafana_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nestedRepeatScopeResource() string {
	return fmt.Sprintf(`{
		"spec":{
			"title":"Nested repeat scopes",
			"elements":{%s},
			"layout":{"kind":"RowsLayout","spec":{"rows":[
				{"kind":"RowsLayoutRow","spec":{
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"node"},
							"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
						}},
						{"kind":"RowsLayoutRow","spec":{
							"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
						}}
					]}}
				}},
				{"kind":"RowsLayoutRow","spec":{
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`, strings.Join([]string{v2TestElement(1), v2TestElement(2), v2TestElement(3)}, ","),
		v2TestItem(1, "GridLayoutItem"), v2TestItem(2, "GridLayoutItem"), v2TestItem(3, "GridLayoutItem"))
}

func TestGetDashboardV2NestedTabStaysAtomicInsideRepeatedRow(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Repeated row with repeated tab",
			"elements":{%s},
			"layout":{"kind":"RowsLayout","spec":{"rows":[
				{"kind":"RowsLayoutRow","spec":{
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"TabsLayout","spec":{"tabs":[
						{"kind":"TabsLayoutTab","spec":{
							"title":"Scoped tab",
							"repeat":{"mode":"variable","value":"region"},
							"layout":{"kind":"RowsLayout","spec":{"rows":[
								{"kind":"RowsLayoutRow","spec":{
									"repeat":{"mode":"variable","value":"node"},
									"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
								}}
							]}}
						}}
					]}}
				}},
				{"kind":"RowsLayoutRow","spec":{
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`, strings.Join([]string{v2TestElement(1), v2TestElement(2)}, ","),
		v2TestItem(1, "GridLayoutItem"), v2TestItem(2, "GridLayoutItem"))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "region", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a"}, "region": {"x", "y"}, "node": {"m", "n"}},
		map[string]string{"cluster": "a", "region": "$__all", "node": "$__all"},
	)
	require.Len(t, instances, 7)
	wantY := []int{0, 2, 7, 13, 15, 20, 27}
	keys := make(map[string]bool, len(instances))
	for index, instance := range instances {
		assert.Equal(t, wantY[index], instance.Panel.GridPos.Y)
		assert.False(t, keys[instance.Key], "duplicate instance key %q", instance.Key)
		keys[instance.Key] = true
	}
}

func TestGetDashboardV2ThreeNestedRowsKeepSiblingPanels(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Nested siblings",
			"elements":{%s},
			"layout":{"kind":"RowsLayout","spec":{"rows":[
				{"kind":"RowsLayoutRow","spec":{
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"region"},
							"layout":{"kind":"RowsLayout","spec":{"rows":[
								{"kind":"RowsLayoutRow","spec":{
									"repeat":{"mode":"variable","value":"node"},
									"layout":{"kind":"GridLayout","spec":{"items":[
										{"kind":"GridLayoutItem","spec":{"x":0,"y":0,"width":12,"height":4,
											"element":{"kind":"ElementReference","name":"panel-1"}}},
										{"kind":"GridLayoutItem","spec":{"x":12,"y":0,"width":12,"height":4,
											"element":{"kind":"ElementReference","name":"panel-2"}}}
									]}}
								}}
							]}}
						}}
					]}}
				}},
				{"kind":"RowsLayoutRow","spec":{
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`, strings.Join([]string{v2TestElement(1), v2TestElement(2), v2TestElement(3)}, ","), v2TestItem(3, "GridLayoutItem"))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "region", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a"}, "region": {"x"}, "node": {"x", "y"}},
		map[string]string{"cluster": "a", "region": "x", "node": "$__all"},
	)
	require.Len(t, instances, 5)
	wantY := []int{0, 0, 5, 5, 12}
	for index, want := range wantY {
		assert.Equal(t, want, instances[index].Panel.GridPos.Y)
	}
	assert.Equal(t, "x", instances[0].Variables["node"])
	assert.Equal(t, "x", instances[1].Variables["node"])
	assert.Equal(t, "y", instances[2].Variables["node"])
	assert.Equal(t, "y", instances[3].Variables["node"])
}

func TestGetDashboardV2MultiValueRepeatedTabIsAtomic(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Atomic repeated tabs",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"Scoped tab",
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"node"},
							"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
						}}
					]}}
				}},
				{"kind":"TabsLayoutTab","spec":{
					"title":"After",
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`, strings.Join([]string{v2TestElement(1), v2TestElement(2)}, ","),
		v2TestItem(1, "GridLayoutItem"), v2TestItem(2, "GridLayoutItem"))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a", "b"}, "node": {"x", "y"}},
		map[string]string{"cluster": "$__all", "node": "$__all"},
	)
	require.Len(t, instances, 8)
	wantY := []int{0, 2, 7, 13, 15, 20, 26, 28}
	for index, want := range wantY {
		assert.Equal(t, want, instances[index].Panel.GridPos.Y)
	}
	assert.Equal(t, "After", instances[6].Panel.Title)
}

func TestGetDashboardV2ThreeNestedRowLevels(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Three nested repeat levels",
			"elements":{%s},
			"layout":{"kind":"RowsLayout","spec":{"rows":[
				{"kind":"RowsLayoutRow","spec":{
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"region"},
							"layout":{"kind":"RowsLayout","spec":{"rows":[
								{"kind":"RowsLayoutRow","spec":{
									"repeat":{"mode":"variable","value":"node"},
									"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
								}}
							]}}
						}}
					]}}
				}},
				{"kind":"RowsLayoutRow","spec":{
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`, strings.Join([]string{v2TestElement(1), v2TestElement(2)}, ","),
		v2TestItem(1, "GridLayoutItem"), v2TestItem(2, "GridLayoutItem"))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "region", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a", "b"}, "region": {"x", "y"}, "node": {"m", "n"}},
		map[string]string{"cluster": "$__all", "region": "$__all", "node": "$__all"},
	)
	require.Len(t, instances, 9)
	wantY := []int{0, 5, 11, 16, 23, 28, 34, 39, 46}
	wantCluster := []string{"a", "a", "a", "a", "b", "b", "b", "b", "$__all"}
	wantRegion := []string{"x", "x", "y", "y", "x", "x", "y", "y", "$__all"}
	wantNode := []string{"m", "n", "m", "n", "m", "n", "m", "n", "$__all"}
	keys := make(map[string]bool, len(instances))
	for index, instance := range instances {
		assert.Equal(t, wantY[index], instance.Panel.GridPos.Y)
		assert.Equal(t, wantCluster[index], instance.Variables["cluster"])
		assert.Equal(t, wantRegion[index], instance.Variables["region"])
		assert.Equal(t, wantNode[index], instance.Variables["node"])
		assert.False(t, keys[instance.Key], "duplicate instance key %q", instance.Key)
		keys[instance.Key] = true
	}
}

func TestGetDashboardV2SingleOuterRepeatIsLayoutNeutral(t *testing.T) {
	t.Parallel()

	server := newV2TestServer(nestedRepeatScopeResource())
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a", "b"}, "node": {"x", "y"}},
		map[string]string{"cluster": "a", "node": "$__all"},
	)
	require.Len(t, instances, 4)
	wantY := []int{0, 5, 10, 16}
	for index, want := range wantY {
		assert.Equal(t, want, instances[index].Panel.GridPos.Y)
	}
}

func TestGetDashboardV2NestedRowsInsideTabOffset(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Nested rows in tabs",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"First",
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}},
				{"kind":"TabsLayoutTab","spec":{
					"title":"Second",
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"cluster"},
							"layout":{"kind":"RowsLayout","spec":{"rows":[
								{"kind":"RowsLayoutRow","spec":{
									"repeat":{"mode":"variable","value":"node"},
									"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
								}}
							]}}
						}}
					]}}
				}}
			]}}
		}
	}`, v2TestElement(1), v2TestItem(1, "GridLayoutItem"), v2TestItem(1, "GridLayoutItem"))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a"}, "node": {"x", "y"}},
		map[string]string{"cluster": "a", "node": "$__all"},
	)
	require.Len(t, instances, 5)
	assert.Equal(t, "First", instances[0].Panel.Title)
	assert.Equal(t, "Second", instances[2].Panel.Title)
	assert.Equal(t, 7, instances[2].Panel.GridPos.Y)
	assert.Equal(t, 9, instances[3].Panel.GridPos.Y)
	assert.Equal(t, 14, instances[4].Panel.GridPos.Y)
}

func TestGetDashboardV2RepeatedTabKeepsNestedRowRepeats(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Repeated tab with nested row",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"Scoped tab",
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"node"},
							"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
						}}
					]}}
				}}
			]}}
		}
	}`, v2TestElement(1), v2TestItem(1, "GridLayoutItem"))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	variables := []grafana.TemplateVariable{
		{Name: "cluster", Multi: true, IncludeAll: true},
		{Name: "node", Multi: true, IncludeAll: true},
	}
	instances := grafana.ExpandPanels(dashboard.Panels, variables,
		map[string][]string{"cluster": {"a"}, "node": {"x", "y"}},
		map[string]string{"cluster": "a", "node": "$__all"},
	)
	require.Len(t, instances, 3)
	assert.Equal(t, "Scoped tab", instances[0].Panel.Title)
	assert.Equal(t, "x", instances[1].Variables["node"])
	assert.Equal(t, "y", instances[2].Variables["node"])
}
