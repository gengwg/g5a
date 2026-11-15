//nolint:exhaustruct // barchart fixtures vary relevant fields only
package grafanatui_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
)

func TestBarChartRendererUsesVendorLabels(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{
			vendorBarFrame("Supermicro", 717),
			vendorBarFrame("Dell Inc.", 366),
			vendorBarFrame("Lenovo", 149),
		}},
	}}

	output := grafanatui.RendererForPanel("barchart").Render(result, grafana.Panel{}, 100, 20, nil)
	for _, expected := range []string{"Supermicro", "Dell Inc.", "Lenovo", "717"} {
		if !strings.Contains(output, expected) {
			t.Errorf("barchart does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "Value{") {
		t.Errorf("barchart exposes raw series keys instead of categories:\n%s", output)
	}
}

func TestBarChartRendererQuotedAndDuplicateLabels(t *testing.T) {
	t.Parallel()

	first := vendorBarFrame("Dell, Inc.", 1)
	first.Schema.Fields[1].Labels["instance"] = "a"
	second := vendorBarFrame("Dell, Inc.", 2)
	second.Schema.Fields[1].Labels["instance"] = "b"
	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{first, second}},
	}}

	output := grafanatui.RendererForPanel("barchart").Render(result, grafana.Panel{}, 100, 20, nil)
	if !strings.Contains(output, "Dell, Inc.") {
		t.Errorf("quoted comma value was truncated:\n%s", output)
	}
	if !strings.Contains(output, "#2") {
		t.Errorf("duplicate compact category was not disambiguated:\n%s", output)
	}
}

func vendorBarFrame(vendor string, value float64) grafana.DataFrame {
	return grafana.DataFrame{
		Schema: grafana.DataFrameSchema{
			RefID: "A",
			Fields: []grafana.DataFrameField{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"vendor": vendor}},
			},
		},
		Data: grafana.DataFrameData{Values: []json.RawMessage{
			json.RawMessage(`[1700000000000]`), mustMarshalJSON([]float64{value}),
		}},
	}
}
