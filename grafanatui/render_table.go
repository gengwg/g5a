package grafanatui

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

const (
	tableColumnPadding = 2
	tableMinColWidth   = 8
)

type tableRenderer struct{}

type tableOptions struct {
	ShowHeader *bool `json:"showHeader"`
	SortBy     []struct {
		DisplayName string `json:"displayName"`
		Desc        bool   `json:"desc"`
	} `json:"sortBy"`
}

type tableColumn struct {
	field  grafana.DataFrameField
	header string
	width  int
	align  string
}

type tableRow struct {
	values []any
}

type tableFrameData struct {
	columns []tableColumn
	rows    []tableRow
}

type tableColumnMapping struct {
	output int
	input  int
}

func (r *tableRenderer) Render(
	result *grafana.QueryResult,
	panel grafana.Panel,
	width int,
	height int,
	hiddenSeries map[string]bool,
) string {
	if isGaugeDisplayMode(panel.FieldConfig) {
		gauge := &gaugeRenderer{}
		return gauge.Render(result, panel, width, height, hiddenSeries)
	}

	columns, rows := collectTableData(result, panel.FieldConfig)
	if len(columns) == 0 {
		return lipgloss.Place(
			width,
			height,
			lipgloss.Center,
			lipgloss.Center,
			"no table data",
		)
	}

	var options tableOptions
	_ = json.Unmarshal(panel.Options, &options)
	sortTableRows(rows, columns, options)

	totalRowCount := len(rows)
	visibleRows := rows[:min(len(rows), max(height, 0))]
	presented := presentTableRows(visibleRows, columns, panel.FieldConfig)
	colWidths := calculateColumnWidths(columns, presented, width)
	lines := renderTableLines(
		columns,
		presented,
		colWidths,
		height,
		totalRowCount,
		options.ShowHeader == nil || *options.ShowHeader,
	)

	return strings.Join(lines, "\n")
}

func collectTableData(
	result *grafana.QueryResult,
	fieldConfig json.RawMessage,
) ([]tableColumn, []tableRow) {
	refIDs := make([]string, 0, len(result.Results))
	for refID := range result.Results {
		refIDs = append(refIDs, refID)
	}
	slices.Sort(refIDs)

	frames := make([]tableFrameData, 0)
	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			frameColumns := tableColumns(frame.Schema.Fields, fieldConfig)
			frameRows, err := tableRows(frame)
			if err != nil || len(frameRows) == 0 {
				continue
			}
			frames = append(frames, tableFrameData{columns: frameColumns, rows: frameRows})
		}
	}
	if len(frames) == 0 {
		return nil, nil
	}
	if joinedColumns, joinedRows, ok := joinTableFrames(frames); ok {
		return joinedColumns, joinedRows
	}

	columns := frames[0].columns
	rows := make([]tableRow, 0)
	for _, frame := range frames {
		if sameTableFields(columns, frame.columns) {
			rows = append(rows, frame.rows...)
		}
	}

	return columns, rows
}

func joinTableFrames(frames []tableFrameData) ([]tableColumn, []tableRow, bool) {
	if len(frames) < 2 {
		return nil, nil, false
	}
	for _, frame := range frames[1:] {
		if !sameTableFields(frames[0].columns, frame.columns) {
			return joinTimeSeriesFrames(frames)
		}
	}

	return nil, nil, false
}

func joinTimeSeriesFrames(frames []tableFrameData) ([]tableColumn, []tableRow, bool) {
	timeIndexes := make([]int, len(frames))
	for index, frame := range frames {
		timeIndexes[index] = tableTimeFieldIndex(frame.columns)
		if timeIndexes[index] < 0 {
			return nil, nil, false
		}
	}

	columns, mappings := timeSeriesTableColumns(frames, timeIndexes)
	rows := joinTimeSeriesRows(frames, timeIndexes, mappings, len(columns))

	return columns, rows, true
}

func timeSeriesTableColumns(
	frames []tableFrameData,
	timeIndexes []int,
) ([]tableColumn, [][]tableColumnMapping) {
	firstTime := timeIndexes[0]
	columns := []tableColumn{frames[0].columns[firstTime]}
	usedHeaders := map[string]bool{columns[0].header: true}
	mappings := make([][]tableColumnMapping, len(frames))
	for frameIndex, frame := range frames {
		for inputIndex, column := range frame.columns {
			if inputIndex == timeIndexes[frameIndex] {
				continue
			}
			column.header = uniqueTableHeader(column.header, usedHeaders)
			mappings[frameIndex] = append(mappings[frameIndex], tableColumnMapping{
				output: len(columns), input: inputIndex,
			})
			columns = append(columns, column)
		}
	}

	return columns, mappings
}

