//nolint:exhaustruct // gauge fixtures vary relevant fields only
package grafanatui_test

import (
	"strings"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
)

func TestBarGaugeRendererUsesVendorLabels(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{
			vendorBarFrame("Supermicro", 717),
			vendorBarFrame("Dell Inc.", 366),
			vendorBarFrame("Lenovo", 149),
		}},
	}}

	output := grafanatui.RendererForPanel("bargauge").Render(result, grafana.Panel{}, 100, 20, nil)
	for _, expected := range []string{"Supermicro", "Dell Inc.", "Lenovo", "717"} {
		if !strings.Contains(output, expected) {
			t.Errorf("bar gauge does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "Value") {
		t.Errorf("bar gauge uses the generic numeric field name instead of labels:\n%s", output)
	}
}
