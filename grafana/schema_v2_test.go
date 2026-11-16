package grafana_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDashboardV2GridLayout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dashboards/uid/demo" {
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/demo" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"apiVersion":"dashboard.grafana.app/v2",
			"spec":{
				"title":"V2 demo",
				"timeSettings":{"from":"now-3h","to":"now","autoRefresh":"30s",
				"timezone":"UTC","weekStart":"monday","nowDelay":"1h"},
				"elements":{"panel-7":{
					"kind":"Panel",
					"spec":{
						"id":7,"title":"CPU",
						"vizConfig":{"kind":"VizConfig","group":"timeseries","version":"1.0","spec":{
							"fieldConfig":{"defaults":{"unit":"percent"}},
							"options":{"legend":{"placement":"right"}}
						}},
						"data":{"kind":"QueryGroup","spec":{
							"queries":[{"kind":"PanelQuery","spec":{
								"refId":"","hidden":false,
								"query":{"kind":"DataQuery","group":"prometheus","datasource":{"name":"prom-1"},"spec":{"expr":"rate(cpu[5m])"}}
							}}],
							"transformations":[{"kind":"Transformation","group":"sortBy","spec":{"options":{"sort":[]}}}]
						}}
					}
				}},
				"layout":{"kind":"GridLayout","spec":{"items":[{"kind":"GridLayoutItem","spec":{
					"x":4,"y":2,"width":12,"height":6,
					"repeat":{"value":"cluster","direction":"h","maxPerRow":3},
					"element":{"kind":"ElementReference","name":"panel-7"}
				}}]}},
				"variables":[{"kind":"QueryVariable","spec":{
					"name":"cluster","multi":true,"includeAll":true,
					"current":{"text":["a"],"value":["a"]},
					"refresh":"onTimeRangeChanged","sort":"alphabeticalAsc",
					"query":{"kind":"DataQuery","group":"prometheus",
						"datasource":{"name":"prom-1"},"spec":{"expr":"label_values(up, cluster)"}}
				}}]
			}
		}`))
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "V2 demo", dashboard.Title)
	assert.Equal(t, "now-3h", dashboard.Time.From)
	assert.Equal(t, grafana.RefreshInterval("30s"), dashboard.Refresh)
	assert.Equal(t, "UTC", dashboard.Timezone)
	assert.Equal(t, "monday", dashboard.WeekStart)
	assert.Equal(t, "1h", dashboard.NowDelay)
	require.Len(t, dashboard.Panels, 1)
	panel := dashboard.Panels[0]
	assert.Equal(t, 7, panel.ID)
	assert.Equal(t, "timeseries", panel.Type)
	assert.Equal(t, grafana.GridPos{H: 6, W: 12, X: 4, Y: 2}, panel.GridPos)
	assert.Equal(t, "cluster", panel.Repeat)
	assert.Equal(t, "h", panel.RepeatDirection)
	assert.Equal(t, 3, panel.MaxPerRow)
	assert.JSONEq(t, `{"legend":{"placement":"right"}}`, string(panel.Options))
	assert.JSONEq(t, `{"defaults":{"unit":"percent"}}`, string(panel.FieldConfig))
	require.Len(t, panel.Targets, 1)
	var target map[string]any
	require.NoError(t, json.Unmarshal(panel.Targets[0].Raw(), &target))
	assert.Equal(t, "A", target["refId"])
	assert.Equal(t, "rate(cpu[5m])", target["expr"])
	assert.Equal(t, "prom-1", target["datasource"].(map[string]any)["uid"])
	assert.Equal(t, "prometheus", target["datasource"].(map[string]any)["type"])
	require.Len(t, panel.Transformations, 1)
	assert.Equal(t, "sortBy", panel.Transformations[0].ID)

	require.Len(t, dashboard.Templating.List, 1)
	variable := dashboard.Templating.List[0]
	assert.Equal(t, "query", variable.Type)
	assert.Equal(t, "label_values(up, cluster)", variable.Definition)
	assert.Equal(t, "prom-1", variable.Datasource.UID)
	assert.Equal(t, "prometheus", variable.Datasource.Type)
	assert.Equal(t, 2, variable.Refresh)
	assert.Equal(t, 1, variable.Sort)
	assert.True(t, variable.Multi)
	assert.True(t, variable.IncludeAll)
}

func TestGetDashboardV2LibraryPanel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dashboards/uid/demo":
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
		case "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/demo":
			_, _ = w.Write([]byte(`{
				"spec":{
					"title":"Library",
					"elements":{
						"library-1":{"kind":"LibraryPanel","spec":{
							"id":9,"title":"Shared",
							"libraryPanel":{"uid":"shared","name":"Shared"}
						}},
						"library-2":{"kind":"LibraryPanel","spec":{
							"id":0,"title":"Fallback",
							"libraryPanel":{"uid":"shared","name":"Shared"}
						}}
					},
					"layout":{"kind":"GridLayout","spec":{"items":[
						{"kind":"GridLayoutItem","spec":{
							"x":0,"y":2,"width":24,"height":6,
							"element":{"kind":"ElementReference","name":"library-1"}
						}},
						{"kind":"GridLayoutItem","spec":{
							"x":0,"y":8,"width":24,"height":6,
							"element":{"kind":"ElementReference","name":"library-2"}
						}}
					]}}
				}
			}`))
		case "/api/library-elements/shared":
			_, _ = w.Write([]byte(`{"result":{"model":{
				"id":0,"type":"stat","title":"Model title",
				"datasource":{"uid":"prom1","type":"prometheus"},
				"targets":[{"refId":"A","expr":"up","datasource":{"uid":"prom1","type":"prometheus"}}],
				"fieldConfig":{"defaults":{}},"options":{}
			}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 2)
	panel := dashboard.Panels[0]
	assert.Equal(t, 9, panel.ID)
	assert.Equal(t, 10, dashboard.Panels[1].ID)
	assert.Equal(t, "Shared", panel.Title)
	assert.Equal(t, "stat", panel.Type)
	assert.Equal(t, grafana.GridPos{H: 6, W: 24, X: 0, Y: 2}, panel.GridPos)
	require.Len(t, panel.Targets, 1)
	var target map[string]any
	require.NoError(t, json.Unmarshal(panel.Targets[0].Raw(), &target))
	assert.Equal(t, "up", target["expr"])
}