func joinTimeSeriesRows(
	frames []tableFrameData,
	timeIndexes []int,
	mappings [][]tableColumnMapping,
	columnCount int,
) []tableRow {
	rows := make([]tableRow, 0)
	rowIndexes := make(map[string]int)
	for frameIndex, frame := range frames {
		seenTimes := make(map[string]int, len(frame.rows))
		for _, inputRow := range frame.rows {
			timeValue := inputRow.values[timeIndexes[frameIndex]]
			baseKey := fmt.Sprintf("%T:%v", timeValue, timeValue)
			occurrence := seenTimes[baseKey]
			seenTimes[baseKey] = occurrence + 1
			key := fmt.Sprintf("%s#%d", baseKey, occurrence)
			outputIndex, exists := rowIndexes[key]
			if !exists {
				outputIndex = len(rows)
				rowIndexes[key] = outputIndex
				rows = append(rows, tableRow{values: make([]any, columnCount)})
				rows[outputIndex].values[0] = timeValue
			}
			for _, mapping := range mappings[frameIndex] {
				if mapping.input < len(inputRow.values) {
					rows[outputIndex].values[mapping.output] = inputRow.values[mapping.input]
				}
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return compareTableValues(rows[i].values[0], rows[j].values[0]) < 0
	})

	return rows
}

func tableTimeFieldIndex(columns []tableColumn) int {
	for index, column := range columns {
		if column.field.Type == fieldTypeTime {
			return index
		}
	}

	return -1
}

func uniqueTableHeader(header string, used map[string]bool) string {
	candidate := header
	for suffix := 2; used[candidate]; suffix++ {
		candidate = fmt.Sprintf("%s #%d", header, suffix)
	}
	used[candidate] = true

	return candidate
}

func tableColumns(
	fields []grafana.DataFrameField,
	fieldConfig json.RawMessage,
) []tableColumn {
	columns := make([]tableColumn, 0, len(fields))
	for _, field := range fields {
		presentation := presentField(fieldConfig, field, nil)
		columns = append(columns, tableColumn{
			field:  field,
			header: presentation.DisplayName,
			width:  presentation.Width,
			align:  presentation.Align,
		})
	}

	return columns
}

func tableRows(frame grafana.DataFrame) ([]tableRow, error) {
	if len(frame.Schema.Fields) == 0 || len(frame.Data.Values) == 0 {
		return []tableRow{}, nil
	}

	columns := make([][]any, len(frame.Schema.Fields))
	rowCount := 0
	for index := range frame.Schema.Fields {
		if index >= len(frame.Data.Values) {
			columns[index] = []any{}
			continue
		}
		if err := json.Unmarshal(frame.Data.Values[index], &columns[index]); err != nil {
			return nil, fmt.Errorf("decoding table column %q: %w", frame.Schema.Fields[index].Name, err)
		}
		rowCount = max(rowCount, len(columns[index]))
	}

	rows := make([]tableRow, rowCount)
	for rowIndex := range rowCount {
		values := make([]any, len(columns))
		for columnIndex := range columns {
			if rowIndex < len(columns[columnIndex]) {
				values[columnIndex] = columns[columnIndex][rowIndex]
			}
		}
		rows[rowIndex] = tableRow{values: values}
	}

	return rows, nil
}

func sameTableFields(left, right []tableColumn) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].field.Name != right[index].field.Name || left[index].header != right[index].header {
			return false
		}
	}

	return true
}

func sortTableRows(
	rows []tableRow,
	columns []tableColumn,
	options tableOptions,
) {
	if len(options.SortBy) == 0 {
		return
	}

	sort.SliceStable(rows, func(i, j int) bool {
		for _, sortField := range options.SortBy {
			columnIndex := tableColumnIndex(columns, sortField.DisplayName)
			if columnIndex < 0 {
				continue
			}
			comparison := compareTableValues(
				rows[i].values[columnIndex],
				rows[j].values[columnIndex],
			)
			if comparison == 0 {
				continue
			}
			if sortField.Desc {
				return comparison > 0
			}
			return comparison < 0
		}

		return false
	})
}

func tableColumnIndex(columns []tableColumn, displayName string) int {
	for index, column := range columns {
		if column.header == displayName || column.field.Name == displayName {
			return index
		}
	}

	return -1
}

func compareTableValues(left, right any) int {
	leftNumber, leftOK := fieldNumber(left)
	rightNumber, rightOK := fieldNumber(right)
	if leftOK && rightOK {
		switch {
		case leftNumber < rightNumber:
			return -1
		case leftNumber > rightNumber:
			return 1
		default:
			return 0
		}
	}

	return strings.Compare(
		fmt.Sprint(left),
		fmt.Sprint(right),
	)
}

