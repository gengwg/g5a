//nolint:cyclop,exhaustruct // API fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVariableOptions_QueryResult(t *testing.T) {
	t.Parallel()

	var queryTarget map[string]any
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[
					{"uid":"prom-main","name":"Main Prometheus","type":"prometheus"},
					{"uid":"loki-main","name":"Main Loki","type":"loki"}
				]`))
			case "/api/ds/query":
				var request struct {
					Queries []json.RawMessage `json:"queries"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decoding query: %v", err)
				}
				if len(request.Queries) != 1 {
					t.Fatalf("got %d variable queries, want 1", len(request.Queries))
				}
				if err := json.Unmarshal(request.Queries[0], &queryTarget); err != nil {
					t.Errorf("decoding target: %v", err)
				}
				_, _ = w.Write([]byte(`{"results":{"vars":{"frames":[
					{"schema":{"refId":"vars","fields":[
						{"name":"Value","type":"number","labels":{"cluster":"beta"}}
					]},"data":{"values":[[1]]}},
					{"schema":{"refId":"vars","fields":[
						{"name":"Value","type":"number","labels":{"cluster":"alpha"}}
					]},"data":{"values":[[1]]}},
					{"schema":{"refId":"vars","fields":[
						{"name":"Value","type":"number","labels":{"cluster":"alpha"}}
					]},"data":{"values":[[1]]}}
				]}}}`))
			default:
				t.Errorf("unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	variables := []grafana.TemplateVariable{
		{
			Name: "datasource", Type: "datasource", Query: json.RawMessage(`"prometheus"`),
			Regex:   "/Main Prometheus/",
			Current: grafana.TemplateVariableCurrent{Value: json.RawMessage(`"prom-main"`)},
		},
		{
			Name: "cluster", Type: "query", Multi: true, IncludeAll: true, Sort: 1,
			Datasource: &grafana.DatasourceRef{UID: "${datasource}", Type: "prometheus"},
			Query:      json.RawMessage(`{"query":"query_result(count by (cluster) (kube_node_role))","refId":"vars"}`),
			Regex:      `/cluster="([^"]+)"/`,
			Current:    grafana.TemplateVariableCurrent{Value: json.RawMessage(`"$__all"`)},
		},
	}

	options, defaults, err := client.ResolveVariableOptionsWithTimeRange(
		t.Context(),
		variables,
		grafana.TimeRange{From: "now-1h", To: "now"},
	)
	if err != nil {
		t.Fatalf("resolving variables: %v", err)
	}
	if got := options["datasource"]; len(got) != 1 || got[0] != "prom-main" {
		t.Errorf("datasource options = %v, want [prom-main]", got)
	}
	if got := options["cluster"]; len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("cluster options = %v, want [alpha beta]", got)
	}
	if defaults["cluster"] != "$__all" {
		t.Errorf("cluster default = %q, want $__all", defaults["cluster"])
	}
	if defaults["datasource"] != "prom-main" {
		t.Errorf("datasource default = %q, want prom-main", defaults["datasource"])
	}
	datasource, ok := queryTarget["datasource"].(map[string]any)
	if !ok || datasource["uid"] != "prom-main" {
		t.Errorf("query datasource = %v, want prom-main", queryTarget["datasource"])
	}
	if queryTarget["expr"] != "count by (cluster) (kube_node_role)" {
		t.Errorf("query_result was not unwrapped: %v", queryTarget["expr"])
	}
}