func TestGetDashboardV2LibraryInstancesUseDistinctIDsAndLayoutRepeats(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dashboards/uid/demo":
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
		case "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/demo":
			_, _ = w.Write([]byte(`{
				"spec":{
					"title":"Library instances",
					"elements":{
						"library-a":{"kind":"LibraryPanel","spec":{"id":0,"title":"A","libraryPanel":{"uid":"shared","name":"Shared"}}},
						"library-b":{"kind":"LibraryPanel","spec":{"id":0,"title":"B","libraryPanel":{"uid":"shared","name":"Shared"}}}
					},
					"layout":{"kind":"TabsLayout","spec":{"tabs":[
						{"kind":"TabsLayoutTab","spec":{
							"title":"Shared",
							"layout":{"kind":"GridLayout","spec":{"items":[
								{"kind":"GridLayoutItem","spec":{
									"x":0,"y":0,"width":12,"height":4,
									"element":{"kind":"ElementReference","name":"library-a"}
								}},
								{"kind":"GridLayoutItem","spec":{
									"x":12,"y":0,"width":12,"height":4,
									"repeat":{"value":"site","direction":"h","maxPerRow":1},
									"element":{"kind":"ElementReference","name":"library-b"}
								}}
							]}}
						}}
					]}}
				}
			}`))
		case "/api/library-elements/shared":
			_, _ = w.Write([]byte(`{"result":{"model":{
				"id":42,"type":"stat","title":"Model",
				"repeat":"cluster","repeatDirection":"h","maxPerRow":2
			}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 3)
	assert.Equal(t, 3, dashboard.Panels[0].ID)
	assert.Equal(t, 4, dashboard.Panels[1].ID)
	assert.Equal(t, 5, dashboard.Panels[2].ID)
	assert.Empty(t, dashboard.Panels[1].Repeat)
	assert.Empty(t, dashboard.Panels[1].RepeatDirection)
	assert.Equal(t, 0, dashboard.Panels[1].MaxPerRow)
	assert.Equal(t, "site", dashboard.Panels[2].Repeat)
	assert.Equal(t, "h", dashboard.Panels[2].RepeatDirection)
	assert.Equal(t, 1, dashboard.Panels[2].MaxPerRow)
}

func TestGetDashboardV2NestedMixedIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/dashboards/uid/demo":
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
		case "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/demo":
			_, _ = w.Write([]byte(`{
				"spec":{
					"title":"Mixed",
					"elements":{
						"library-a":{"kind":"LibraryPanel","spec":{
							"id":0,"title":"Library",
							"libraryPanel":{"uid":"shared","name":"Shared"}
						}},
						"panel-b":{"kind":"Panel","spec":{
							"id":0,"title":"Panel B",
							"vizConfig":{"group":"stat","spec":{}},
							"data":{"spec":{"queries":[]}}
						}}
					},
					"layout":{"kind":"TabsLayout","spec":{"tabs":[
						{"kind":"TabsLayoutTab","spec":{
							"title":"Mixed",
							"layout":{"kind":"GridLayout","spec":{"items":[
								{"kind":"GridLayoutItem","spec":{"x":0,"y":0,"width":12,"height":4,"element":{"name":"library-a"}}},
								{"kind":"GridLayoutItem","spec":{"x":12,"y":0,"width":12,"height":4,"element":{"name":"panel-b"}}}
							]}}
						}}
					]}}
				}
			}`))
		case "/api/library-elements/shared":
			_, _ = w.Write([]byte(`{"result":{"model":{"id":42,"type":"stat"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 3)
	assert.Equal(t, 3, dashboard.Panels[0].ID)
	assert.Equal(t, 4, dashboard.Panels[1].ID)
	assert.Equal(t, 5, dashboard.Panels[2].ID)
}

func TestGetDashboardV2RowsLayoutOffsetsRows(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dashboards/uid/demo" {
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{
			"spec":{
				"title":"Rows",
				"elements":{
					"panel-1":{"kind":"Panel","spec":{
						"id":1,"title":"$cluster",
						"vizConfig":{"group":"stat","spec":{}},
						"data":{"spec":{"queries":[]}}
					}},
					"panel-2":{"kind":"Panel","spec":{
						"id":2,"title":"Two",
						"vizConfig":{"group":"stat","spec":{}},
						"data":{"spec":{"queries":[]}}
					}}
				},
				"layout":{"kind":"RowsLayout","spec":{"rows":[
					{"kind":"RowsLayoutRow","spec":{
						"repeat":{"mode":"variable","value":"cluster"},
						"layout":{"kind":"GridLayout","spec":{"items":[
							{"kind":"GridLayoutItem","spec":{
								"x":0,"y":0,"width":12,"height":4,
								"element":{"name":"panel-1"}
							}}
						]}}
					}},
					{"kind":"RowsLayoutRow","spec":{
						"layout":{"kind":"GridLayout","spec":{"items":[
							{"kind":"GridLayoutItem","spec":{
								"x":0,"y":0,"width":12,"height":6,
								"element":{"name":"panel-2"}
							}}
						]}}
					}}
				]}}
			}
		}`))
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 2)
	assert.Equal(t, 0, dashboard.Panels[0].GridPos.Y)
	assert.Equal(t, 5, dashboard.Panels[1].GridPos.Y)

	variables := make([]grafana.TemplateVariable, 1)
	variables[0].Name = "cluster"
	variables[0].Multi = true
	variables[0].IncludeAll = true
	instances := grafana.ExpandPanels(
		dashboard.Panels,
		variables,
		map[string][]string{"cluster": {"a", "b"}},
		map[string]string{"cluster": "$__all"},
	)
	require.Len(t, instances, 3)
	assert.Equal(t, "a", instances[0].Panel.Title)
	assert.Equal(t, "b", instances[1].Panel.Title)
	assert.Equal(t, "Two", instances[2].Panel.Title)
	assert.Equal(t, 0, instances[0].Panel.GridPos.Y)
	assert.Equal(t, 5, instances[1].Panel.GridPos.Y)
	assert.Equal(t, 10, instances[2].Panel.GridPos.Y)
	assert.NotEqual(t, instances[0].Key, instances[1].Key)
}

func TestGetDashboardV2UsesOrgNamespace(t *testing.T) {
	t.Parallel()

	var gotPath, gotOrgID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dashboards/uid/demo" {
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
			return
		}
		gotPath = r.URL.Path
		gotOrgID = r.Header.Get("X-Grafana-Org-Id")
		_, _ = w.Write([]byte(
			`{"spec":{"title":"Org dashboard","elements":{},` +
				`"layout":{"kind":"GridLayout","spec":{"items":[]}}}}`,
		))
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithOrgID("2"), grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	_, err = client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "/apis/dashboard.grafana.app/v2/namespaces/org-2/dashboards/demo", gotPath)
	assert.Equal(t, "2", gotOrgID)
}

func TestGetDashboardV2NestedRowRepeats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		outerRepeat string
		innerRepeat string
		wantY       []int
		wantError   bool
	}{
		{
			name: "inner", innerRepeat: `"repeat":{"mode":"variable","value":"cluster"},`,
			outerRepeat: "", wantY: []int{0, 5}, wantError: false,
		},
		{
			name: "outer", outerRepeat: `"repeat":{"mode":"variable","value":"cluster"},`,
			innerRepeat: "", wantY: []int{0, 6}, wantError: false,
		},
		{
			name: "multiple levels rejected", wantError: true,
			outerRepeat: `"repeat":{"mode":"variable","value":"cluster"},`,
			innerRepeat: `"repeat":{"mode":"variable","value":"cluster"},`,
			wantY:       nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resource := fmt.Sprintf(`{
				"spec":{
					"title":"Nested rows",
					"elements":{%s},
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							%s
							"layout":{"kind":"RowsLayout","spec":{"rows":[
								{"kind":"RowsLayoutRow","spec":{
									%s
									"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
								}}
							]}}
						}}
					]}}
				}
			}`, v2TestElement(1), test.outerRepeat, test.innerRepeat, v2TestItem(1, "GridLayoutItem"))
			server := newV2TestServer(resource)
			defer server.Close()
			client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
			require.NoError(t, err)
			dashboard, err := client.GetDashboard(t.Context(), "demo")
			if test.wantError {
				require.ErrorContains(t, err, "nested dashboard v2 row repeats")
				return
			}
			require.NoError(t, err)
			instances := expandV2ClusterRows(dashboard)
			require.Len(t, instances, len(test.wantY))
			for index, wantY := range test.wantY {
				assert.Equal(t, wantY, instances[index].Panel.GridPos.Y)
			}
		})
	}
}

func TestGetDashboardV2RepeatedRowUsesAbsoluteTabOffset(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Tab rows",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"First",
					"layout":{"kind":"GridLayout","spec":{"items":[]}}
				}},
				{"kind":"TabsLayoutTab","spec":{
					"title":"Second",
					"layout":{"kind":"RowsLayout","spec":{"rows":[
						{"kind":"RowsLayoutRow","spec":{
							"repeat":{"mode":"variable","value":"cluster"},
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
	instances := expandV2ClusterRows(dashboard)
	require.Len(t, instances, 4)
	assert.Equal(t, "First", instances[0].Panel.Title)
	assert.Equal(t, "Second", instances[1].Panel.Title)
	assert.Equal(t, 6, instances[2].Panel.GridPos.Y)
	assert.Equal(t, 11, instances[3].Panel.GridPos.Y)
}

func expandV2ClusterRows(dashboard *grafana.Dashboard) []grafana.PanelInstance {
	variables := make([]grafana.TemplateVariable, 1)
	variables[0].Name = "cluster"
	variables[0].Multi = true
	variables[0].IncludeAll = true

	return grafana.ExpandPanels(
		dashboard.Panels,
		variables,
		map[string][]string{"cluster": {"a", "b"}},
		map[string]string{"cluster": "$__all"},
	)
}

func TestGetDashboardClassicDirectResource(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards/demo" {
			_, _ = w.Write([]byte(`{"spec":{"title":"Classic direct","panels":[]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "Classic direct", dashboard.Title)
}

