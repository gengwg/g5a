//nolint:exhaustruct // alert fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryAlertListUnifiedFilters(t *testing.T) {
	t.Parallel()

	requestCh := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCh <- r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":{"groups":[{
				"name":"compute","file":"Operations","rules":[{
					"name":"CPU high","state":"firing","type":"alerting",
					"annotations":{"__dashboardUid__":"dash1"},
					"alerts":[
						{"state":"firing","activeAt":"2026-11-15T10:00:00Z","labels":{"severity":"critical","instance":"a"}},
						{"state":"firing","activeAt":"2026-11-15T10:01:00Z","labels":{"severity":"warning","instance":"b"}},
						{"state":"pending","activeAt":"2026-11-15T10:02:00Z","labels":{"severity":"critical","instance":"c"}}
					]
				},{
					"name":"Memory high","state":"firing","type":"alerting",
					"annotations":{"__dashboardUid__":"dash1"},
					"alerts":[{"state":"firing","labels":{"severity":"critical"}}]
				}]
			},{
				"name":"other","file":"Other","rules":[{
					"name":"CPU other","state":"firing","type":"alerting",
					"annotations":{"__dashboardUid__":"dash1"},
					"alerts":[{"state":"firing","labels":{"severity":"critical"}}]
				}]
			}]}
		}`))
	}))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{
		"datasource":"grafana",
		"dashboardAlerts":true,
		"alertName":"$name",
		"alertInstanceLabelFilter":"{severity=\"$severity\"}",
		"folder":{"uid":"ops","title":"Stale title"},
		"stateFilter":{"firing":true}
	}`)}
	result, err := client.QueryAlertList(
		t.Context(), panel, "dash1",
		[]grafana.TemplateVariable{{Name: "name"}, {Name: "severity"}},
		map[string]string{"name": "cpu high", "severity": "critical"},
	)
	require.NoError(t, err)
	request := <-requestCh
	assert.Equal(t, "/api/prometheus/grafana/api/v1/rules", request.URL.Path)
	assert.Equal(t, "ops", request.URL.Query().Get("folder_uid"))
	assert.Equal(t, "dash1", request.URL.Query().Get("dashboard_uid"))

	require.Len(t, result.Alerts, 1)
	assert.Equal(t, "CPU high", result.Alerts[0].Name)
	assert.Equal(t, "Operations", result.Alerts[0].Folder)
	require.Len(t, result.Alerts[0].Instances, 1)
	assert.Equal(t, "a", result.Alerts[0].Instances[0].Labels["instance"])
	assert.Equal(t,
		time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC),
		result.Alerts[0].ActiveAt,
	)
}

func writeAlertRules(w http.ResponseWriter, rules string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":{"groups":[{"name":"group","file":"Operations","rules":%s}]}}`, rules)
}

