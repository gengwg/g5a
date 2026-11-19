//nolint:exhaustruct,testpackage // internal view stack with empty dashboard fixtures
package grafanatui

import (
	"testing"

	"github.com/gengwg/g5a/grafana"
)

// A time range picked while a panel is zoomed belongs to the dashboard, so the
// grid must show it after Esc instead of the range it had before zooming.
func TestZoomedPanelTimeRangeAppliesToDashboardOnReturn(t *testing.T) {
	t.Parallel()

	sixHours := grafana.TimeRange{From: timeSixHours, To: "now"}
	dayRange := grafana.TimeRange{From: "now-24h", To: "now"}

	dashboard := newDashboardView(nil, "fixture")
	dashboard.dashboard = &grafana.Dashboard{}
	dashboard.timeRange = sixHours

	panel := newPanelView(nil, "fixture")
	panel.dashboard = &grafana.Dashboard{}
	panel.preserveTimeRange = true
	panel.timeRange = sixHours
	picker := newTimeRangePickerView(sixHours)

	app := &App{views: []view{dashboard, panel, picker}}
	_, _ = app.Update(popAndSetTimeRangeMsg{timeRange: dayRange})
	_, _ = app.Update(popViewMsg{})

	if len(app.views) != 1 || app.views[0] != dashboard {
		t.Fatalf("expected the dashboard to be the only view, got %d views", len(app.views))
	}
	if dashboard.timeRange != dayRange {
		t.Errorf("dashboard time range = %+v, want %+v", dashboard.timeRange, dayRange)
	}
}
