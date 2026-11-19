//nolint:exhaustruct,testpackage // internal views and empty dashboard fixture
package grafanatui

import (
	"testing"

	"github.com/gengwg/g5a/grafana"
)

func TestSetTimeRangePreservesDashboardSettings(t *testing.T) {
	t.Parallel()

	current := grafana.TimeRange{
		From: "now/d", To: "now/d", Timezone: "UTC", WeekStart: "saturday", NowDelay: "1h",
	}
	next := grafana.TimeRange{From: timeSixHours, To: "now", Timezone: "", WeekStart: "", NowDelay: ""}

	dashboard := newDashboardView(nil, "fixture")
	dashboard.timeRange = current
	_, _ = dashboard.Update(setTimeRangeMsg{timeRange: next})
	assertTimeRangeSettings(t, dashboard.timeRange)

	panel := newPanelView(nil, "fixture")
	panel.dashboard = &grafana.Dashboard{}
	panel.timeRange = current
	_, _ = panel.handleSetTimeRange(setTimeRangeMsg{timeRange: next})
	assertTimeRangeSettings(t, panel.timeRange)
}

func assertTimeRangeSettings(t *testing.T, got grafana.TimeRange) {
	t.Helper()
	if got.Timezone != "UTC" || got.WeekStart != "saturday" || got.NowDelay != "1h" {
		t.Errorf("time range settings were not preserved: %+v", got)
	}
}