func TestQueryAlertListMergesAllSupportedSources(t *testing.T) {
	t.Parallel()

	ruleNames := map[string]string{
		"/api/prometheus/grafana/api/v1/rules": "Grafana rule",
		"/api/prometheus/prom/api/v1/rules":    "Prometheus rule",
		"/api/prometheus/loki/api/v1/rules":    "Loki rule",
		"/api/prometheus/amazon/api/v1/rules":  "Amazon rule",
		"/api/prometheus/azure/api/v1/rules":   "Azure rule",
	}
	var mutex sync.Mutex
	requested := make([]string, 0, len(ruleNames)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		requested = append(requested, r.URL.Path)
		mutex.Unlock()
		if r.URL.Path == "/api/datasources" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"uid":"prom","name":"Prometheus","type":"prometheus"},
				{"uid":"loki","name":"Loki","type":"loki"},
				{"uid":"amazon","name":"Amazon","type":"grafana-amazonprometheus-datasource"},
				{"uid":"azure","name":"Azure","type":"grafana-azureprometheus-datasource"},
				{"uid":"disabled","name":"Disabled","type":"prometheus","jsonData":{"manageAlerts":false}},
				{"uid":"tempo","name":"Tempo","type":"tempo"}
			]`))
			return
		}
		name, ok := ruleNames[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeAlertRules(w, fmt.Sprintf(
			`[{"name":%q,"state":"firing","type":"alerting","alerts":[{"state":"firing","labels":{}}]}]`,
			name,
		))
	}))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	result, err := client.QueryAlertList(t.Context(), grafana.Panel{Options: json.RawMessage(`{}`)}, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, len(ruleNames))
	names := make([]string, 0, len(result.Alerts))
	for _, rule := range result.Alerts {
		names = append(names, rule.Name)
	}
	assert.ElementsMatch(t, []string{
		"Grafana rule", "Prometheus rule", "Loki rule", "Amazon rule", "Azure rule",
	}, names)
	assert.Empty(t, result.Warnings)

	mutex.Lock()
	gotRequests := append([]string(nil), requested...)
	mutex.Unlock()
	assert.ElementsMatch(t, []string{
		"/api/datasources",
		"/api/prometheus/grafana/api/v1/rules",
		"/api/prometheus/prom/api/v1/rules",
		"/api/prometheus/loki/api/v1/rules",
		"/api/prometheus/amazon/api/v1/rules",
		"/api/prometheus/azure/api/v1/rules",
	}, gotRequests)
}

func TestQueryAlertListFetchesSourcesConcurrently(t *testing.T) {
	t.Parallel()

	var active, maxActive atomic.Int32
	ruleNames := map[string]string{
		"/api/prometheus/grafana/api/v1/rules": "Grafana",
		"/api/prometheus/prom-a/api/v1/rules":  "A",
		"/api/prometheus/prom-b/api/v1/rules":  "B",
		"/api/prometheus/prom-c/api/v1/rules":  "C",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/datasources" {
			_, _ = w.Write([]byte(`[
				{"uid":"prom-a","name":"A","type":"prometheus"},
				{"uid":"prom-b","name":"B","type":"prometheus"},
				{"uid":"prom-c","name":"C","type":"prometheus"}
			]`))
			return
		}
		name, ok := ruleNames[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		active.Add(-1)
		writeAlertRules(w, fmt.Sprintf(
			`[{"name":%q,"state":"firing","type":"alerting","alerts":[{"state":"firing","labels":{}}]}]`,
			name,
		))
	}))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	result, err := client.QueryAlertList(t.Context(), grafana.Panel{Options: json.RawMessage(`{}`)}, "", nil, nil)
	require.NoError(t, err)
	assert.Len(t, result.Alerts, len(ruleNames))
	assert.GreaterOrEqual(t, maxActive.Load(), int32(2))
	assert.LessOrEqual(t, maxActive.Load(), int32(4))
}

func TestQueryAlertListExplicitDatasourceOnly(t *testing.T) {
	t.Parallel()

	var mutex sync.Mutex
	requested := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		requested = append(requested, r.URL.Path)
		mutex.Unlock()
		switch r.URL.Path {
		case "/api/datasources":
			_, _ = w.Write([]byte(`[
				{"uid":"prom","name":"Prometheus","type":"prometheus"},
				{"uid":"loki","name":"Loki","type":"loki"}
			]`))
		case "/api/prometheus/prom/api/v1/rules":
			writeAlertRules(w, `[{
				"name":"Prometheus rule","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{}}]
			}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{"datasource":"Prometheus"}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 1)
	assert.Equal(t, "Prometheus rule", result.Alerts[0].Name)

	mutex.Lock()
	gotRequests := append([]string(nil), requested...)
	mutex.Unlock()
	assert.ElementsMatch(t, []string{
		"/api/datasources", "/api/prometheus/prom/api/v1/rules",
	}, gotRequests)
}

func TestQueryAlertListKeepsRulesWhenSourceFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/datasources":
			_, _ = w.Write([]byte(`[
				{"uid":"prom","name":"Prometheus","type":"prometheus"},
				{"uid":"loki","name":"Loki","type":"loki"}
			]`))
		case "/api/prometheus/grafana/api/v1/rules":
			writeAlertRules(w, `[{
				"name":"Grafana rule","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{}}]
			}]`)
		case "/api/prometheus/loki/api/v1/rules":
			writeAlertRules(w, `[{
				"name":"Loki rule","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{}}]
			}]`)
		default:
			http.Error(w, "unavailable", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	result, err := client.QueryAlertList(t.Context(), grafana.Panel{Options: json.RawMessage(`{}`)}, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2)
	assert.Equal(t, "Grafana rule", result.Alerts[0].Name)
	assert.Equal(t, "Loki rule", result.Alerts[1].Name)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "Prometheus")
}

