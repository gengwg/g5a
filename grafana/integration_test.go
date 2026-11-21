//go:build integration

package grafana_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// promConfig is a minimal Prometheus configuration that scrapes itself
// every second so integration tests don't have to wait for the default
// 15s interval.
const promConfig = `global:
  scrape_interval: 1s
scrape_configs:
  - job_name: prometheus
    static_configs:
      - targets: ["localhost:9090"]
`

// Images are pinned to the latest multi-architecture digests from 2026-11-20.
const (
	grafanaUser     = "admin"
	grafanaPassword = "admin"
	grafanaImage    = "grafana/grafana@sha256:0615ec4fbd4072004b7d86b46ded63013a74b7d6eb1119273dd89e8fa25c13cf"
	prometheusImage = "prom/prometheus@sha256:e04987641b57704f408e31f6373ba4138ecf27cb4a165d24aabb6c38fddd0f06"
)

func integrationSetupFailure(t *testing.T, step string, err error) {
	t.Helper()
	if os.Getenv("CI") == "true" {
		t.Fatalf("integration test: %s: %v", step, err)
	}
	t.Skipf("skipping integration test: %s: %v", step, err)
}

// setupGrafana starts Prometheus and Grafana containers on a shared
// Docker network, adds Prometheus as a datasource in Grafana, creates
// a test dashboard with PromQL panels, and returns a configured client.
func setupGrafana(t *testing.T) *grafana.Client {
	t.Helper()

	ctx := t.Context()

	// Create a shared network so Grafana can reach Prometheus by name.
	dockerNet, err := network.New(ctx)
	if err != nil {
		integrationSetupFailure(t, "could not create docker network", err)
	}

	t.Cleanup(func() {
		if removeErr := dockerNet.Remove(context.Background()); removeErr != nil {
			t.Logf("warning: failed to remove network: %v", removeErr)
		}
	})

	// Start Prometheus with a fast scrape interval (1s).
	promContainer, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{ //nolint:exhaustruct
			ContainerRequest: testcontainers.ContainerRequest{ //nolint:exhaustruct
				Image:        prometheusImage,
				ExposedPorts: []string{"9090/tcp"},
				Networks:     []string{dockerNet.Name},
				NetworkAliases: map[string][]string{
					dockerNet.Name: {"prometheus"},
				},
				Files: []testcontainers.ContainerFile{{
					Reader:            bytes.NewReader([]byte(promConfig)),
					ContainerFilePath: "/etc/prometheus/prometheus.yml",
					FileMode:          0o644,
				}},
				WaitingFor: wait.ForHTTP("/-/ready").
					WithPort("9090/tcp").
					WithStartupTimeout(60 * time.Second),
			},
			Started: true,
		},
	)
	if err != nil {
		integrationSetupFailure(t, "could not start prometheus container", err)
	}

	t.Cleanup(func() {
		if termErr := promContainer.Terminate(context.Background(), testcontainers.StopTimeout(0)); termErr != nil {
			t.Logf("warning: failed to terminate prometheus container: %v", termErr)
		}
	})

	// Start Grafana on the same network.
	grafanaContainer, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{ //nolint:exhaustruct
			ContainerRequest: testcontainers.ContainerRequest{ //nolint:exhaustruct
				Image:        grafanaImage,
				ExposedPorts: []string{"3000/tcp"},
				Networks:     []string{dockerNet.Name},
				Env: map[string]string{
					"GF_SECURITY_ADMIN_USER":     grafanaUser,
					"GF_SECURITY_ADMIN_PASSWORD": grafanaPassword,
				},
				WaitingFor: wait.ForHTTP("/api/health").
					WithPort("3000/tcp").
					WithStartupTimeout(60 * time.Second),
			},
			Started: true,
		},
	)
	if err != nil {
		integrationSetupFailure(t, "could not start grafana container", err)
	}

	t.Cleanup(func() {
		if termErr := grafanaContainer.Terminate(context.Background(), testcontainers.StopTimeout(0)); termErr != nil {
			t.Logf("warning: failed to terminate grafana container: %v", termErr)
		}
	})

	host, err := grafanaContainer.Host(ctx)
	require.NoError(t, err)

	port, err := grafanaContainer.MappedPort(ctx, "3000/tcp")
	require.NoError(t, err)

	baseURL := "http://" + net.JoinHostPort(host, port.Port())

	client, err := grafana.NewClient(
		baseURL,
		grafana.WithBasicAuth(grafanaUser, grafanaPassword),
	)
	require.NoError(t, err)

	// Wait for Prometheus to have scraped data before proceeding.
	waitForPrometheusData(t, promContainer, ctx)

	datasourceUID := createPrometheusDatasource(t, baseURL)
	waitForDatasourceHealth(t, baseURL, datasourceUID)
	createTestDashboard(t, baseURL, datasourceUID)

	return client
}