func TestResolveVariableOptions_UsesCurrentSelections(t *testing.T) {
	t.Parallel()

	var queryTarget map[string]any
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
			case "/api/ds/query":
				var request struct {
					Queries []json.RawMessage `json:"queries"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(request.Queries[0], &queryTarget); err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte(`{"results":{"vars":{"frames":[]}}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	variables := []grafana.TemplateVariable{
		{
			Name: "env", Type: "custom", Query: json.RawMessage(`"prod,dev"`),
			Current: grafana.TemplateVariableCurrent{Value: json.RawMessage(`"prod"`)},
		},
		{
			Name: "instance", Type: "query",
			Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
			Query:      json.RawMessage(`"query_result(up{env=\"$env\"})"`),
		},
	}

	_, _, err = client.ResolveVariableOptionsWithSelections(
		t.Context(),
		variables,
		grafana.TimeRange{From: "now-1h", To: "now"},
		map[string]string{"env": "dev"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if queryTarget["expr"] != `up{env="dev"}` {
		t.Errorf("dependent query = %v, want current selection dev", queryTarget["expr"])
	}
}

func TestResolveVariableOptions_LabelValuesQueries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		query  string
		metric string
	}{
		{
			name:  "label only",
			query: "label_values(instance)",
		},
		{
			name:   "metric and label",
			query:  "label_values(up, instance)",
			metric: "up",
		},
		{
			name:   "metric with label filters",
			query:  `label_values(up{job="api",env="prod"}, instance)`,
			metric: `up{job="api",env="prod"}`,
		},
		{
			name:   "comma inside quoted label value",
			query:  `label_values(up{job="api,worker",env="prod"}, instance)`,
			metric: `up{job="api,worker",env="prod"}`,
		},
		{
			name:   "escaped quote and comma inside label value",
			query:  `label_values(up{job="api\"worker,secondary",env="prod"}, instance)`,
			metric: `up{job="api\"worker,secondary",env="prod"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			const labelValuesPath = "/api/datasources/proxy/uid/prom1/api/v1/label/instance/values"
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/datasources":
						_, _ = w.Write([]byte(`[
							{"uid":"prom1","name":"Prometheus","type":"prometheus"}
						]`))
					case labelValuesPath:
						if got := r.URL.Query().Get("match[]"); got != test.metric {
							t.Errorf("match[] = %q, want %q", got, test.metric)
						}
						_, _ = w.Write([]byte(`{"status":"success","data":["host1","host2"]}`))
					default:
						t.Errorf("unexpected request: %s", r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
					}
				},
			))
			defer server.Close()

			client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			encodedQuery, err := json.Marshal(test.query)
			if err != nil {
				t.Fatal(err)
			}
			variables := []grafana.TemplateVariable{{
				Name: "instance", Type: "query",
				Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
				Query:      json.RawMessage(encodedQuery),
			}}

			options, _, err := client.ResolveVariableOptionsWithTimeRange(
				t.Context(),
				variables,
				grafana.TimeRange{From: "now-1h", To: "now"},
			)
			if err != nil {
				t.Fatalf("resolving label values: %v", err)
			}
			got := options["instance"]
			if len(got) != 2 || got[0] != "host1" || got[1] != "host2" {
				t.Errorf("options = %v, want [host1 host2]", got)
			}
		})
	}
}

func TestResolveVariableOptionsRespectRefreshMode(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/v1/label/") {
			requests++
		}
		_, _ = w.Write([]byte(`{"status":"success","data":["dev"]}`))
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	newVariable := func(refresh int) grafana.TemplateVariable {
		return grafana.TemplateVariable{
			Name: "cluster", Type: "query", Refresh: refresh,
			Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
			Current:    grafana.TemplateVariableCurrent{Value: json.RawMessage(`"prod"`)},
			Options:    []grafana.TemplateVariableOption{{Text: "prod", Value: "prod"}, {Text: "dev", Value: "dev"}},
			Definition: "label_values(up, cluster)",
		}
	}

	options, defaults, err := client.ResolveVariableOptionsWithSelectionsAndInitialLoad(
		t.Context(), []grafana.TemplateVariable{newVariable(0)}, grafana.TimeRange{From: "now-1h", To: "now"}, nil, true,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"prod", "dev"}, options["cluster"])
	assert.Equal(t, "prod", defaults["cluster"])
	assert.Equal(t, 0, requests)

	_, _, err = client.ResolveVariableOptionsWithSelectionsAndInitialLoad(
		t.Context(), []grafana.TemplateVariable{newVariable(1)}, grafana.TimeRange{From: "now-1h", To: "now"}, nil, true,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, requests)
	_, _, err = client.ResolveVariableOptionsWithSelectionsAndInitialLoad(
		t.Context(), []grafana.TemplateVariable{newVariable(1)}, grafana.TimeRange{From: "now-1h", To: "now"}, nil, false,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, requests)

	_, _, err = client.ResolveVariableOptionsWithSelectionsAndInitialLoad(
		t.Context(), []grafana.TemplateVariable{newVariable(2)}, grafana.TimeRange{From: "now-1h", To: "now"}, nil, true,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	_, _, err = client.ResolveVariableOptionsWithSelectionsAndInitialLoad(
		t.Context(), []grafana.TemplateVariable{newVariable(2)}, grafana.TimeRange{From: "now-1h", To: "now"}, nil, false,
	)
	require.NoError(t, err)
	assert.Equal(t, 3, requests)
}

func TestQueryLabelValuesIncludesTimeRange(t *testing.T) {
	t.Parallel()

	var start, end string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start = r.URL.Query().Get("start")
		end = r.URL.Query().Get("end")
		_, _ = w.Write([]byte(`{"status":"success","data":["alpha"]}`))
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	variable := grafana.TemplateVariable{
		Name: "cluster", Type: "query", Refresh: 1,
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Definition: "label_values(up, cluster)",
		Current:    grafana.TemplateVariableCurrent{Value: json.RawMessage(`"alpha"`)},
	}
	_, _, err = client.ResolveVariableOptionsWithTimeRange(
		t.Context(), []grafana.TemplateVariable{variable}, grafana.TimeRange{From: "now-1h", To: "now"},
	)
	require.NoError(t, err)
	assert.NotEmpty(t, start)
	assert.NotEmpty(t, end)
	assert.NotEqual(t, start, end)
}

func TestQueryVariableFailureIsReturned(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	variable := grafana.TemplateVariable{
		Name: "cluster", Type: "query", Refresh: 1,
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Definition: "label_values(up, cluster)",
	}
	_, _, err = client.ResolveVariableOptionsWithTimeRange(
		t.Context(), []grafana.TemplateVariable{variable}, grafana.TimeRange{From: "now-1h", To: "now"},
	)
	assert.Error(t, err)
}

func TestResolveVariableOptions_RoundedTimeRange(t *testing.T) {
	t.Parallel()

	var request struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
			case "/api/ds/query":
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				_, _ = w.Write([]byte(`{"results":{"vars":{"frames":[]}}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	variables := []grafana.TemplateVariable{{
		Name: "instance", Type: "query",
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Query:      json.RawMessage(`"query_result(up)"`),
	}}
	_, _, err = client.ResolveVariableOptionsWithTimeRange(
		t.Context(),
		variables,
		grafana.TimeRange{From: "now/d", To: "now/d"},
	)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	fromMs, err := strconv.ParseInt(request.From, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	toMs, err := strconv.ParseInt(request.To, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if fromMs != dayStart.UnixMilli() {
		t.Errorf("from = %d, want day start %d", fromMs, dayStart.UnixMilli())
	}
	wantTo := dayStart.AddDate(0, 0, 1).Add(-time.Millisecond).UnixMilli()
	if toMs != wantTo {
		t.Errorf("to = %d, want day end %d", toMs, wantTo)
	}
}

func TestResolveVariableOptions_InvalidTimeRange(t *testing.T) {
	t.Parallel()

	client, err := grafana.NewClient("http://unused")
	if err != nil {
		t.Fatal(err)
	}
	variables := []grafana.TemplateVariable{{
		Name: "instance", Type: "query",
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Query:      json.RawMessage(`"query_result(up)"`),
	}}
	_, _, err = client.ResolveVariableOptionsWithTimeRange(
		t.Context(),
		variables,
		grafana.TimeRange{From: "yesterday", To: "now"},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported time range") {
		t.Fatalf("expected unsupported time range error, got %v", err)
	}
}

func TestResolveVariableOptions_DatasourceQueryType(t *testing.T) {
	t.Parallel()

	var target map[string]any
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"incident1","name":"Incident","type":"grafana-incident-datasource"}]`))
			case "/api/ds/query":
				var request struct {
					Queries []json.RawMessage `json:"queries"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if err := json.Unmarshal(request.Queries[0], &target); err != nil {
					t.Error(err)
				}
				_, _ = w.Write([]byte(`{"results":{"vars":{"frames":[{
					"schema":{"refId":"vars","fields":[{"name":"severity","type":"string"}]},
					"data":{"values":[["critical","high"]]}
				}]}}}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	variables := []grafana.TemplateVariable{{
		Name: "severity", Type: "query",
		Datasource: &grafana.DatasourceRef{UID: "incident1", Type: "grafana-incident-datasource"},
		Query:      json.RawMessage(`{"queryType":"severities"}`),
		Definition: "Grafana Incident - severities",
	}}
	options, _, err := client.ResolveVariableOptionsWithTimeRange(
		t.Context(),
		variables,
		grafana.TimeRange{From: "now-1h", To: "now"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if target["queryType"] != "severities" {
		t.Errorf("queryType = %v, want severities", target["queryType"])
	}
	if _, exists := target["expr"]; exists {
		t.Errorf("query type should not fall back to PromQL: %v", target["expr"])
	}
	got := options["severity"]
	if len(got) != 2 || got[0] != "critical" || got[1] != "high" {
		t.Errorf("options = %v, want [critical high]", got)
	}
}
