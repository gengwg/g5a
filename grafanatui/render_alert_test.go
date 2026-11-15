//nolint:exhaustruct // alert fixtures intentionally omit irrelevant fields
package grafanatui_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
)

func TestAlertListRendererListAndInstances(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Alerts: []grafana.AlertRule{{
		Name: "CPU high", State: "firing", Folder: "Operations", ActiveAt: time.Now(),
		Instances: []grafana.AlertInstance{{
			State: "firing", Labels: map[string]string{"severity": "critical"}, ActiveAt: time.Now(),
		}},
	}}}
	panel := grafana.Panel{Options: json.RawMessage(`{"showInstances":true}`)}
	output := grafanatui.RendererForPanel("alertlist").Render(result, panel, 100, 5, nil)
	for _, expected := range []string{"FIRING", "CPU high", "1 instance(s)", "Operations", "severity=\"critical\""} {
		if !strings.Contains(output, expected) {
			t.Errorf("alert list does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "not implemented") {
		t.Fatalf("alert-list placeholder remains:\n%s", output)
	}
}

func TestAlertListRendererStatCountsBeforeMaxItems(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Alerts: []grafana.AlertRule{
		{Name: "A", State: "firing"},
		{Name: "B", State: "pending"},
	}}
	panel := grafana.Panel{Options: json.RawMessage(`{"viewMode":"stat","maxItems":1}`)}
	output := grafanatui.RendererForPanel("alertlist").Render(result, panel, 40, 5, nil)
	if !strings.Contains(output, "2 alert rules") {
		t.Errorf("stat count = %q, want 2 alert rules", output)
	}
}

func TestAlertListRendererEmpty(t *testing.T) {
	t.Parallel()

	output := grafanatui.RendererForPanel("alertlist").Render(nil, grafana.Panel{}, 40, 5, nil)
	if !strings.Contains(output, "No alerts matching filters") {
		t.Errorf("empty alert list = %q", output)
	}
}
