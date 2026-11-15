package grafana_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
				"timeSettings":{"from":"now-3h","to":"now","autoRefresh":"30s"},
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
						"id":1,"title":"One",
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

func TestGetDashboardV2RejectsAutoGrid(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"spec":{"title":"Auto","layout":{"kind":"AutoGridLayout","spec":{"items":[]}}}}`))
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	_, err = client.GetDashboard(t.Context(), "demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported dashboard v2 layout")
}