// waitForPrometheusData polls Prometheus until it has at least one
// sample for the up metric.
func waitForPrometheusData(
	t *testing.T,
	container testcontainers.Container,
	ctx context.Context,
) {
	t.Helper()

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "9090/tcp")
	require.NoError(t, err)

	promURL := "http://" + net.JoinHostPort(host, port.Port())

	deadline := time.Now().Add(30 * time.Second) //nolint:mnd
	for time.Now().Before(deadline) {
		req, reqErr := http.NewRequestWithContext(
			ctx, http.MethodGet,
			promURL+"/api/v1/query?query=up",
			nil,
		)
		require.NoError(t, reqErr)

		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			time.Sleep(500 * time.Millisecond) //nolint:mnd
			continue
		}

		var result struct {
			Data struct {
				Result []any `json:"result"`
			} `json:"data"`
		}

		_ = json.NewDecoder(resp.Body).Decode(&result)
		_ = resp.Body.Close()

		if len(result.Data.Result) > 0 {
			// With a 1s scrape interval a couple more seconds is
			// enough for query_range to return multiple data points.
			time.Sleep(3 * time.Second) //nolint:mnd
			return
		}

		time.Sleep(500 * time.Millisecond) //nolint:mnd
	}

	t.Fatal("timed out waiting for prometheus to have data")
}

// createPrometheusDatasource creates a Prometheus datasource in
// Grafana pointing at the Prometheus container and returns its UID.
func createPrometheusDatasource(t *testing.T, baseURL string) string {
	t.Helper()

	payload := map[string]any{
		"name":   "Prometheus",
		"type":   "prometheus",
		"access": "proxy",
		"url":    "http://prometheus:9090",
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		baseURL+"/api/datasources",
		bytes.NewReader(body),
	)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"failed to create datasource")

	var result struct {
		Datasource struct {
			UID string `json:"uid"`
		} `json:"datasource"`
	}

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result.Datasource.UID)

	return result.Datasource.UID
}

