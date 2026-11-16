package grafanatui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

type geoMapRenderer struct{}

type geoPoint struct {
	name   string
	lat    float64
	lon    float64
	value  any
	text   string
	color  string
	marker string
}

func (r *geoMapRenderer) Render(
	result *grafana.QueryResult,
	panel grafana.Panel,
	width, height int,
	_ map[string]bool,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	points := collectGeoPoints(result, panel.FieldConfig)
	if len(points) == 0 {
		return lipgloss.Place(
			width, height, lipgloss.Center, lipgloss.Center,
			"no geospatial data",
		)
	}
	sort.SliceStable(points, func(i, j int) bool {
		if points[i].lat == points[j].lat {
			return points[i].lon < points[j].lon
		}
		return points[i].lat > points[j].lat
	})

	mapHeight := max(min(height-len(points)-1, height-1, 12), 1)
	mapLines := renderGeoGrid(points, width, mapHeight)
	lines := make([]string, 0, height)
	lines = append(lines, mapLines...)
	for _, point := range points {
		if len(lines) >= height {
			break
		}
		line := fmt.Sprintf("%s %-24s %10.4f %11.4f  %s",
			point.marker, point.name, point.lat, point.lon, point.text)
		style := lipgloss.NewStyle()
		if point.color != "" {
			style = style.Foreground(lipgloss.Color(point.color))
		}
		lines = append(lines, style.Render(truncateTableText(line, width)))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = truncateTableText(lines[index], width)
	}

	return strings.Join(lines, "\n")
}

func renderGeoGrid(points []geoPoint, width, height int) []string {
	width = max(width-5, 1)
	grid := make([][]string, height)
	for row := range grid {
		grid[row] = make([]string, width)
		for column := range grid[row] {
			grid[row][column] = "."
		}
	}
	colors := make(map[[2]int]string)
	for _, point := range points {
		x := min(max(int((point.lon+180)/360*float64(width-1)), 0), width-1)
		y := min(max(int((90-point.lat)/180*float64(height-1)), 0), height-1)
		grid[y][x] = point.marker
		colors[[2]int{y, x}] = point.color
	}

	lines := make([]string, 0, height+2)
	for row, cells := range grid {
		latitude := 90 - row*180/max(height-1, 1)
		for column, cell := range cells {
			if color := colors[[2]int{row, column}]; color != "" {
				cells[column] = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(cell)
			}
		}
		lines = append(lines, fmt.Sprintf("%4d %s", latitude, strings.Join(cells, "")))
	}
	lines = append(lines, "     "+geoLongitudeAxis(width))

	return lines
}

func geoLongitudeAxis(width int) string {
	ticks := []struct {
		label string
		lon   float64
	}{
		{label: "-180", lon: -180},
		{label: "-90", lon: -90},
		{label: "0", lon: 0},
		{label: "90", lon: 90},
		{label: "180", lon: 180},
	}
	axis := make([]string, width)
	for index := range axis {
		axis[index] = " "
	}
	for _, tick := range ticks {
		if len(tick.label) > width {
			continue
		}
		position := int((tick.lon + 180) / 360 * float64(width-1))
		position = min(max(position, 0), width-len(tick.label))
		for index, char := range []rune(tick.label) {
			axis[position+index] = string(char)
		}
	}

	return strings.Join(axis, "")
}

func collectGeoPoints(result *grafana.QueryResult, fieldConfig json.RawMessage) []geoPoint {
	points := make([]geoPoint, 0)
	for _, data := range result.Results {
		for _, frame := range data.Frames {
			points = append(points, collectFrameGeoPoints(frame, fieldConfig)...)
		}
	}

	return points
}

func collectFrameGeoPoints(frame grafana.DataFrame, fieldConfig json.RawMessage) []geoPoint {
	latIndex := geoFieldIndex(frame.Schema.Fields, "latitude", "lat")
	lonIndex := geoFieldIndex(frame.Schema.Fields, "longitude", "lon", "lng", "long")
	if latIndex < 0 || lonIndex < 0 {
		return nil
	}
	nameIndex := geoFieldIndex(frame.Schema.Fields, "name", "site", "location", "city", "label")
	valueIndex := geoValueFieldIndex(frame.Schema.Fields, latIndex, lonIndex)
	rows, err := tableRows(frame)
	if err != nil {
		return nil
	}
	points := make([]geoPoint, 0, len(rows))
	for _, row := range rows {
		point, ok := geoPointFromRow(
			frame, row, latIndex, lonIndex, nameIndex, valueIndex, fieldConfig,
		)
		if ok {
			points = append(points, point)
		}
	}

	return points
}

func validGeoCoordinates(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

func geoPointFromRow(
	frame grafana.DataFrame,
	row tableRow,
	latIndex int,
	lonIndex int,
	nameIndex int,
	valueIndex int,
	fieldConfig json.RawMessage,
) (geoPoint, bool) {
	lat, latOK := fieldNumber(row.values[latIndex])
	lon, lonOK := fieldNumber(row.values[lonIndex])
	if !latOK || !lonOK || !validGeoCoordinates(lat, lon) {
		return geoPoint{name: "", lat: 0, lon: 0, value: nil, text: "", color: "", marker: ""}, false
	}
	point := geoPoint{name: "point", lat: lat, lon: lon, value: nil, text: "", color: "", marker: "◆"}
	if nameIndex >= 0 {
		if name := fmt.Sprint(row.values[nameIndex]); name != "" && name != "<nil>" {
			point.name = name
		}
	} else {
		point.name = geoPointName(frame)
	}
	if valueIndex >= 0 {
		presentation := presentField(fieldConfig, frame.Schema.Fields[valueIndex], row.values[valueIndex])
		point.value = row.values[valueIndex]
		point.text = presentation.Text
		point.color = presentation.Color
	}

	return point, true
}

func geoFieldIndex(fields []grafana.DataFrameField, names ...string) int {
	for index, field := range fields {
		for _, name := range names {
			if strings.EqualFold(field.Name, name) {
				return index
			}
		}
	}

	return -1
}

func geoValueFieldIndex(fields []grafana.DataFrameField, latIndex, lonIndex int) int {
	for index, field := range fields {
		if index != latIndex && index != lonIndex && field.Type == fieldTypeNumber {
			return index
		}
	}

	return -1
}

func geoPointName(frame grafana.DataFrame) string {
	for _, field := range frame.Schema.Fields {
		for _, key := range []string{labelName, "site", "location", "city", labelCluster, labelInstance, labelHost} {
			if value, ok := field.Labels[key]; ok && value != "" {
				return value
			}
		}
	}

	return "point"
}
