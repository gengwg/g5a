package main

import "testing"

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