func TestQueryAlertListFolderUIDUsesOnlyGrafana(t *testing.T) {
	t.Parallel()

	requestCh := make(chan *http.Request, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCh <- r
		switch r.URL.Path {
		case "/api/datasources":
			_, _ = w.Write([]byte(`[{"uid":"prom","name":"Prometheus","type":"prometheus"}]`))
		case "/api/prometheus/grafana/api/v1/rules":
			writeAlertRules(w, `[{
				"name":"Grafana rule","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{}}]
			}]`)
		case "/api/prometheus/prom/api/v1/rules":
			writeAlertRules(w, `[{
				"name":"External rule","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{}}]
			}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{
		"folder":{"uid":"ops","title":"Operations"}
	}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 1)
	assert.Equal(t, "Grafana rule", result.Alerts[0].Name)

	request := <-requestCh
	assert.Equal(t, "/api/prometheus/grafana/api/v1/rules", request.URL.Path)
	assert.Equal(t, "ops", request.URL.Query().Get("folder_uid"))
	assert.Empty(t, requestCh)
}

func TestQueryAlertListExplicitExternalFolderUIDReturnsNoRules(t *testing.T) {
	t.Parallel()

	requestCh := make(chan *http.Request, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCh <- r
		switch r.URL.Path {
		case "/api/datasources":
			_, _ = w.Write([]byte(`[{"uid":"prom","name":"Prometheus","type":"prometheus"}]`))
		case "/api/prometheus/prom/api/v1/rules":
			writeAlertRules(w, `[{
				"name":"External rule","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{}}]
			}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{
		"datasource":"Prometheus","folder":{"uid":"ops","title":"Operations"}
	}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Alerts)

	listRequest := <-requestCh
	assert.Equal(t, "/api/datasources", listRequest.URL.Path)
	rulesRequest := <-requestCh
	assert.Equal(t, "/api/prometheus/prom/api/v1/rules", rulesRequest.URL.Path)
	assert.Empty(t, rulesRequest.URL.Query()["folder_uid"])
	assert.Empty(t, requestCh)
}

func TestQueryAlertListLegacyFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/prometheus/grafana/api/v1/rules" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/api/alerts" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"Legacy firing","state":"alerting","dashboardUId":"dash1","newStateDate":"2026-11-15T10:00:00Z"},
			{"name":"Legacy normal","state":"ok","dashboardUId":"dash1","newStateDate":"2026-11-15T10:00:00Z"}
		]`))
	}))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{"datasource":"grafana","stateFilter":{"alerting":true}}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "dash1", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 1)
	assert.Equal(t, "Legacy firing", result.Alerts[0].Name)
	require.Len(t, result.Alerts[0].Instances, 1)
}

func TestQueryAlertListRejectsInvalidLabelFilter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"groups":[]}}`))
	}))
	t.Cleanup(server.Close)
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	for _, filter := range []string{"{severity}", "{cluster=}", "{cluster=~}"} {
		t.Run(filter, func(t *testing.T) {
			t.Parallel()

			options, err := json.Marshal(map[string]string{
				"datasource": "grafana", "alertInstanceLabelFilter": filter,
			})
			require.NoError(t, err)
			panel := grafana.Panel{Options: options}
			_, err = client.QueryAlertList(t.Context(), panel, "", nil, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid alert instance label filter")
		})
	}
}

func TestQueryAlertListAcceptsEmptyLabelMatcherValues(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAlertRules(w, `[{
			"name":"Cluster rule","state":"firing","type":"alerting",
			"alerts":[
				{"state":"firing","labels":{"cluster":"","instance":"empty"}},
				{"state":"firing","labels":{"cluster":"prod","instance":"prod"}}
			]
		}]`)
	}))
	t.Cleanup(server.Close)
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	for _, filter := range []string{`{cluster=""}`, `{cluster=~""}`} {
		t.Run(filter, func(t *testing.T) {
			t.Parallel()

			options, err := json.Marshal(map[string]string{
				"datasource": "grafana", "alertInstanceLabelFilter": filter,
			})
			require.NoError(t, err)
			panel := grafana.Panel{Options: options}
			result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
			require.NoError(t, err)
			require.Len(t, result.Alerts, 1)
			require.Len(t, result.Alerts[0].Instances, 1)
			assert.Equal(t, "empty", result.Alerts[0].Instances[0].Labels["instance"])
		})
	}
}

