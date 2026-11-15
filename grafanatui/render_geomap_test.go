//nolint:exhaustruct // geomap fixtures vary relevant fields only
package grafanatui_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
)

func TestGeoMapRenderer(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "site", Type: "string"},
				{Name: "latitude", Type: "number"},
				{Name: "longitude", Type: "number"},
				{Name: "temperature", Type: "number"},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`["San Francisco","Berlin"]`),
				json.RawMessage(`[37.7749,52.52]`),
				json.RawMessage(`[-122.4194,13.405]`),
				json.RawMessage(`[18,22]`),
			}},
		}}},
	}}

	output := grafanatui.RendererForPanel("geomap").Render(result, grafana.Panel{}, 100, 15, nil)
	for _, expected := range []string{
		"San Francisco", "Berlin", "37.7749", "52.5200", "-122.4194", "13.4050", "-180", "90",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("geomap output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestGeoMapRendererAliasesAndLabels(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "lat", Type: "number"},
				{Name: "lng", Type: "number"},
				{Name: "Value", Type: "number", Labels: map[string]string{"site": "Reykjavik"}},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`[64.1466]`),
				json.RawMessage(`[-21.9426]`),
				json.RawMessage(`[3]`),
			}},
		}}},
	}}

	output := grafanatui.RendererForPanel("geomap").Render(result, grafana.Panel{}, 80, 12, nil)
	if !strings.Contains(output, "Reykjavik") {
		t.Errorf("geomap does not contain label-derived site name:\n%s", output)
	}
}

func TestGeoMapRendererHonorsTinyDimensions(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{
		"A": {Frames: []grafana.DataFrame{{
			Schema: grafana.DataFrameSchema{Fields: []grafana.DataFrameField{
				{Name: "site", Type: "string"},
				{Name: "latitude", Type: "number"},
				{Name: "longitude", Type: "number"},
			}},
			Data: grafana.DataFrameData{Values: []json.RawMessage{
				json.RawMessage(`["San Francisco","Berlin"]`),
				json.RawMessage(`[37.7749,52.52]`),
				json.RawMessage(`[-122.4194,13.405]`),
			}},
		}}},
	}}

	output := grafanatui.RendererForPanel("geomap").Render(result, grafana.Panel{}, 20, 4, nil)
	lines := strings.Split(output, "\n")
	if len(lines) > 4 {
		t.Fatalf("output has %d lines, want at most 4", len(lines))
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 20 {
			t.Errorf("line exceeds width 20: %q", line)
		}
	}
}

func TestGeoMapRendererNoData(t *testing.T) {
	t.Parallel()

	result := &grafana.QueryResult{Results: map[string]grafana.QueryResultData{}}
	output := grafanatui.RendererForPanel("geomap").Render(result, grafana.Panel{}, 60, 5, nil)
	if !strings.Contains(output, "no geospatial data") {
		t.Errorf("missing no-data message: %q", output)
	}
}