func TestGetDashboardV2AutoGrid(t *testing.T) {
	t.Parallel()

	elements := make([]string, 0, 4)
	items := make([]string, 0, 4)
	for id := 1; id <= 4; id++ {
		elements = append(elements, v2TestElement(id))
		items = append(items, v2TestItem(id, "AutoGridLayoutItem"))
	}
	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Auto",
			"elements":{%s},
			"layout":{"kind":"AutoGridLayout","spec":{
				"maxColumnCount":2,"rowHeightMode":"short","items":[%s]
			}}
		}
	}`, strings.Join(elements, ","), strings.Join(items, ","))
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 4)
	want := []grafana.GridPos{
		{H: 4, W: 12, X: 0, Y: 0},
		{H: 4, W: 12, X: 12, Y: 0},
		{H: 4, W: 12, X: 0, Y: 4},
		{H: 4, W: 12, X: 12, Y: 4},
	}
	for index, panel := range dashboard.Panels {
		assert.Equal(t, want[index], panel.GridPos)
	}
}

func TestGetDashboardV2TabsAreStacked(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Tabs",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"First",
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}},
				{"kind":"TabsLayoutTab","spec":{
					"title":"Second",
					"layout":{"kind":"TabsLayout","spec":{"tabs":[
						{"kind":"TabsLayoutTab","spec":{
							"title":"Inner",
							"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
						}}
					]}}
				}}
			]}}
		}
	}`,
		strings.Join([]string{v2TestElement(1000000), v2TestElement(2)}, ","),
		v2TestItem(1000000, "GridLayoutItem"),
		v2TestItem(2, "GridLayoutItem"),
	)
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 5)
	assert.Equal(t, "First", dashboard.Panels[0].Title)
	assert.Equal(t, "text", dashboard.Panels[0].Type)
	assert.Equal(t, 1000001, dashboard.Panels[0].ID)
	assert.Equal(t, 0, dashboard.Panels[0].GridPos.Y)
	assert.Equal(t, 2, dashboard.Panels[1].GridPos.Y)
	assert.Equal(t, "Second", dashboard.Panels[2].Title)
	assert.Equal(t, 1000002, dashboard.Panels[2].ID)
	assert.Equal(t, 7, dashboard.Panels[2].GridPos.Y)
	assert.Equal(t, "Inner", dashboard.Panels[3].Title)
	assert.Equal(t, 1000003, dashboard.Panels[3].ID)
	assert.Equal(t, 9, dashboard.Panels[3].GridPos.Y)
	assert.Equal(t, 11, dashboard.Panels[4].GridPos.Y)
}

