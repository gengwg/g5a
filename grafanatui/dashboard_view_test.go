//nolint:exhaustruct,testpackage // internal view state and fixtures need direct access
package grafanatui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gengwg/g5a/grafana"
)

func TestAppHeaderUsesG5AName(t *testing.T) {
	t.Parallel()

	client, err := grafana.NewClient("https://grafana.example.com")
	if err != nil {
		t.Fatal(err)
	}
	header := NewApp(client, Options{}).renderHeader()
	if !strings.Contains(header, " g5a") {
		t.Errorf("header does not contain g5a: %q", header)
	}
	if strings.Contains(header, "grafana-tui") {
		t.Errorf("header retains the upstream application name: %q", header)
	}
	if !strings.Contains(header, "https://grafana.example.com") {
		t.Errorf("header does not contain the Grafana URL: %q", header)
	}
}

func TestDashboardViewRendersRepeatedPanels(t *testing.T) {
	t.Parallel()

	fieldConfig := json.RawMessage(`{
		"defaults":{"mappings":[{"type":"value","options":{
			"0":{"text":"OK","color":"green"},
			"2":{"text":"Degraded","color":"orange"}
		}}]}
	}`)
	options := json.RawMessage(
		`{"colorMode":"background","textMode":"value_and_name",` +
			`"reduceOptions":{"calcs":["lastNotNull"]}}`,
	)
	variable := grafana.TemplateVariable{
		Name: "cluster", Multi: true, IncludeAll: true,
		Current: grafana.TemplateVariableCurrent{Value: json.RawMessage(`"$__all"`)},
	}
	dashboard := &grafana.Dashboard{
		Title:      "Fleet fixture",
		Templating: grafana.Templating{List: []grafana.TemplateVariable{variable}},
		Panels: []grafana.Panel{{
			ID: 7, Type: "stat", Title: "$cluster", Repeat: "cluster", RepeatDirection: "h", MaxPerRow: 2,
			GridPos:     grafana.GridPos{X: 0, Y: 0, W: 24, H: 4},
			FieldConfig: fieldConfig, Options: options,
		}},
	}
	view := newDashboardView(nil, "fixture")
	view.dashboard = dashboard
	view.variableValues = map[string]string{"cluster": "$__all"}
	view.variableOptions = map[string][]string{"cluster": {"alpha", "beta"}}
	view.expandPanels()

	for _, instance := range view.instances {
		view.results[instance.Key] = numericResult(2)
	}
	output := view.View(100, 20)
	for _, expected := range []string{"alpha", "beta", "Degraded"} {
		if !strings.Contains(output, expected) {
			t.Errorf("dashboard output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestDashboardViewIsolatesPanelErrors(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Fixture"}
	view.instances = []grafana.PanelInstance{
		{
			Key: "1",
			Panel: grafana.Panel{
				ID: 1, Type: "stat", Title: "Healthy", GridPos: grafana.GridPos{W: 12, H: 4},
			},
			RepeatLength: 1,
		},
		{
			Key: "2",
			Panel: grafana.Panel{
				ID: 2, Type: "stat", Title: "Broken", GridPos: grafana.GridPos{X: 12, W: 12, H: 4},
			},
			RepeatLength: 1,
		},
	}
	view.results["1"] = numericResult(42)
	view.errors["2"] = testError{}

	output := view.View(100, 8)
	if !strings.Contains(output, "42") {
		t.Errorf("healthy panel should still render:\n%s", output)
	}
	if !strings.Contains(output, "test query failed") {
		t.Errorf("failed panel should contain its own error:\n%s", output)
	}
}

func TestDashboardViewRejectsStaleResult(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	view.generation = 2
	view.handlePanelData(dashboardPanelDataMsg{
		key: "panel", generation: 1, result: numericResult(1),
	})
	if _, exists := view.results["panel"]; exists {
		t.Fatal("stale result should not replace current state")
	}
}

func TestDashboardViewDeadlineExceededMarksStaleData(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Fixture"}
	view.instances = []grafana.PanelInstance{{
		Key: "panel",
		Panel: grafana.Panel{
			ID: 1, Type: "stat", Title: "Panel", GridPos: grafana.GridPos{W: 24, H: 4},
		},
		RepeatLength: 1,
	}}
	view.results["panel"] = numericResult(42)
	view.generation = 1

	view.handlePanelError(dashboardPanelErrorMsg{
		key: "panel", generation: 1, err: context.DeadlineExceeded,
	})

	if !errors.Is(view.errors["panel"], context.DeadlineExceeded) {
		t.Fatalf("panel error = %v, want context deadline exceeded", view.errors["panel"])
	}
	output := view.View(80, 4)
	if !strings.Contains(output, "! Panel") {
		t.Errorf("stale data lacks an error indicator:\n%s", output)
	}
	if strings.Contains(output, "No data") {
		t.Errorf("deadline was incorrectly rendered as no data:\n%s", output)
	}
}

//nolint:cyclop // end-to-end assertions cover loading, variables, queries, and rendering
func TestDashboardRefreshConcurrency(t *testing.T) {
	t.Parallel()

	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
			case "/api/ds/query":
				current := active.Add(1)
				for {
					previous := maximum.Load()
					if current <= previous || maximum.CompareAndSwap(previous, current) {
						break
					}
				}
				time.Sleep(25 * time.Millisecond)
				active.Add(-1)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{
					"schema":{"refId":"A","fields":[{"name":"Value","type":"number"}]},
					"data":{"values":[[1]]}
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
	view := newDashboardView(client, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Concurrency"}
	view.instances = make([]grafana.PanelInstance, 8)
	for index := range view.instances {
		view.instances[index] = grafana.PanelInstance{
			Key: strconv.Itoa(index + 1),
			Panel: grafana.Panel{
				ID: index + 1, Type: "stat", Title: "Panel",
				Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
				Targets: []grafana.Target{{
					RefID: "A", Datasource: &grafana.DatasourceRef{UID: "prom1", Type: "prometheus"},
				}},
				GridPos: grafana.GridPos{X: (index % 2) * 12, Y: index / 2 * 4, W: 12, H: 4},
			},
			Variables: map[string]string{}, RepeatLength: 1,
		}
	}

	batch, ok := view.startRefresh()().(tea.BatchMsg)
	if !ok {
		t.Fatal("refresh did not return a command batch")
	}
	messages := make(chan tea.Msg, len(batch))
	for _, command := range batch {
		go func() {
			messages <- command()
		}()
	}
	for range batch {
		view.Update(<-messages)
	}

	if got := maximum.Load(); got > dashboardQueryConcurrency {
		t.Errorf("maximum in-flight queries = %d, limit is %d", got, dashboardQueryConcurrency)
	}
	if len(view.results) != len(view.instances) {
		t.Errorf("received %d results, want %d", len(view.results), len(view.instances))
	}
}

//nolint:cyclop // end-to-end assertions cover loading, variables, queries, and rendering
func TestDashboardViewEndToEnd(t *testing.T) {
	t.Parallel()

	queryCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/dashboards/uid/fixture":
				_, _ = w.Write([]byte(`{"dashboard":{
					"title":"Fleet fixture","refresh":"1m",
					"time":{"from":"now-1h","to":"now"},
					"templating":{"list":[
						{"name":"datasource","type":"datasource","query":"prometheus","current":{"value":"prom1"}},
						{"name":"cluster","type":"custom","query":"alpha,beta","multi":true,` +
					`"includeAll":true,"current":{"value":"$__all"}}
					]},
					"panels":[{"id":7,"type":"stat","title":"$cluster","repeat":"cluster","repeatDirection":"h","maxPerRow":2,
						"datasource":{"uid":"${datasource}","type":"prometheus"},
						"targets":[{"refId":"A","expr":"up{cluster=\"$cluster\"}","instant":true,"range":false}],
						"gridPos":{"x":0,"y":0,"w":24,"h":4},
						"fieldConfig":{"defaults":{"mappings":[{"type":"value","options":{"1":{"text":"OK","color":"green"}}}]}},
						"options":{"colorMode":"background","textMode":"value"}
					}]
				}}`))
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
			case "/api/ds/query":
				queryCount++
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{
					"schema":{"refId":"A","fields":[{"name":"Value","type":"number"}]},
					"data":{"values":[[1]]}
				}]}}}`))
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	view := newDashboardView(client, "fixture")
	_, command := view.Update(view.Init()())
	if command == nil {
		t.Fatal("dashboard load did not schedule variable resolution")
	}
	_, command = view.Update(command())
	if command == nil {
		t.Fatal("variable resolution did not schedule panel queries")
	}

	refreshMessage := command()
	batch, ok := refreshMessage.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("refresh returned %T, want a command batch", refreshMessage)
	}
	fetchMessage := batch[0]()
	fetchBatch, ok := fetchMessage.(tea.BatchMsg)
	if !ok {
		t.Fatalf("panel refresh returned %T, want a command batch", fetchMessage)
	}
	for _, fetch := range fetchBatch {
		view.Update(fetch())
	}

	if queryCount != 2 {
		t.Fatalf("executed %d panel queries, want 2", queryCount)
	}
	if len(view.loading) != 0 {
		t.Fatalf("panels still loading: %v", view.loading)
	}
	output := view.View(100, 16)
	for _, expected := range []string{"alpha", "beta", "OK"} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
}

//nolint:cyclop,gocyclo // end-to-end dependent-variable lifecycle assertions
func TestDashboardViewReResolvesDependentVariables(t *testing.T) {
	t.Parallel()

	client, queries := newDependentVariableClient(t)
	view := newDashboardView(client, "fixture")
	view.dashboard = dependentVariableDashboard(t)
	view.refresh = -1
	view.variableValues = map[string]string{"env": "prod", "instance": "prod-a"}
	view.variableGeneration = 1
	view.variableOptions = map[string][]string{
		"env": {"prod", "dev"}, "instance": {"prod-a", "prod-b"},
	}

	_, command := view.Update(setVariablesMsg{
		values: map[string]string{"env": "dev", "instance": "prod-a"},
	})
	if command == nil {
		t.Fatal("changing variables did not schedule resolution")
	}
	if view.variableGeneration != 2 {
		t.Fatalf("variable generation = %d, want 2", view.variableGeneration)
	}
	if view.generation != 1 {
		t.Fatalf("panel query generation = %d, want immediate invalidation to 1", view.generation)
	}
	if len(view.instances) != 0 {
		t.Fatal("panels expanded before dependent variables resolved")
	}

	resolved, ok := command().(variablesResolvedMsg)
	if !ok {
		t.Fatal("variable change did not return a resolution message")
	}
	if resolved.generation != view.variableGeneration {
		t.Fatalf("resolution generation = %d, want %d", resolved.generation, view.variableGeneration)
	}
	variableQueries := queries.forRef("vars")
	if len(variableQueries) != 1 || variableQueries[0] != `up{env="dev"}` {
		t.Fatalf("dependent variable queries = %v, want current parent selection", variableQueries)
	}
	if panelQueries := queries.forRef("A"); len(panelQueries) != 0 {
		t.Fatalf("panel queries ran before resolution: %v", panelQueries)
	}

	_, command = view.Update(resolved)
	if command == nil {
		t.Fatal("resolved variables did not schedule a panel refresh")
	}
	if view.generation != 2 {
		t.Fatalf("panel query generation = %d, want 2", view.generation)
	}
	if got := view.variableValues["env"]; got != "dev" {
		t.Errorf("independent selection = %q, want dev", got)
	}
	if got := view.variableValues["instance"]; got != "dev-a" {
		t.Errorf("dependent selection = %q, want refreshed default dev-a", got)
	}
	if got := view.variableOptions["instance"]; len(got) != 2 || got[0] != "dev-a" || got[1] != "dev-b" {
		t.Errorf("dependent options = %v, want [dev-a dev-b]", got)
	}

	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, fetch := range batch {
			view.Update(fetch())
		}
	} else {
		view.Update(message)
	}
	panelQueries := queries.forRef("A")
	if len(panelQueries) != 1 || panelQueries[0] != `up{instance="dev-a"}` {
		t.Errorf("panel queries = %v, want refreshed dependent selection", panelQueries)
	}
}

func TestDashboardViewChangeVariablesRejectsInFlightQuery(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Fixture"}
	view.instances = []grafana.PanelInstance{{
		Key: "panel",
		Panel: grafana.Panel{
			ID: 1, Type: "stat", Title: "Panel", GridPos: grafana.GridPos{W: 24, H: 4},
		},
		RepeatLength: 1,
	}}
	view.variableValues = map[string]string{"instance": "old"}
	view.generation = 4
	view.loading["panel"] = true
	canceled := false
	view.cancel = func() { canceled = true }

	_, command := view.Update(setVariablesMsg{
		values: map[string]string{"instance": "new"},
	})
	if command == nil {
		t.Fatal("changing variables did not schedule resolution")
	}
	if !canceled {
		t.Error("changing variables did not cancel the in-flight query")
	}
	if view.generation != 5 {
		t.Fatalf("panel query generation = %d, want 5", view.generation)
	}

	view.Update(dashboardPanelDataMsg{
		key: "panel", generation: 4, result: numericResult(42),
	})
	if view.results["panel"] != nil {
		t.Fatal("old query result was installed after the selection changed")
	}
	if output := view.View(80, 4); strings.Contains(output, "42") {
		t.Errorf("old query result was displayed under the new selection:\n%s", output)
	}
}

func TestDashboardViewRejectsStaleVariables(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Fixture"}
	view.variableValues = map[string]string{"instance": "current"}
	view.variableOptions = map[string][]string{"instance": {"current"}}
	_ = view.resolveVariables()
	view.Update(setTimeRangeMsg{
		timeRange: grafana.TimeRange{From: "now-15m", To: "now"},
	})
	if view.variableGeneration != 2 {
		t.Fatalf("variable generation after time change = %d, want 2", view.variableGeneration)
	}

	_, command := view.Update(variablesResolvedMsg{
		generation: 1,
		options:    map[string][]string{"instance": {"stale"}},
		defaults:   map[string]string{"instance": "stale"},
	})

	if command != nil {
		t.Error("stale variable resolution scheduled a refresh")
	}
	if got := view.variableValues["instance"]; got != "current" {
		t.Errorf("stale defaults replaced selection with %q", got)
	}
	if got := view.variableOptions["instance"][0]; got != "current" {
		t.Errorf("stale options replaced current option with %q", got)
	}
}

func TestDashboardViewVariableResolutionErrorLifecycle(t *testing.T) {
	t.Parallel()

	view := newDashboardView(newFlakyVariableClient(t), "fixture")
	view.dashboard = datasourceVariableDashboard()
	view.loading["panel"] = true

	failure, ok := view.resolveVariables()().(variablesResolveErrorMsg)
	if !ok {
		t.Fatal("resolver failure did not return a variable-resolution error")
	}
	if failure.generation != 1 {
		t.Fatalf("error generation = %d, want 1", failure.generation)
	}
	_, command := view.Update(failure)
	if command != nil {
		t.Error("variable-resolution failure scheduled another command")
	}
	if view.err == nil {
		t.Error("current variable-resolution error was not retained")
	}
	if len(view.loading) != 0 {
		t.Errorf("panels remained loading after resolution failed: %v", view.loading)
	}

	_, command = view.Update(setVariablesMsg{
		values: map[string]string{"datasource": "prom1"},
	})
	resolved, ok := command().(variablesResolvedMsg)
	if !ok {
		t.Fatal("retry did not resolve variables successfully")
	}
	view.Update(resolved)
	if view.err != nil {
		t.Fatalf("successful retry left error %v", view.err)
	}

	view.Update(failure)
	if view.err != nil {
		t.Errorf("stale failure replaced healthy state with %v", view.err)
	}
	if got := view.variableValues["datasource"]; got != "prom1" {
		t.Errorf("stale failure changed healthy selection to %q", got)
	}
}

func TestDashboardViewTimeChangeInvalidatesInflightQuery(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	view.dashboard = dependentVariableDashboard(t)
	view.generation = 4
	view.variableGeneration = 1
	_, command := view.Update(setTimeRangeMsg{
		timeRange: grafana.TimeRange{From: "now-15m", To: "now"},
	})
	if command == nil {
		t.Fatal("time change did not schedule variable resolution")
	}
	if view.generation != 5 {
		t.Fatalf("panel generation = %d, want 5", view.generation)
	}

	view.handlePanelData(dashboardPanelDataMsg{
		key: "old", generation: 4, result: numericResult(1),
	})
	if _, exists := view.results["old"]; exists {
		t.Fatal("old time-range result was accepted after invalidation")
	}
}

func TestDashboardViewCachesRenderedBody(t *testing.T) {
	t.Parallel()

	view := newDashboardView(nil, "fixture")
	instance := grafana.PanelInstance{Key: "panel", Panel: grafana.Panel{Type: "table"}}
	newResult := func(value string) *grafana.QueryResult {
		return &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
			"A": {Frames: []grafana.DataFrame{{
				Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{{Name: "Value", Type: "string"}}},
				Data:   grafana.DataFrameData{Values: []json.RawMessage{json.RawMessage(`["` + value + `"]`)}},
			}}},
		}}
	}

	result := newResult("first")
	view.results[instance.Key] = result
	first := view.renderPanelBody(instance, 40, 5)
	if !strings.Contains(first, "first") {
		t.Fatalf("initial body = %q, want first value", first)
	}

	// Results are immutable in production; changing this pointer's contents
	// demonstrates that the second call uses the cached rendering.
	result.Results["A"].Frames[0].Data.Values[0] = json.RawMessage(`["second"]`)
	cached := view.renderPanelBody(instance, 40, 5)
	if !strings.Contains(cached, "first") || strings.Contains(cached, "second") {
		t.Fatalf("cached body = %q, want original rendering", cached)
	}

	view.results[instance.Key] = newResult("second")
	updated := view.renderPanelBody(instance, 40, 5)
	if !strings.Contains(updated, "second") {
		t.Fatalf("updated body = %q, want new result", updated)
	}
}

func TestDashboardRefreshInterval(t *testing.T) {
	t.Parallel()

	if got := dashboardRefreshInterval("0s"); got != 0 {
		t.Errorf("disabled refresh = %v, want 0", got)
	}
	if got := dashboardRefreshInterval("5m"); got != 5*time.Minute {
		t.Errorf("refresh = %v, want 5m", got)
	}
}

type testQueryRecorder struct {
	mu          sync.Mutex
	expressions map[string][]string
}

func (r *testQueryRecorder) add(refID, expression string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expressions[refID] = append(r.expressions[refID], expression)
}

func (r *testQueryRecorder) forRef(refID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.expressions[refID]...)
}

func newFlakyVariableClient(t *testing.T) *grafana.Client {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/datasources" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if requests.Add(1) == 1 {
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
		},
	))
	t.Cleanup(server.Close)

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	return client
}

func datasourceVariableDashboard() *grafana.Dashboard {
	return &grafana.Dashboard{
		Title: "Datasource variable",
		Templating: grafana.Templating{List: []grafana.TemplateVariable{{
			Name: "datasource", Type: "datasource", Query: json.RawMessage(`"prometheus"`),
		}}},
	}
}

func newDependentVariableClient(t *testing.T) (*grafana.Client, *testQueryRecorder) {
	t.Helper()
	recorder := &testQueryRecorder{expressions: make(map[string][]string)}
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/datasources":
				_, _ = w.Write([]byte(`[{"uid":"prom1","name":"Prometheus","type":"prometheus"}]`))
			case "/api/ds/query":
				var request struct {
					Queries []struct {
						RefID string `json:"refId"`
						Expr  string `json:"expr"`
					} `json:"queries"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decoding query: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if len(request.Queries) != 1 {
					t.Errorf("got %d queries, want 1", len(request.Queries))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				query := request.Queries[0]
				recorder.add(query.RefID, query.Expr)
				switch query.RefID {
				case "vars":
					_, _ = w.Write([]byte(`{"results":{"vars":{"frames":[{
						"schema":{"refId":"vars","fields":[{"name":"Value","type":"string"}]},
						"data":{"values":[["dev-a","dev-b"]]}
					}]}}}`))
				case "A":
					_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
				default:
					t.Errorf("unexpected query ref ID %q", query.RefID)
					w.WriteHeader(http.StatusBadRequest)
				}
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		},
	))
	t.Cleanup(server.Close)

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	return client, recorder
}