func waitForDatasourceHealth(t *testing.T, baseURL, datasourceUID string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			baseURL+"/api/datasources/uid/"+datasourceUID+"/health",
			nil,
		)
		require.NoError(t, err)
		request.SetBasicAuth(grafanaUser, grafanaPassword)

		response, err := http.DefaultClient.Do(request)
		if err == nil {
			var health struct {
				Status string `json:"status"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&health)
			_ = response.Body.Close()
			if decodeErr == nil && response.StatusCode == http.StatusOK && health.Status == "OK" {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	t.Fatal("timed out waiting for the Prometheus datasource to become healthy")
}

// createTestDashboard creates timeseries, stat, joined-table, and repeated-stat
// panels with datasource, query_result, and custom variables.
func createTestDashboard(t *testing.T, baseURL, datasourceUID string) {
	t.Helper()

	dsRef := map[string]string{
		"type": "prometheus",
		"uid":  datasourceUID,
	}

	dashboard := map[string]any{
		"dashboard": map[string]any{
			"title": "Integration Test Dashboard",
			"uid":   "integration-test",
			"time":  map[string]string{"from": "now-1h", "to": "now"},
			"templating": map[string]any{
				"list": []map[string]any{
					{
						"name": "datasource", "type": "datasource", "query": "prometheus",
						"current": map[string]any{"text": "Prometheus", "value": datasourceUID},
					},
					{
						"name": "job", "type": "query", "multi": true, "includeAll": true, "sort": 1,
						"datasource": dsRef,
						"query": map[string]any{
							"query": "query_result(count by (job) (up))", "refId": "vars",
						},
						"regex":   `/job="([^"]+)"/`,
						"current": map[string]any{"text": "All", "value": "$__all"},
					},
					{
						"name": "cluster", "type": "custom", "query": "alpha,beta,gamma", "multi": true, "includeAll": true,
						"current": map[string]any{"text": "All", "value": "$__all"},
					},
				},
			},
			"panels": []map[string]any{
				{
					"id":         1,
					"type":       "timeseries",
					"title":      "Time Series Panel",
					"datasource": dsRef,
					"targets": []map[string]any{{
						"refId":      "A",
						"datasource": dsRef,
						"expr":       "up",
					}},
					"gridPos": map[string]int{
						"h": 8, "w": 12, "x": 0, "y": 0,
					},
				},
				{
					"id":         2,
					"type":       "stat",
					"title":      "Stat Panel",
					"datasource": dsRef,
					"targets": []map[string]any{{
						"refId":      "A",
						"datasource": dsRef,
						"expr":       "up",
					}},
					"gridPos": map[string]int{
						"h": 4, "w": 6, "x": 0, "y": 8,
					},
				},
				{
					"id": 3, "type": "table", "title": "Joined Table Panel", "datasource": dsRef,
					"targets": []map[string]any{
						{
							"refId": "A", "datasource": dsRef, "expr": "prometheus_build_info * 1",
							"instant": true, "range": false, "format": "table",
						},
						{
							"refId": "B", "datasource": dsRef, "expr": "up * 1",
							"instant": true, "range": false, "format": "table",
						},
					},
					"transformations": []map[string]any{
						{"id": "joinByField", "options": map[string]any{"byField": "job", "mode": "outer"}},
						{"id": "filterFieldsByName", "options": map[string]any{
							"include": map[string]string{"pattern": "^(job|Value #.*)$"},
						}},
						{"id": "organize", "options": map[string]any{
							"excludeByName": map[string]bool{},
							"indexByName":   map[string]int{"job": 0, "Value #A": 1, "Value #B": 2},
							"renameByName":  map[string]string{"Value #A": "build_info", "Value #B": "up"},
						}},
					},
					"fieldConfig": map[string]any{
						"defaults": map[string]any{"noValue": "-"},
						"overrides": []map[string]any{{
							"matcher": map[string]string{"id": "byName", "options": "up"},
							"properties": []map[string]any{
								{"id": "mappings", "value": []map[string]any{{
									"type": "value", "options": map[string]any{
										"1": map[string]string{"text": "UP", "color": "green"},
									},
								}}},
								{"id": "custom.cellOptions", "value": map[string]string{"type": "color-background", "mode": "basic"}},
							},
						}},
					},
					"options": map[string]any{"showHeader": true},
					"gridPos": map[string]int{"h": 8, "w": 12, "x": 12, "y": 0},
				},
				{
					"id": 4, "type": "stat", "title": "$cluster", "repeat": "cluster", "repeatDirection": "h", "maxPerRow": 2,
					"datasource": map[string]string{"type": "prometheus", "uid": "${datasource}"},
					"targets": []map[string]any{{
						"refId": "A", "datasource": dsRef,
						"expr":    `label_replace(up, "cluster", "$cluster", "job", "prometheus")`,
						"instant": true, "range": false,
					}},
					"fieldConfig": map[string]any{
						"defaults": map[string]any{
							"displayName": "${__field.labels.cluster}",
							"mappings": []map[string]any{{
								"type": "value", "options": map[string]any{
									"1": map[string]string{"text": "OK", "color": "green"},
								},
							}},
						},
					},
					"options": map[string]any{
						"colorMode": "background", "textMode": "value_and_name",
						"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}},
					},
					"gridPos": map[string]int{"h": 4, "w": 24, "x": 0, "y": 12},
				},
			},
		},
		"overwrite": true,
	}

	body, err := json.Marshal(dashboard)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		baseURL+"/api/dashboards/db",
		bytes.NewReader(body),
	)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(grafanaUser, grafanaPassword)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"failed to create dashboard")
}

