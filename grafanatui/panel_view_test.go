//nolint:exhaustruct,testpackage // internal view state and fixtures need direct access
package grafanatui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gengwg/g5a/grafana"
)

//nolint:cyclop // end-to-end dependent-variable lifecycle assertions
func TestPanelViewReResolvesDependentVariables(t *testing.T) {
	t.Parallel()

	client, queries := newDependentVariableClient(t)
	view := newPanelView(client, "fixture")
	view.dashboard = dependentVariableDashboard(t)
	view.panels = grafana.FlattenPanels(view.dashboard.Panels)
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
		t.Fatal("resolved variables did not schedule a panel query")
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

	view.Update(command())
	panelQueries := queries.forRef("A")
	if len(panelQueries) != 1 || panelQueries[0] != `up{instance="dev-a"}` {
		t.Errorf("panel queries = %v, want refreshed dependent selection", panelQueries)
	}
}

func TestPanelViewAdoptsValidatedInitialVariables(t *testing.T) {
	t.Parallel()

	client, _ := newDependentVariableClient(t)
	view := newPanelView(client, "fixture")
	view.dashboard = dependentVariableDashboard(t)
	view.panels = grafana.FlattenPanels(view.dashboard.Panels)
	view.initialVariables = map[string]string{"env": "dev", "instance": "prod-a"}

	command := view.resolveVariables()
	resolved, ok := command().(variablesResolvedMsg)
	if !ok {
		t.Fatal("initial overrides did not return a resolution message")
	}
	_, query := view.Update(resolved)
	if query == nil {
		t.Fatal("resolved initial variables did not schedule a panel query")
	}

	if got := view.variableValues["env"]; got != "dev" {
		t.Errorf("initial independent override = %q, want dev", got)
	}
	if got := view.variableValues["instance"]; got != "dev-a" {
		t.Errorf("initial dependent override = %q, want validated default dev-a", got)
	}
	if view.initialVariables != nil {
		t.Error("initial overrides were not consumed")
	}
}

func TestPanelViewChangeVariablesRejectsInFlightQuery(t *testing.T) {
	t.Parallel()

	view := newPanelView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Fixture"}
	view.panels = []grafana.Panel{{ID: 1, Type: "stat", Title: "Panel"}}
	view.variableValues = map[string]string{"instance": "old"}
	view.generation = 4
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

	view.Update(panelDataMsg{generation: 4, result: numericResult(42)})
	if view.data != nil {
		t.Fatal("old query result was installed after the selection changed")
	}
	if !view.loading {
		t.Error("stale query result cleared loading for the pending resolution")
	}
	if output := view.renderContent(80, 4); strings.Contains(output, "42") {
		t.Errorf("old query result was displayed under the new selection:\n%s", output)
	}
}

func TestPanelViewKeyGuardWaitsForVariables(t *testing.T) {
	t.Parallel()

	view := newPanelView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{}
	view.panels = []grafana.Panel{{ID: 1}, {ID: 2}}
	view.variableValues = map[string]string{"instance": "current"}
	view.variablesReady = false
	view.data = numericResult(42)
	view.loading = false
	_, command := view.handleKeyMsgGuarded(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if command != nil || view.index != 0 || view.data == nil || view.loading {
		t.Error("navigation changed panel state while variables were not ready")
	}
}

func TestPanelViewRetriesVariableResolution(t *testing.T) {
	t.Parallel()

	client, _ := newDependentVariableClient(t)
	view := newPanelView(client, "fixture")
	view.dashboard = dependentVariableDashboard(t)
	view.panels = grafana.FlattenPanels(view.dashboard.Panels)
	view.variableValues = map[string]string{"env": "prod", "instance": "prod-a"}
	view.variableGeneration = 1
	view.variablesReady = false
	view.err = errors.New("resolution failed")

	_, command := view.handleKeyMsgGuarded(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command == nil || view.err != nil || !view.loading {
		t.Fatal("manual retry did not restart variable resolution")
	}
	resolved, ok := command().(variablesResolvedMsg)
	if !ok {
		t.Fatal("retry did not resolve variables")
	}
	_, command = view.Update(resolved)
	if command == nil || !view.variablesReady || view.err != nil {
		t.Error("panel did not recover after successful variable resolution")
	}
}

func TestPanelViewRejectsStaleVariables(t *testing.T) {
	t.Parallel()

	view := newPanelView(nil, "fixture")
	view.dashboard = &grafana.Dashboard{Title: "Fixture"}
	view.panels = []grafana.Panel{{ID: 1}}
	view.generation = 3
	view.initialVariables = map[string]string{"instance": "current"}
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
		t.Error("stale variable resolution scheduled a query")
	}
	if view.generation != 4 {
		t.Errorf("stale resolution changed panel query generation to %d", view.generation)
	}
	if view.variableValues != nil {
		t.Errorf("stale resolution installed values %v", view.variableValues)
	}
	if got := view.initialVariables["instance"]; got != "current" {
		t.Errorf("stale resolution replaced initial override with %q", got)
	}
	if got := view.variableOptions["instance"][0]; got != "current" {
		t.Errorf("stale options replaced current option with %q", got)
	}
}

//nolint:cyclop // covers current and stale variable-resolution failures
func TestPanelViewVariableResolutionErrorLifecycle(t *testing.T) {
	t.Parallel()

	view := newPanelView(newFlakyVariableClient(t), "fixture")
	view.dashboard = datasourceVariableDashboard()
	view.panels = []grafana.Panel{{ID: 1, Type: "stat", Title: "Panel"}}
	view.loading = true

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
	if view.loading {
		t.Error("panel remained loading after resolution failed")
	}

	_, command = view.Update(setVariablesMsg{
		values: map[string]string{"datasource": "prom1"},
	})
	resolved, ok := command().(variablesResolvedMsg)
	if !ok {
		t.Fatal("retry did not resolve variables successfully")
	}
	_, command = view.Update(resolved)
	if command == nil {
		t.Fatal("successful retry did not schedule a panel query")
	}
	view.Update(command())
	if view.err != nil || view.loading || view.data == nil {
		t.Fatalf("retry did not reach healthy state: error=%v loading=%v data=%v",
			view.err, view.loading, view.data)
	}
	healthy := view.data

	view.Update(failure)
	if view.err != nil {
		t.Errorf("stale failure replaced healthy state with %v", view.err)
	}
	if view.loading {
		t.Error("stale failure changed healthy loading state")
	}
	if view.data != healthy {
		t.Error("stale failure replaced healthy panel data")
	}
}

func TestPanelViewTimeChangeInvalidatesInflightQuery(t *testing.T) {
	t.Parallel()

	view := newPanelView(nil, "fixture")
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

	view.handlePanelResult(panelDataMsg{generation: 4, result: numericResult(1)})
	if view.data != nil {
		t.Fatal("old time-range result was accepted after invalidation")
	}
}

func TestPanelViewDeadlineExceededRemainsVisible(t *testing.T) {
	t.Parallel()

	view := newPanelView(nil, "fixture")
	view.generation = 1
	_, _ = view.handlePanelResult(panelErrorMsg{
		generation: 1,
		err:        context.DeadlineExceeded,
	})

	if !errors.Is(view.err, context.DeadlineExceeded) {
		t.Fatalf("panel error = %v, want context deadline exceeded", view.err)
	}
	output := view.renderContent(80, 4)
	if !strings.Contains(output, context.DeadlineExceeded.Error()) {
		t.Errorf("deadline error is not visible:\n%s", output)
	}
	if strings.Contains(output, "No data") {
		t.Errorf("deadline was incorrectly rendered as no data:\n%s", output)
	}
}