func TestQueryAlertListAppliesRuleStateBeforeInstanceState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAlertRules(w, `[
			{
				"name":"Firing with normal instance","state":"firing","type":"alerting",
				"alerts":[{"state":"normal","labels":{}}]
			},
			{
				"name":"Normal","state":"inactive","type":"alerting",
				"alerts":[{"state":"normal","labels":{}}]
			},
			{
				"name":"Alerting with reason","state":"Alerting (NoData)","type":"alerting",
				"alerts":[{"state":"Alerting (NoData)","labels":{}}]
			},
			{
				"name":"Normal with reason","state":"Normal (NoData)","type":"alerting",
				"alerts":[{"state":"Normal (NoData)","labels":{}}]
			},
			{
				"name":"Pending","state":"pending","type":"alerting",
				"alerts":[{"state":"pending","labels":{}}]
			},
			{
				"name":"Recovering","state":"recovering","type":"alerting",
				"alerts":[{"state":"recovering","labels":{}}]
			},
			{
				"name":"Error","state":"Error","type":"alerting",
				"alerts":[{"state":"Error","labels":{}}]
			},
			{
				"name":"No data","state":"NoData","type":"alerting",
				"alerts":[{"state":"NoData","labels":{}}]
			}
		]`)
	}))
	t.Cleanup(server.Close)
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	tests := []struct {
		filter string
		want   []string
	}{
		{filter: "normal", want: []string{"Normal", "Normal with reason"}},
		{filter: "firing", want: []string{"Alerting with reason"}},
		{filter: "pending", want: []string{"Pending"}},
		{filter: "recovering", want: []string{"Recovering"}},
		{filter: "error", want: []string{"Error"}},
		{filter: "noData", want: []string{"No data"}},
	}
	for _, test := range tests {
		t.Run(test.filter, func(t *testing.T) {
			t.Parallel()

			options, err := json.Marshal(map[string]any{
				"datasource": "grafana", "stateFilter": map[string]bool{test.filter: true},
			})
			require.NoError(t, err)
			result, err := client.QueryAlertList(
				t.Context(), grafana.Panel{Options: options}, "", nil, nil,
			)
			require.NoError(t, err)
			names := make([]string, 0, len(result.Alerts))
			for _, rule := range result.Alerts {
				names = append(names, rule.Name)
			}
			assert.ElementsMatch(t, test.want, names)
		})
	}
}

func TestQueryAlertListCustomGroupingStatRetainsRules(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAlertRules(w, `[
			{
				"name":"Rule one","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","labels":{"cluster":"prod","instance":"a"}}]
			},
			{
				"name":"Rule two","state":"pending","type":"alerting",
				"alerts":[{"state":"pending","labels":{"cluster":"prod","instance":"b"}}]
			}
		]`)
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{
		"datasource":"grafana","viewMode":"stat",
		"groupMode":"custom","groupBy":["cluster"]
	}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2)
	assert.Equal(t, "Rule one", result.Alerts[0].Name)
	assert.Equal(t, "firing", result.Alerts[0].State)
	assert.Equal(t, "Rule two", result.Alerts[1].Name)
	assert.Equal(t, "pending", result.Alerts[1].State)
	assert.Empty(t, result.Warnings)
}

func TestQueryAlertListCustomGroupingMissingLabels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAlertRules(w, `[
			{
				"name":"Partial labels","state":"firing","type":"alerting",
				"alerts":[
					{
						"state":"firing",
						"labels":{"cluster":"prod","severity":"critical","instance":"a"}
					},
					{"state":"firing","labels":{"cluster":"prod","instance":"b"}}
				]
			},
			{
				"name":"Missing labels","state":"firing","type":"alerting",
				"alerts":[
					{"state":"firing","labels":{"instance":"c"}},
					{"state":"firing","labels":{"severity":"critical","instance":"d"}}
				]
			}
		]`)
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{
		"datasource":"grafana","viewMode":"list",
		"groupMode":"custom","groupBy":["cluster","severity"]
	}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 3)

	assert.Equal(t, "cluster=prod&severity=", result.Alerts[0].Name)
	require.Len(t, result.Alerts[0].Instances, 1)
	assert.Equal(t, "b", result.Alerts[0].Instances[0].Labels["instance"])
	assert.Equal(t, "cluster=prod&severity=critical", result.Alerts[1].Name)
	require.Len(t, result.Alerts[1].Instances, 1)
	assert.Equal(t, "a", result.Alerts[1].Instances[0].Labels["instance"])
	assert.Equal(t, "No grouping", result.Alerts[2].Name)
	require.Len(t, result.Alerts[2].Instances, 2)
	assert.Equal(t, "c", result.Alerts[2].Instances[0].Labels["instance"])
	assert.Equal(t, "d", result.Alerts[2].Instances[1].Labels["instance"])

	repeated, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, result.Alerts, repeated.Alerts)
}