func TestGetDashboardV2TabsRepeatWithHeadings(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Repeated tabs",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"$cluster",
					"repeat":{"mode":"variable","value":"cluster"},
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}},
				{"kind":"TabsLayoutTab","spec":{
					"title":"Static",
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`,
		strings.Join([]string{v2TestElement(1), v2TestElement(2)}, ","),
		v2TestItem(1, "GridLayoutItem"), v2TestItem(2, "GridLayoutItem"),
	)
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	instances := expandV2ClusterRows(dashboard)
	require.Len(t, instances, 6)
	wantY := []int{0, 2, 7, 9, 14, 16}
	for index, want := range wantY {
		assert.Equal(t, want, instances[index].Panel.GridPos.Y)
	}
	assert.Equal(t, "a", instances[0].Panel.Title)
	assert.Equal(t, "b", instances[2].Panel.Title)
	assert.JSONEq(t, `{"content":"## a"}`, string(instances[0].Panel.Options))
	assert.JSONEq(t, `{"content":"## b"}`, string(instances[2].Panel.Options))
	assert.Equal(t, "Static", instances[4].Panel.Title)
	keys := make(map[string]bool, len(instances))
	for _, instance := range instances {
		assert.False(t, keys[instance.Key], "duplicate key %q", instance.Key)
		keys[instance.Key] = true
	}
}

func TestGetDashboardV2TabIDsAvoidZeroElementIDs(t *testing.T) {
	t.Parallel()

	resource := fmt.Sprintf(`{
		"spec":{
			"title":"Zero IDs",
			"elements":{%s},
			"layout":{"kind":"TabsLayout","spec":{"tabs":[
				{"kind":"TabsLayoutTab","spec":{
					"title":"Only",
					"layout":{"kind":"GridLayout","spec":{"items":[%s]}}
				}}
			]}}
		}
	}`,
		strings.Join([]string{v2TestElement(0), v2TestElement(2)}, ","),
		v2TestItem(0, "GridLayoutItem")+","+v2TestItem(2, "GridLayoutItem"),
	)
	server := newV2TestServer(resource)
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	dashboard, err := client.GetDashboard(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, dashboard.Panels, 3)
	assert.Equal(t, 3, dashboard.Panels[0].ID)
	assert.Equal(t, 4, dashboard.Panels[1].ID)
	assert.Equal(t, 2, dashboard.Panels[2].ID)
}

func v2TestElement(id int) string {
	return fmt.Sprintf(
		`"panel-%d":{"kind":"Panel","spec":{"id":%d,"title":"Panel %d",`+
			`"vizConfig":{"group":"stat","spec":{}},"data":{"spec":{"queries":[]}}}}`,
		id, id, id,
	)
}

func v2TestItem(id int, kind string) string {
	return fmt.Sprintf(
		`{"kind":%q,"spec":{"x":0,"y":0,"width":12,"height":4,`+
			`"element":{"kind":"ElementReference","name":"panel-%d"}}}`,
		kind, id,
	)
}

func newV2TestServer(resource string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dashboards/uid/demo" {
			http.Error(w, "legacy unavailable", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/apis/dashboard.grafana.app/v2/namespaces/default/dashboards/demo" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(resource))
	}))
}
