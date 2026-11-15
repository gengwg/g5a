package grafanatui

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

const (
	legendLines        = 2
	legendNameMaxWidth = 40
)

// timeSeriesRenderer renders time series data as a braille line chart.
type timeSeriesRenderer struct{}

type timeSeriesOptions struct {
	Legend struct {
		Calcs       []string `json:"calcs"`
		Placement   string   `json:"placement"`
		DisplayMode string   `json:"displayMode"`
	} `json:"legend"`
}

func (r *timeSeriesRenderer) Render(
	result *grafana.QueryResult,
	panel grafana.Panel,
	width, height int,
	hiddenSeries map[string]bool,
) (rendered string) {
	if width <= 0 || height <= 0 {
		return ""
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			rendered = lipgloss.Place(
				width, height,
				lipgloss.Center, lipgloss.Center,
				fmt.Sprintf("chart rendering error: %v", recovered),
			)
		}
	}()

	seriesData := collectTimeSeries(result, panel.FieldConfig)
	filterHiddenSeries(seriesData, hiddenSeries)
	if len(seriesData) == 0 {
		return lipgloss.Place(
			width, height,
			lipgloss.Center, lipgloss.Center,
			"no time series data",
		)
	}

	var options timeSeriesOptions
	_ = json.Unmarshal(panel.Options, &options)
	seriesNames := sortedSeriesNames(seriesData)
	rightLegend := options.Legend.Placement == directionRight && width >= 50
	chartWidth := width
	chartHeight := max(height-legendLines, 1)
	if rightLegend {
		chartWidth = max(width*2/3, 20)
		chartHeight = height
	}
	if chartHeight < 3 {
		return renderTinyTimeSeries(seriesData, seriesNames, width, height)
	}

	chart := timeserieslinechart.New(chartWidth, chartHeight,
		timeserieslinechart.WithXLabelFormatter(smartTimeLabelFormatter(seriesData)),
		timeserieslinechart.WithYLabelFormatter(unitLabelFormatter(ExtractUnit(panel.FieldConfig))),
	)
	pointCount := pushTimeSeriesData(&chart, seriesData, seriesNames)
	if pointCount == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, "no time series data")
	}
	styleTimeSeriesDataSets(&chart, seriesNames)
	chart.DrawBrailleAll()

	if rightLegend {
		legend := buildCalculatedLegend(
			seriesNames,
			seriesData,
			width-chartWidth,
			height,
			options.Legend.Calcs,
		)
		return lipgloss.JoinHorizontal(0, chart.View(), legend)
	}

	legend := buildLegendWithCalculations(
		seriesNames,
		seriesData,
		width,
		options.Legend.Calcs,
	)

	return chart.View() + "\n" + legend
}

// smartTimeLabelFormatter shows hours for short ranges and dates for long ranges.
func smartTimeLabelFormatter(
	seriesData map[string]timeSeriesEntry,
) linechart.LabelFormatter {
	rangeMs := timeRangeMs(seriesData)
	dayMs := int64(24 * 60 * 60 * 1000)
	if rangeMs <= dayMs {
		return func(_ int, value float64) string {
			return time.Unix(int64(value), 0).In(time.Local).Format("15:04") //nolint:gosmopolitan // user-local dashboard time
		}
	}

	return func(_ int, value float64) string {
		//nolint:gosmopolitan // user-local dashboard time
		return time.Unix(int64(value), 0).In(time.Local).Format("Jan 02 15:04")
	}
}

func timeRangeMs(seriesData map[string]timeSeriesEntry) int64 {
	var minTimestamp, maxTimestamp int64
	first := true
	for _, entry := range seriesData {
		if len(entry.timestamps) == 0 {
			continue
		}
		low := entry.timestamps[0]
		high := entry.timestamps[len(entry.timestamps)-1]
		if first || low < minTimestamp {
			minTimestamp = low
		}
		if first || high > maxTimestamp {
			maxTimestamp = high
		}
		first = false
	}

	return maxTimestamp - minTimestamp
}

func unitLabelFormatter(unit string) linechart.LabelFormatter {
	return func(_ int, value float64) string {
		if unit != "" {
			return FormatValue(value, unit)
		}

		return fmt.Sprintf("%.2g", value)
	}
}

// timeSeriesEntry holds timestamps, values, and presentation for one series.
type timeSeriesEntry struct {
	timestamps  []int64
	values      []float64
	field       grafana.DataFrameField
	fieldConfig json.RawMessage
}

