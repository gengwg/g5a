package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGrafanaURL(t *testing.T) {
	t.Parallel()

	parts := parseGrafanaURL(
		"https://grafana.example.com/d-solo/abc/slug?orgId=2&from=now-6h&to=now" +
			"&var-cluster=prod&var-cluster=staging&viewPanel=22",
	)
	if parts.baseURL != "https://grafana.example.com" {
		t.Errorf("baseURL = %q", parts.baseURL)
	}
	if parts.dashUID != "abc" || parts.panelID != 22 {
		t.Errorf("dashboard/panel = %q/%d", parts.dashUID, parts.panelID)
	}
	if !parts.timeRangeSet {
		t.Error("URL time range was not marked as explicitly set")
	}
	if parts.timeRange.From != "now-6h" || parts.timeRange.To != "now" {
		t.Errorf("time range = %+v", parts.timeRange)
	}
	if parts.orgID != "2" {
		t.Errorf("orgID = %q", parts.orgID)
	}
	if parts.variables["cluster"] != "prod,staging" {
		t.Errorf("cluster variable = %q", parts.variables["cluster"])
	}
}

func TestParseGrafanaURLDefaults(t *testing.T) {
	t.Parallel()

	parts := parseGrafanaURL("https://grafana.example.com/d/abc")
	if parts.timeRange.From != "now-1h" || parts.timeRange.To != "now" {
		t.Errorf("default time range = %+v", parts.timeRange)
	}
	if parts.timeRangeSet {
		t.Error("default time range should not be marked as explicit")
	}
	if parts.orgID != "" || len(parts.variables) != 0 {
		t.Errorf("unexpected defaults: %+v", parts)
	}
}

func TestResolvePositionalURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		current    string
		args       []string
		urlFlagSet bool
		want       string
	}{
		{
			name: "positional", current: "", args: []string{"https://grafana.example.com/d/abc"},
			urlFlagSet: false, want: "https://grafana.example.com/d/abc",
		},
		{
			name: "flag wins", current: "https://flag.example.com",
			args: []string{"https://positional.example.com"}, urlFlagSet: true, want: "https://flag.example.com",
		},
		{
			name: "environment fallback preserved", current: "https://env.example.com",
			args: nil, urlFlagSet: false, want: "https://env.example.com",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := resolvePositionalURL(test.current, test.args, test.urlFlagSet)
			if got != test.want {
				t.Errorf("URL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInitStderrDebugLogging(t *testing.T) {
	for _, test := range []struct {
		name      string
		debug     bool
		wantDebug bool
	}{
		{name: "default", debug: false, wantDebug: false},
		{name: "debug", debug: true, wantDebug: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cacheDir := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", cacheDir)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`[{"uid":"body-secret"}]`))
			}))
			defer server.Close()

			cleanup := initStderr(test.debug)
			client, err := grafana.NewClient(
				server.URL,
				grafana.WithToken("token-secret"),
				grafana.WithHTTPClient(server.Client()),
			)
			if err == nil {
				err = client.Ping(t.Context())
			}
			cleanup()
			require.NoError(t, err)

			contents, err := os.ReadFile(filepath.Join(cacheDir, "g5a", "g5a.log"))
			require.NoError(t, err)
			log := string(contents)
			assert.Equal(t, test.wantDebug, strings.Contains(log, "grafana request"))
			assert.NotContains(t, log, "token-secret")
			assert.NotContains(t, log, "body-secret")
			if test.wantDebug {
				assert.Contains(t, log, "level=DEBUG")
				assert.Contains(t, log, "path=/api/search")
				assert.Contains(t, log, "status=200")
			}
		})
	}
}
