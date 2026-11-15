//nolint:cyclop,exhaustruct // API fixtures intentionally vary relevant fields only
package grafana_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lovromazgon/grafana-tui/grafana"
)

func TestQueryPanel_GenericEndpoint(t *testing.T) {
	t.Parallel()

	var request struct {
		From    string            `json:"from"`
		To      string            `json:"to"`
		Queries []json.RawMessage `json:"queries"`
	}
	server := newDSQueryServer(t, &request)
	defer server.Close()

	client, err := grafana.NewClient(
		server.URL,
		grafana.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	panel := grafana.Panel{
		ID:         1,
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Targets: []grafana.Target{
			mustUnmarshalTarget(t,
				`{"refId":"A","expr":"up","instant":true,"range":false,"format":"table",`+
					`"datasource":{"uid":"prom1","type":"prometheus"}}`),
		},
	}
	result, err := client.QueryPanel(
		t.Context(),
		panel,
		grafana.TimeRange{From: "1700000000000", To: "1700003600000"},
		100,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Results["A"]; !ok {
		t.Fatal("expected result for refId A")
	}
	if request.From != "1700000000000" || request.To != "1700003600000" {
		t.Errorf("time range = %s..%s", request.From, request.To)
	}
	if len(request.Queries) != 1 {
		t.Fatalf("got %d queries, want 1", len(request.Queries))
	}

	var target map[string]any
	if err := json.Unmarshal(request.Queries[0], &target); err != nil {
		t.Fatal(err)
	}
	if target["expr"] != "up" || target["format"] != "table" {
		t.Errorf("target fields were not preserved: %v", target)
	}
	if target["instant"] != true || target["range"] != false {
		t.Errorf("instant/range semantics were not preserved: %v", target)
	}
	if target["maxDataPoints"] != float64(100) {
		t.Errorf("maxDataPoints = %v, want 100", target["maxDataPoints"])
	}
}

func TestQueryPanel_VariableResolution(t *testing.T) {
	t.Parallel()

	var request struct {
		Queries []json.RawMessage `json:"queries"`
	}
	server := newDSQueryServer(t, &request)
	defer server.Close()

	client, err := grafana.NewClient(
		server.URL,
		grafana.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	panel := grafana.Panel{
		ID:         1,
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Targets: []grafana.Target{
			mustUnmarshalTarget(t,
				`{"refId":"A","expr":"up{instance=\"$instance\"}",`+
					`"datasource":{"uid":"prom1","type":"prometheus"}}`),
		},
	}
	variables := []grafana.TemplateVariable{{
		Name: "instance",
		Type: "query",
		Current: grafana.TemplateVariableCurrent{
			Text:  json.RawMessage(`"localhost"`),
			Value: json.RawMessage(`"localhost:9090"`),
		},
	}}

	_, err = client.QueryPanel(
		t.Context(),
		panel,
		grafana.TimeRange{From: "now-1h", To: "now"},
		100,
		variables,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var target map[string]any
	if err := json.Unmarshal(request.Queries[0], &target); err != nil {
		t.Fatal(err)
	}
	want := `up{instance="localhost:9090"}`
	if target["expr"] != want {
		t.Errorf("query = %q, want %q", target["expr"], want)
	}
}

func TestQueryPanel_HiddenTarget(t *testing.T) {
	t.Parallel()

	var request struct {
		Queries []json.RawMessage `json:"queries"`
	}
	server := newDSQueryServer(t, &request)
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	panel := grafana.Panel{
		Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
		Targets: []grafana.Target{
			mustUnmarshalTarget(t, `{"refId":"A","expr":"up","hide":true}`),
			mustUnmarshalTarget(t, `{"refId":"B","expr":"up"}`),
		},
	}
	_, err = client.QueryPanel(
		t.Context(), panel, grafana.TimeRange{From: "now-1h", To: "now"},
		100, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Queries) != 1 {
		t.Fatalf("got %d queries, want only the visible target", len(request.Queries))
	}
}

func TestQueryPanel_EmptyTargets(t *testing.T) {
	t.Parallel()

	client, err := grafana.NewClient("http://unused")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := client.QueryPanel(
		t.Context(),
		grafana.Panel{ID: 1, Type: "timeseries"},
		grafana.TimeRange{From: "now-1h", To: "now"},
		100,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected empty results, got %d", len(result.Results))
	}
}

func TestQueryPanel_UnresolvedDatasource(t *testing.T) {
	t.Parallel()

	client, err := grafana.NewClient("http://unused")
	if err != nil {
		t.Fatal(err)
	}
	panel := grafana.Panel{
		Datasource: &grafana.DatasourceRef{UID: "${DS_INCIDENT}", Type: "grafana-incident-datasource"},
		Targets: []grafana.Target{
			mustUnmarshalTarget(t, `{"refId":"A","queryType":"incidents"}`),
		},
	}
	_, err = client.QueryPanel(
		t.Context(), panel, grafana.TimeRange{From: "now-1h", To: "now"},
		100, nil, nil, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "unresolved datasource") {
		t.Fatalf("expected unresolved datasource error, got %v", err)
	}
}

func TestQueryPanel_InvalidTimeRange(t *testing.T) {
	t.Parallel()

	client, err := grafana.NewClient("http://unused")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.QueryPanel(
		t.Context(),
		grafana.Panel{ID: 1, Type: "timeseries"},
		grafana.TimeRange{From: "yesterday", To: "now"},
		100,
		nil,
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported time range") {
		t.Fatalf("expected unsupported time range error, got %v", err)
	}
}

func newDSQueryServer(
	t *testing.T,
	request any,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
			case r.Method == http.MethodPost && r.URL.Path == "/api/ds/query":
				if err := json.NewDecoder(r.Body).Decode(request); err != nil {
					t.Errorf("decoding request: %v", err)
				}
				_, _ = w.Write([]byte(`{
					"results":{"A":{"frames":[{
						"schema":{"refId":"A","fields":[
							{"name":"time","type":"time"},
							{"name":"up","type":"number"}
						]},
						"data":{"values":[[1700000000000],[1]]}
					}]}}
				}`))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
}

func mustUnmarshalTarget(t *testing.T, data string) grafana.Target {
	t.Helper()
	var target grafana.Target
	if err := json.Unmarshal([]byte(data), &target); err != nil {
		t.Fatalf("unmarshaling target: %v", err)
	}

	return target
}