func dependentVariableDashboard(t *testing.T) *grafana.Dashboard {
	t.Helper()
	var dashboard grafana.Dashboard
	err := json.Unmarshal([]byte(`{
		"title":"Dependent variables",
		"templating":{"list":[
			{"name":"env","type":"custom","query":"prod,dev","current":{"value":"prod"}},
			{"name":"instance","type":"query","datasource":{"uid":"prom1","type":"prometheus"},
				"query":"query_result(up{env=\"$env\"})","current":{"value":"prod-a"},"refresh":1}
		]},
		"panels":[{
			"id":1,"type":"stat","title":"$instance",
			"datasource":{"uid":"prom1","type":"prometheus"},
			"targets":[{"refId":"A","expr":"up{instance=\"$instance\"}","instant":true,"range":false}],
			"gridPos":{"x":0,"y":0,"w":24,"h":4}
		}]
	}`), &dashboard)
	if err != nil {
		t.Fatal(err)
	}

	return &dashboard
}

type testError struct{}

func (testError) Error() string { return "test query failed" }

func numericResult(value float64) *grafana.QueryResult {
	encoded, err := json.Marshal([]float64{value})
	if err != nil {
		panic(err)
	}

	return &grafana.QueryResult{
		Results: map[string]grafana.QueryResultData{
			"A": {Frames: []grafana.DataFrame{{
				Schema: grafana.DataFrameSchema{
					RefID:  "A",
					Fields: []grafana.DataFrameField{{Name: "Value", Type: "number"}},
				},
				Data: grafana.DataFrameData{Values: []json.RawMessage{encoded}},
			}}},
		},
	}
}