func collectTimeSeries(
	result *grafana.QueryResult,
	fieldConfig json.RawMessage,
) map[string]timeSeriesEntry {
	seriesData := make(map[string]timeSeriesEntry)
	refIDs := make([]string, 0, len(result.Results))
	for refID := range result.Results {
		refIDs = append(refIDs, refID)
	}
	slices.Sort(refIDs)

	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			timestamps, series, err := frame.TimeSeries()
			if err != nil {
				continue
			}
			for _, field := range frame.Schema.Fields {
				if field.Type != fieldTypeNumber {
					continue
				}
				name := presentField(fieldConfig, field, nil).DisplayName
				values, ok := series[formatFrameFieldKey(field)]
				if !ok || allNaN(values) {
					continue
				}
				name = uniqueSeriesName(seriesData, name, refID)
				seriesData[name] = timeSeriesEntry{
					timestamps:  timestamps,
					values:      values,
					field:       field,
					fieldConfig: fieldConfig,
				}
			}
		}
	}

	return seriesData
}

// formatFrameFieldKey mirrors DataFrame.TimeSeries's label-aware key format.
func formatFrameFieldKey(field grafana.DataFrameField) string {
	if len(field.Labels) == 0 {
		return field.Name
	}
	keys := make([]string, 0, len(field.Labels))
	for key := range field.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+field.Labels[key])
	}

	return field.Name + "{" + strings.Join(pairs, ",") + "}"
}

func uniqueSeriesName(
	seriesData map[string]timeSeriesEntry,
	name string,
	refID string,
) string {
	if _, exists := seriesData[name]; !exists {
		return name
	}
	candidate := name + " #" + refID
	if _, exists := seriesData[candidate]; !exists {
		return candidate
	}
	for suffix := 2; ; suffix++ {
		candidate = fmt.Sprintf("%s #%s-%d", name, refID, suffix)
		if _, exists := seriesData[candidate]; !exists {
			return candidate
		}
	}
}

func allNaN(values []float64) bool {
	for _, value := range values {
		if !math.IsNaN(value) {
			return false
		}
	}

	return true
}

func filterHiddenSeries(
	seriesData map[string]timeSeriesEntry,
	hidden map[string]bool,
) {
	for name := range seriesData {
		if hidden[name] {
			delete(seriesData, name)
		}
	}
}