func presentTableRows(
	rows []tableRow,
	columns []tableColumn,
	fieldConfig json.RawMessage,
) [][]fieldPresentation {
	presented := make([][]fieldPresentation, len(rows))
	for rowIndex, row := range rows {
		presented[rowIndex] = make([]fieldPresentation, len(columns))
		for columnIndex, column := range columns {
			presented[rowIndex][columnIndex] = presentField(
				fieldConfig,
				column.field,
				row.values[columnIndex],
			)
			if column.field.Type == fieldTypeTime {
				presented[rowIndex][columnIndex].Text = formatTableTime(
					row.values[columnIndex],
					presented[rowIndex][columnIndex].Text,
				)
			}
		}
	}

	return presented
}

func formatTableTime(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	milliseconds, ok := fieldNumber(value)
	if !ok {
		return fallback
	}

	//nolint:gosmopolitan // table timestamps use the viewer's local timezone
	return time.UnixMilli(int64(milliseconds)).In(time.Local).Format("2006-01-02 15:04:05")
}

func calculateColumnWidths(
	columns []tableColumn,
	rows [][]fieldPresentation,
	maxWidth int,
) []int {
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = max(lipgloss.Width(column.header), tableMinColWidth)
		if column.width > 0 {
			widths[index] = max(widths[index], column.width/10)
		}
	}
	for _, row := range rows {
		for index, cell := range row {
			if index < len(widths) {
				widths[index] = max(widths[index], lipgloss.Width(cell.Text))
			}
		}
	}

	totalPadding := max((len(widths)-1)*tableColumnPadding, 0)
	available := max(maxWidth-totalPadding, len(widths)*tableMinColWidth)
	total := 0
	for _, width := range widths {
		total += width
	}
	if total > available {
		for index, width := range widths {
			widths[index] = max(width*available/total, tableMinColWidth)
		}
	}

	return widths
}

//nolint:exhaustruct // presentation defaults are intentionally zero-valued
func renderTableLines(
	columns []tableColumn,
	rows [][]fieldPresentation,
	widths []int,
	maxHeight int,
	totalRowCount int,
	showHeader bool,
) []string {
	lines := make([]string, 0, maxHeight)
	if showHeader {
		headers := make([]fieldPresentation, len(columns))
		for index, column := range columns {
			headers[index] = fieldPresentation{Text: column.header}
		}
		header := formatTableRow(headers, widths, columns, true)
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(header))
		lines = append(lines, buildTableSeparator(widths))
	}

	remainingHeight := maxHeight - len(lines)
	if remainingHeight <= 0 {
		return lines[:maxHeight]
	}
	displayCount := min(len(rows), remainingHeight)
	truncated := totalRowCount > remainingHeight
	if truncated {
		displayCount = min(len(rows), max(remainingHeight-1, 0))
	}
	for index := range displayCount {
		lines = append(lines, formatTableRow(rows[index], widths, columns, false))
	}
	if truncated && len(lines) < maxHeight {
		lines = append(lines, fmt.Sprintf("... and %d more rows", totalRowCount-displayCount))
	}

	return lines
}

//nolint:exhaustruct // empty cells use all zero-value presentation fields
func formatTableRow(
	cells []fieldPresentation,
	widths []int,
	columns []tableColumn,
	header bool,
) string {
	parts := make([]string, len(widths))
	for index, width := range widths {
		cell := fieldPresentation{}
		if index < len(cells) {
			cell = cells[index]
		}
		text := truncateTableText(cell.Text, width)
		align := lipgloss.Left
		if !header && index < len(columns) {
			switch columns[index].align {
			case alignCenter:
				align = lipgloss.Center
			case directionRight:
				align = lipgloss.Right
			}
		}
		style := lipgloss.NewStyle().Width(width).Align(align)
		if !header && cell.Color != "" {
			if cell.Background {
				style = style.Background(lipgloss.Color(cell.Color)).
					Foreground(lipgloss.Color(contrastColor(cell.Color)))
			} else {
				style = style.Foreground(lipgloss.Color(cell.Color))
			}
		}
		parts[index] = style.Render(text)
	}

	return strings.Join(parts, strings.Repeat(" ", tableColumnPadding))
}

func truncateTableText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) <= width {
		return text
	}

	runes := []rune(text)
	for end := len(runes); end > 0; end-- {
		if lipgloss.Width(string(runes[:end])) <= width {
			return string(runes[:end])
		}
	}

	return ""
}

func buildTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for index, width := range widths {
		parts[index] = strings.Repeat("─", width)
	}

	return strings.Join(parts, strings.Repeat(" ", tableColumnPadding))
}