func TestIntegration(t *testing.T) {
	client := setupGrafana(t)

	t.Run("SearchDashboards", func(t *testing.T) {
		results, err := client.SearchDashboards(t.Context(), "")
		require.NoError(t, err)
		require.NotEmpty(t, results)

		var found bool
		for _, r := range results {
			if r.Title == "Integration Test Dashboard" {
				found = true
				break
			}
		}

		assert.True(t, found,
			"expected to find Integration Test Dashboard in search results")
	})

	t.Run("GetDashboard", func(t *testing.T) {
		results, err := client.SearchDashboards(
			t.Context(), "Integration Test",
		)
		require.NoError(t, err)
		require.NotEmpty(t, results)

		dashboard, err := client.GetDashboard(
			t.Context(), results[0].UID,
		)
		require.NoError(t, err)

		assert.Equal(t, "Integration Test Dashboard", dashboard.Title)
		assert.Len(t, dashboard.Panels, 4)
	})

	t.Run("QueryPanel", func(t *testing.T) {
		results, err := client.SearchDashboards(
			t.Context(), "Integration Test",
		)
		require.NoError(t, err)
		require.NotEmpty(t, results)

		dashboard, err := client.GetDashboard(
			t.Context(), results[0].UID,
		)
		require.NoError(t, err)

		panels := grafana.FlattenPanels(dashboard.Panels)
		require.NotEmpty(t, panels)

		timeRange := grafana.TimeRange{From: "now-1h", To: "now"}
		var result *grafana.QueryResult
		require.Eventually(t, func() bool {
			var queryErr error
			result, queryErr = client.QueryPanel(
				t.Context(), panels[0], timeRange, 100, nil, nil, panels,
			)
			if queryErr != nil {
				return false
			}
			for _, data := range result.Results {
				for _, frame := range data.Frames {
					if _, _, tsErr := frame.TimeSeries(); tsErr == nil {
						return true
					}
				}
			}

			return false
		}, 30*time.Second, 250*time.Millisecond,
			"expected a nonempty time-series response from Grafana")
		require.NotEmpty(t, result.Results)

		checkedFrames := 0
		for _, data := range result.Results {
			for _, frame := range data.Frames {
				if len(frame.Schema.Fields) == 0 {
					continue
				}
				timestamps, series, tsErr := frame.TimeSeries()
				require.NoError(t, tsErr, "frame schema: %+v; data: %+v", frame.Schema, frame.Data)
				assert.NotEmpty(t, timestamps)
				assert.NotEmpty(t, series)
				checkedFrames++
			}
		}
		assert.Positive(t, checkedFrames)
	})

	t.Run("FullFlow", func(t *testing.T) {
		// Search for dashboards.
		results, err := client.SearchDashboards(t.Context(), "")
		require.NoError(t, err)
		require.NotEmpty(t, results)

		// Get the test dashboard.
		var dashboardUID string
		for _, r := range results {
			if r.Title == "Integration Test Dashboard" {
				dashboardUID = r.UID
				break
			}
		}

		require.NotEmpty(t, dashboardUID,
			"expected to find Integration Test Dashboard")

		dashboard, err := client.GetDashboard(
			t.Context(), dashboardUID,
		)
		require.NoError(t, err)

		options, selections, err := client.ResolveVariableOptionsWithTimeRange(
			t.Context(),
			dashboard.Templating.List,
			grafana.TimeRange{From: "now-1h", To: "now"},
		)
		require.NoError(t, err)
		assert.Contains(t, options["job"], "prometheus")

		panels := grafana.FlattenPanels(dashboard.Panels)
		instances := grafana.ExpandPanels(
			dashboard.Panels,
			dashboard.Templating.List,
			options,
			selections,
		)
		require.Len(t, instances, 6)

		for _, instance := range instances {
			result, queryErr := client.QueryPanel(
				t.Context(),
				instance.Panel,
				grafana.TimeRange{From: "now-1h", To: "now"},
				80,
				dashboard.Templating.List,
				instance.Variables,
				panels,
			)
			require.NoError(t, queryErr, "panel %q query failed", instance.Panel.Title)
			require.NotEmpty(t, result.Results, "panel %q should return data", instance.Panel.Title)

			for refID, data := range result.Results {
				assert.NotEmpty(t, data.Frames,
					"panel %q refID %q should have frames", instance.Panel.Title, refID)
			}

			output := grafanatui.RendererForPanel(instance.Panel.Type).Render(
				result,
				instance.Panel,
				100,
				20,
				nil,
			)
			switch {
			case instance.Panel.Repeat == "cluster":
				assert.Contains(t, output, "OK")
				assert.Contains(t, output, instance.Panel.Title)
			case instance.Panel.ID == 3:
				assert.Contains(t, output, "build_info")
				assert.Contains(t, output, "up")
				assert.Contains(t, output, "UP")
			}
		}
	})
}
