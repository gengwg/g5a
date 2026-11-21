//nolint:exhaustruct,testpackage // internal view stack with empty dashboard fixtures
package grafanatui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestAppDashboardRefreshHiddenByPanelOnlyRefreshesPanel(t *testing.T) {
	t.Parallel()

	app, dashboard, panel := newRefreshTestStack()
	dashboardCanceled := false
	panelCanceled := false
	dashboard.cancel = func() { dashboardCanceled = true }
	panel.cancel = func() { panelCanceled = true }

	_, command := app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	if command == nil {
		t.Fatal("refresh returned no command")
	}
	if dashboard.generation != 2 || dashboardCanceled || dashboard.loading["grid"] {
		t.Error("hidden dashboard started a refresh")
	}
	if panel.generation != 5 || !panelCanceled || !panel.loading {
		t.Error("visible panel did not start a refresh")
	}
}

func TestAppRedirectedRefreshExecutesOnlyPanelQuery(t *testing.T) {
	t.Parallel()

	client, queries := newDependentVariableClient(t)
	dashboard := newDashboardView(client, "fixture")
	dashboard.dashboard = dependentVariableDashboard(t)
	dashboard.instances = []grafana.PanelInstance{{
		Key: "grid", Panel: dashboard.dashboard.Panels[0],
	}}
	dashboard.generation = 2
	dashboard.refresh = 0
	panel := newPanelView(client, "fixture")
	panel.dashboard = dependentVariableDashboard(t)
	panel.panels = grafana.FlattenPanels(panel.dashboard.Panels)
	panel.variableValues = map[string]string{"env": "prod", "instance": "prod-a"}
	panel.variableOptions = map[string][]string{
		"env": {"prod", "dev"}, "instance": {"prod-a", "prod-b"},
	}
	panel.variablesReady = true
	panel.loading = false
	app := &App{views: []view{dashboard, panel}}

	_, command := app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	for _, message := range executeImmediateCommands(command) {
		panel.Update(message)
	}
	if got := queries.forRef("A"); len(got) != 1 || got[0] != `up{instance="prod-a"}` {
		t.Errorf("panel queries = %v, want one visible-panel query", got)
	}
	if dashboard.generation != 2 || dashboard.loading["grid"] {
		t.Error("redirected command queried the hidden grid")
	}
}

func TestAppHiddenDashboardRefreshRejectsStaleGeneration(t *testing.T) {
	t.Parallel()

	app, dashboard, panel := newRefreshTestStack()
	_, _ = app.handleDashboardRefresh(dashboardRefreshMsg{generation: 1})
	if dashboard.generation != 2 || panel.generation != 4 || panel.loading {
		t.Error("stale refresh changed dashboard or panel state")
	}
}

func TestAppRefreshReachesPanelBelowOverlay(t *testing.T) {
	t.Parallel()

	app, dashboard, panel := newRefreshTestStack()
	app.views = append(app.views, newTimeRangePickerView(panel.timeRange))
	_, _ = app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	if dashboard.generation != 2 || panel.generation != 5 {
		t.Error("refresh did not reach the panel below its overlay")
	}
	_, _ = app.Update(panelDataMsg{generation: 5, result: numericResult(42)})
	if panel.data == nil || panel.loading {
		t.Error("overlay discarded the panel result or left the panel loading")
	}
}

func TestAppVariablesResolveBelowOverlay(t *testing.T) {
	t.Parallel()

	app, _, panel := newRefreshTestStack()
	app.views = append(app.views, newTimeRangePickerView(panel.timeRange))
	panel.variablesReady = false
	_, command := app.Update(variablesResolvedMsg{
		generation: panel.variableGeneration,
		options:    map[string][]string{},
		defaults:   map[string]string{},
	})
	if command == nil || !panel.variablesReady || panel.generation != 5 {
		t.Error("overlay discarded variable resolution for the panel")
	}
}

