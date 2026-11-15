package grafanatui

import (
	"encoding/json"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

// statRenderer renders numeric values as large centered displays.
type statRenderer struct{}

type statValue struct {
	presentation fieldPresentation
}

type statPanelOptions struct {
	ColorMode string `json:"colorMode"`
	TextMode  string `json:"textMode"`
	Reduce    struct {
		Calcs []string `json:"calcs"`
	} `json:"reduceOptions"`
}

func (r *statRenderer) Render(
	result *grafana.QueryResult,
	panel grafana.Panel,
	width, height int,
	_ map[string]bool,
) string {
	var options statPanelOptions
	_ = json.Unmarshal(panel.Options, &options)
	values := collectStatValues(result, panel, options)
	if len(values) == 0 {
		presentation := presentField(
			panel.FieldConfig,
			grafana.DataFrameField{Name: fieldNameValue, Type: fieldTypeNumber, Labels: nil, Config: nil},
			nil,
		)
		text := presentation.Text
		if text == "" {
			text = "no data"
		}
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, text)
	}

	if len(values) == 1 {
		return renderStatValue(values[0], options, width, height)
	}

	perRow := max(width/18, 1)
	blocks := make([]string, 0, len(values))
	for _, value := range values {
		blocks = append(blocks, renderStatValue(
			value,
			options,
			width/perRow,
			height,
		))
	}

	rows := make([]string, 0, (len(blocks)+perRow-1)/perRow)
	for start := 0; start < len(blocks); start += perRow {
		end := min(start+perRow, len(blocks))
		rows = append(rows, lipgloss.JoinHorizontal(0, blocks[start:end]...))
	}

	return strings.Join(rows, "\n")
}

func collectStatValues(
	result *grafana.QueryResult,
	panel grafana.Panel,
	options statPanelOptions,
) []statValue {
	refIDs := make([]string, 0, len(result.Results))
	for refID := range result.Results {
		refIDs = append(refIDs, refID)
	}
	slices.Sort(refIDs)

	values := make([]statValue, 0)
	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			for fieldIndex, field := range frame.Schema.Fields {
				if field.Type != fieldTypeNumber || fieldIndex >= len(frame.Data.Values) {
					continue
				}
				column, err := frame.NumericColumn(fieldIndex)
				if err != nil || len(column) == 0 {
					continue
				}
				value := reduceStatColumn(column, options.Reduce.Calcs)
				if math.IsNaN(value) {
					continue
				}
				values = append(values, statValue{
					presentation: presentField(panel.FieldConfig, field, value),
				})
			}
		}
	}

	return values
}

//nolint:cyclop,gocognit,gocyclo,funlen // reducer dispatch follows Grafana calculation names
func reduceStatColumn(values []float64, calculations []string) float64 {
	calculation := calcLastNotNull
	if len(calculations) > 0 {
		calculation = calculations[0]
	}

	switch calculation {
	case calcFirst:
		if len(values) > 0 {
			return values[0]
		}
	case calcFirstNotNull:
		for _, value := range values {
			if !math.IsNaN(value) {
				return value
			}
		}
	case calcLast:
		if len(values) > 0 {
			return values[len(values)-1]
		}
	case calcMin:
		return reduceStatExtreme(values, math.Inf(1), false)
	case calcMax:
		return reduceStatExtreme(values, math.Inf(-1), true)
	case calcMean:
		var sum float64
		count := 0
		for _, value := range values {
			if !math.IsNaN(value) {
				sum += value
				count++
			}
		}
		if count > 0 {
			return sum / float64(count)
		}
	case calcSum:
		var sum float64
		for _, value := range values {
			if !math.IsNaN(value) {
				sum += value
			}
		}
		return sum
	case calcCount:
		count := 0
		for _, value := range values {
			if !math.IsNaN(value) {
				count++
			}
		}
		return float64(count)
	case calcLastNotNull:
		for index := range slices.Backward(values) {
			if !math.IsNaN(values[index]) {
				return values[index]
			}
		}
	}

	return math.NaN()
}

func supportedStatCalculation(calculation string) bool {
	switch calculation {
	case calcFirst, calcFirstNotNull, calcLast, calcLastNotNull,
		calcMin, calcMax, calcMean, calcSum, calcCount:
		return true
	default:
		return false
	}
}

func reduceStatExtreme(values []float64, initial float64, maximum bool) float64 {
	result := initial
	found := false
	for _, value := range values {
		if math.IsNaN(value) {
			continue
		}
		if !found || (maximum && value > result) || (!maximum && value < result) {
			result = value
			found = true
		}
	}
	if !found {
		return math.NaN()
	}

	return result
}

func renderStatValue(
	value statValue,
	options statPanelOptions,
	width int,
	height int,
) string {
	presentation := value.presentation
	showName := options.TextMode == "value_and_name" || options.TextMode == labelName
	showValue := options.TextMode != labelName
	lines := make([]string, 0, 2)
	if showName && presentation.DisplayName != "" {
		lines = append(lines, presentation.DisplayName)
	}
	if showValue {
		lines = append(lines, presentation.Text)
	}
	content := strings.Join(lines, "\n")
	body := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)

	style := lipgloss.NewStyle().Bold(true).Width(width).Height(height)
	if presentation.Color != "" {
		if options.ColorMode == "background" {
			style = style.Background(lipgloss.Color(presentation.Color)).
				Foreground(lipgloss.Color(contrastColor(presentation.Color)))
		} else {
			style = style.Foreground(lipgloss.Color(presentation.Color))
		}
	} else {
		style = style.Foreground(lipgloss.Color("#00FFFF"))
	}

	return style.Render(body)
}

func contrastColor(background string) string {
	if len(background) != 7 || background[0] != '#' {
		return colorWhite
	}
	red, errRed := strconv.ParseUint(background[1:3], 16, 8)
	green, errGreen := strconv.ParseUint(background[3:5], 16, 8)
	blue, errBlue := strconv.ParseUint(background[5:7], 16, 8)
	if errRed != nil || errGreen != nil || errBlue != nil {
		return colorWhite
	}
	luminance := 0.299*float64(red) + 0.587*float64(green) + 0.114*float64(blue)
	if luminance > 150 {
		return "#111111"
	}

	return colorWhite
}

// extractAllNumericValues is shared by the gauge, bar, and pie renderers.
func extractAllNumericValues(result *grafana.QueryResult) map[string]float64 {
	values := make(map[string]float64)
	for _, resultData := range result.Results {
		for _, frame := range resultData.Frames {
			extractFrameValues(&frame, values)
		}
	}

	return values
}

func extractFrameValues(
	frame *grafana.DataFrame,
	values map[string]float64,
) {
	numericValues, err := frame.NumericValues()
	if err == nil && len(numericValues) > 0 {
		maps.Copy(values, numericValues)
		return
	}

	_, series, timeSeriesErr := frame.TimeSeries()
	if timeSeriesErr != nil {
		return
	}
	for name, seriesValues := range series {
		if len(seriesValues) > 0 {
			values[name] = seriesValues[len(seriesValues)-1]
		}
	}
}