func TestQueryAlertListCustomGrouping(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAlertRules(w, `[
			{
				"name":"Staging normal","state":"inactive","type":"alerting",
				"alerts":[{
					"state":"normal","labels":{"cluster":"staging","severity":"warning","instance":"c"}
				}]
			},
			{
				"name":"Prod pending","state":"pending","type":"alerting",
				"alerts":[{
					"state":"pending","labels":{"cluster":"prod","severity":"critical","instance":"a"}
				}]
			},
			{
				"name":"Prod firing","state":"firing","type":"alerting",
				"alerts":[{
					"state":"firing","labels":{"cluster":"prod","severity":"critical","instance":"b"}
				}]
			},
			{
				"name":"Staging recovering","state":"recovering","type":"alerting",
				"alerts":[{
					"state":"recovering","labels":{"cluster":"staging","severity":"warning","instance":"d"}
				}]
			}
		]`)
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{
		"datasource":"grafana","groupMode":"custom","groupBy":["cluster","severity"]
	}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2)
	assert.Empty(t, result.Warnings)

	assert.Equal(t, "cluster=prod&severity=critical", result.Alerts[0].Name)
	assert.Equal(t, "firing", result.Alerts[0].State)
	assert.Empty(t, result.Alerts[0].Folder)
	require.Len(t, result.Alerts[0].Instances, 2)
	assert.Equal(t, "a", result.Alerts[0].Instances[0].Labels["instance"])
	assert.Equal(t, "b", result.Alerts[0].Instances[1].Labels["instance"])

	assert.Equal(t, "cluster=staging&severity=warning", result.Alerts[1].Name)
	assert.Equal(t, "recovering", result.Alerts[1].State)
	require.Len(t, result.Alerts[1].Instances, 2)
	assert.Equal(t, "c", result.Alerts[1].Instances[0].Labels["instance"])
	assert.Equal(t, "d", result.Alerts[1].Instances[1].Labels["instance"])

	repeated, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, result.Alerts, repeated.Alerts)

	ungroupedPanel := grafana.Panel{Options: json.RawMessage(`{
		"datasource":"grafana","groupMode":"custom","groupBy":[]
	}`)}
	ungrouped, err := client.QueryAlertList(t.Context(), ungroupedPanel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, ungrouped.Alerts, 1)
	assert.Equal(t, "No grouping", ungrouped.Alerts[0].Name)
	assert.Equal(t, "firing", ungrouped.Alerts[0].State)
	assert.Len(t, ungrouped.Alerts[0].Instances, 4)
	assert.Empty(t, ungrouped.Warnings)
}

func TestQueryAlertListTimeSortIgnoresNormalInstances(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAlertRules(w, `[
			{
				"name":"Uses firing timestamp","state":"firing","type":"alerting",
				"alerts":[
					{"state":"normal","activeAt":"2026-11-15T09:00:00Z","labels":{}},
					{"state":"firing","activeAt":"2026-11-15T11:00:00Z","labels":{}}
				]
			},
			{
				"name":"Middle timestamp","state":"firing","type":"alerting",
				"alerts":[{"state":"firing","activeAt":"2026-11-15T10:00:00Z","labels":{}}]
			}
		]`)
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	panel := grafana.Panel{Options: json.RawMessage(`{"datasource":"grafana","sortOrder":4}`)}
	result, err := client.QueryAlertList(t.Context(), panel, "", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Alerts, 2)
	assert.Equal(t, "Middle timestamp", result.Alerts[0].Name)
	assert.Equal(t, "Uses firing timestamp", result.Alerts[1].Name)
	assert.Equal(t,
		time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC),
		result.Alerts[1].ActiveAt,
	)
}