func TestAppPanelRefreshWaitsForVariables(t *testing.T) {
	t.Parallel()

	app, _, panel := newRefreshTestStack()
	panel.variablesReady = false
	_, _ = app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	if panel.generation != 4 {
		t.Error("panel queried while variables were resolving")
	}
	panel.variablesReady = true
	_, _ = app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	if panel.generation != 5 {
		t.Error("panel did not refresh after variables resolved")
	}
}

func TestAppDashboardRefreshWorksAfterPanelReturn(t *testing.T) {
	t.Parallel()

	app, dashboard, panel := newRefreshTestStack()
	dashboardCanceled := false
	dashboard.cancel = func() { dashboardCanceled = true }
	_, _ = app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	app.views = app.views[:1]
	_, _ = app.handleDashboardRefresh(dashboardRefreshMsg{generation: 2})
	if dashboard.generation != 3 || !dashboardCanceled || !dashboard.loading["grid"] {
		t.Error("visible dashboard did not resume normal refresh")
	}
	if panel.generation != 5 {
		t.Error("hidden panel refreshed after returning to the grid")
	}
}

func TestDirectPanelDeepLinkStartsRefreshWithoutGridLoad(t *testing.T) {
	t.Parallel()

	app := NewApp(nil, Options{DashboardUID: "fixture", InitialPanelID: 1})
	dashboard := app.views[1].(*dashboardView)
	panel := app.views[2].(*panelView)
	_, _ = app.Update(dashboardLoadedMsg{dashboard: &grafana.Dashboard{
		Refresh: "5m",
		Panels:  []grafana.Panel{{ID: 1, Type: "stat"}},
	}})

	if dashboard.dashboard != nil || len(dashboard.loading) != 0 {
		t.Error("direct panel bootstrap loaded the hidden grid")
	}
	if dashboard.refresh != 5*time.Minute || dashboard.scheduleRefresh() == nil {
		t.Error("direct panel did not start its refresh scheduler")
	}
	if panel.dashboard == nil {
		t.Fatal("panel did not receive the dashboard")
	}

	_, command := app.handlePopView()
	if command == nil {
		t.Fatal("returning to the grid did not initialize it")
	}
	if dashboard.generation != 1 {
		t.Errorf("grid generation = %d, want direct panel timer invalidated", dashboard.generation)
	}
}

func TestDirectPanelDeepLinkRefreshOverrideDisablesTimer(t *testing.T) {
	t.Parallel()

	app := NewApp(nil, Options{
		DashboardUID: "fixture", InitialPanelID: 1, RefreshSet: true, Refresh: 0,
	})
	dashboard := app.views[1].(*dashboardView)
	_, _ = app.Update(dashboardLoadedMsg{dashboard: &grafana.Dashboard{
		Refresh: "5m",
		Panels:  []grafana.Panel{{ID: 1, Type: "stat"}},
	}})
	if dashboard.refresh != 0 || dashboard.scheduleRefresh() != nil {
		t.Error("CLI refresh override was not preserved")
	}
}

func newRefreshTestStack() (*App, *dashboardView, *panelView) {
	dashboard := newDashboardView(nil, "fixture")
	dashboard.dashboard = &grafana.Dashboard{}
	dashboard.instances = []grafana.PanelInstance{{
		Key: "grid", Panel: grafana.Panel{ID: 1, Type: "stat"},
	}}
	dashboard.generation = 2
	dashboard.refresh = time.Minute
	panel := newPanelView(nil, "fixture")
	panel.dashboard = &grafana.Dashboard{}
	panel.panels = []grafana.Panel{{ID: 1, Type: "stat"}}
	panel.variableValues = map[string]string{}
	panel.variablesReady = true
	panel.generation = 4
	panel.loading = false

	return &App{views: []view{dashboard, panel}}, dashboard, panel
}

func executeImmediateCommands(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	message := command()
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{message}
	}
	messages := make([]tea.Msg, 0, len(batch))
	for _, child := range batch {
		messages = append(messages, executeImmediateCommands(child)...)
	}

	return messages
}