func sortedSeriesNames(seriesData map[string]timeSeriesEntry) []string {
	names := make([]string, 0, len(seriesData))
	for name := range seriesData {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func pushTimeSeriesData(
	chart *timeserieslinechart.Model,
	seriesData map[string]timeSeriesEntry,
	seriesNames []string,
) int {
	count := 0
	for _, name := range seriesNames {
		entry := seriesData[name]
		pointCount := min(len(entry.timestamps), len(entry.values))
		for index := range pointCount {
			if math.IsNaN(entry.values[index]) {
				continue
			}
			chart.PushDataSet(name, timeserieslinechart.TimePoint{
				Time:  time.UnixMilli(entry.timestamps[index]),
				Value: entry.values[index],
			})
			count++
		}
	}

	return count
}

func styleTimeSeriesDataSets(
	chart *timeserieslinechart.Model,
	seriesNames []string,
) {
	for index, name := range seriesNames {
		style := lipgloss.NewStyle().Foreground(chartColor(index))
		chart.SetDataSetStyle(name, style)
		chart.SetDataSetLineStyle(name, runes.ThinLineStyle)
	}
}

func compactLegendName(name string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(name) <= width {
		return name
	}
	name = trimLegendScheme(name)
	if lipgloss.Width(name) <= width {
		return name
	}
	if width == 1 {
		return "…"
	}

	runes := []rune(name)
	left := (width - 1) / 2
	right := width - 1 - left
	for left > 0 {
		if left+right < len(runes) {
			candidate := string(runes[:left]) + "…" + string(runes[len(runes)-right:])
			if lipgloss.Width(candidate) <= width {
				return candidate
			}
		}
		left--
		right = min(right+1, len(runes))
	}

	return truncateTableText(name, width)
}

func trimLegendScheme(name string) string {
	parsed, err := url.Parse(name)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return name
	}

	return strings.TrimPrefix(name, parsed.Scheme+"://")
}

func buildLegendWithCalculations(
	seriesNames []string,
	seriesData map[string]timeSeriesEntry,
	maxWidth int,
	calculations []string,
) string {
	parts := make([]string, 0, len(seriesNames))
	for index, name := range seriesNames {
		style := lipgloss.NewStyle().Foreground(chartColor(index))
		name = compactLegendName(name, min(maxWidth, legendNameMaxWidth))
		text := "■ " + name + legendCalculations(seriesData[name], calculations)
		parts = append(parts, style.Render(text))
	}

	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(strings.Join(parts, "  "))
}

func buildCalculatedLegend(
	seriesNames []string,
	seriesData map[string]timeSeriesEntry,
	width int,
	height int,
	calculations []string,
) string {
	lines := make([]string, 0, height)
	if len(calculations) > 0 {
		labels := make([]string, 0, len(calculations))
		for _, calculation := range calculations {
			labels = append(labels, legendCalculationLabel(calculation))
		}
		lines = append(lines, truncateTableText("Name  "+strings.Join(labels, "  "), width))
	}
	for index, name := range seriesNames {
		if len(lines) >= height {
			break
		}
		style := lipgloss.NewStyle().Foreground(chartColor(index))
		calculationText := legendCalculationValues(seriesData[name], calculations)
		nameWidth := min(width-3-lipgloss.Width(calculationText), legendNameMaxWidth)
		name = compactLegendName(name, nameWidth)
		text := "■ " + name + calculationText
		lines = append(lines, style.Render(truncateTableText(text, max(width-1, 1))))
	}

	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func legendCalculationValues(entry timeSeriesEntry, calculations []string) string {
	values := make([]string, 0, len(calculations))
	for _, calculation := range calculations {
		if !supportedStatCalculation(calculation) {
			values = append(values, "unsupported")
			continue
		}
		value := reduceStatColumn(entry.values, []string{calculation})
		if math.IsNaN(value) || math.IsInf(value, 0) {
			values = append(values, "-")
			continue
		}
		presentation := presentField(entry.fieldConfig, entry.field, value)
		values = append(values, presentation.Text)
	}
	if len(values) == 0 {
		return ""
	}

	return "  " + strings.Join(values, "  ")
}

func renderTinyTimeSeries(
	seriesData map[string]timeSeriesEntry,
	seriesNames []string,
	width int,
	height int,
) string {
	if len(seriesNames) == 0 || width <= 0 || height <= 0 {
		return ""
	}
	entry := seriesData[seriesNames[0]]
	values := make([]float64, 0, len(entry.values))
	for _, value := range entry.values {
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return "no time series data"
	}

	last := values[len(values)-1]
	valueText := presentField(entry.fieldConfig, entry.field, last).Text
	chartWidth := max(width-len(valueText)-2, 1)
	chart := sparkline(values, chartWidth)
	line := truncateTableText(chart+" "+valueText, width)
	if height > 1 {
		line += "\n" + compactLegendName(seriesNames[0], width)
	}

	return line
}

func sparkline(values []float64, width int) string {
	if width <= 0 || len(values) == 0 {
		return ""
	}
	points := make([]float64, width)
	for index := range points {
		start := index * len(values) / width
		end := (index + 1) * len(values) / width
		if end <= start {
			end = start + 1
		}
		minValue, maxValue := values[start], values[start]
		for _, value := range values[start:end] {
			minValue = min(minValue, value)
			maxValue = max(maxValue, value)
		}
		points[index] = (minValue + maxValue) / 2
	}

	minPoint, maxPoint := points[0], points[0]
	for _, point := range points {
		minPoint = min(minPoint, point)
		maxPoint = max(maxPoint, point)
	}
	levels := []rune("▁▂▃▄▅▆▇█")
	result := make([]rune, len(points))
	for index, point := range points {
		var level int
		if maxPoint > minPoint {
			level = int((point - minPoint) / (maxPoint - minPoint) * float64(len(levels)-1))
		} else {
			level = len(levels) / 2
		}
		result[index] = levels[level]
	}

	return string(result)
}

//nolint:cyclop // calculation labels follow Grafana reducer names
func legendCalculationLabel(calculation string) string {
	switch calculation {
	case "lastNotNull":
		return "Last*"
	case "firstNotNull":
		return "First*"
	case calcLast:
		return "Last"
	case calcFirst:
		return "First"
	case calcMin:
		return "Min"
	case calcMax:
		return "Max"
	case calcMean:
		return "Mean"
	case calcSum:
		return "Sum"
	case calcCount:
		return "Count"
	default:
		return calculation
	}
}

func legendCalculations(entry timeSeriesEntry, calculations []string) string {
	if len(calculations) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calculations))
	for _, calculation := range calculations {
		if !supportedStatCalculation(calculation) {
			parts = append(parts, calculation+"=unsupported")
			continue
		}

		value := reduceStatColumn(entry.values, []string{calculation})
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		presentation := presentField(entry.fieldConfig, entry.field, value)
		parts = append(parts, legendCalculationLabel(calculation)+"="+presentation.Text)
	}
	if len(parts) == 0 {
		return ""
	}

	return "  " + strings.Join(parts, " ")
}
