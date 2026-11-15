package grafana

import (
	"encoding/json"
	"errors"
	"fmt"
)

type groupingToMatrixOptions struct {
	RowField    string `json:"rowField"`
	ColumnField string `json:"columnField"`
	ValueField  string `json:"valueField"`
	EmptyValue  string `json:"emptyValue"`
}

var (
	errMatrixFields     = errors.New("groupingToMatrix required fields not found")
	errMatrixColumns    = errors.New("groupingToMatrix could not read input columns")
	errMatrixIncomplete = errors.New("groupingToMatrix input columns are incomplete")
)

func applyGroupingToMatrix(result *QueryResult, raw json.RawMessage) {
	var options groupingToMatrixOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		result.Warnings = append(result.Warnings, "invalid groupingToMatrix options")
		return
	}
	if options.RowField == "" || options.ColumnField == "" || options.ValueField == "" {
		result.Warnings = append(result.Warnings, "groupingToMatrix requires row, column, and value fields")
		return
	}

	refIDs := sortedResultRefIDs(result)
	if len(refIDs) == 0 {
		return
	}

	matrix := newMatrixBuilder(options)
	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			if err := matrix.addFrame(frame); err != nil {
				result.Warnings = append(result.Warnings, err.Error())
			}
		}
	}
	//nolint:exhaustruct // query errors have no metadata here
	result.Results = map[string]QueryResultData{
		refIDs[0]: {Frames: []DataFrame{matrix.dataFrame(refIDs[0])}},
	}
}

type matrixBuilder struct {
	options     groupingToMatrixOptions
	columns     []string
	columnIndex map[string]int
	rows        []any
	rowIndex    map[string]int
	values      [][]any
	valueField  DataFrameField
}

func newMatrixBuilder(options groupingToMatrixOptions) *matrixBuilder {
	return &matrixBuilder{
		options:     options,
		columns:     []string{},
		columnIndex: make(map[string]int),
		rows:        []any{},
		rowIndex:    make(map[string]int),
		values:      [][]any{},
		valueField:  DataFrameField{Name: options.ValueField, Type: fieldTypeNumber, Labels: nil, Config: nil},
	}
}

//nolint:cyclop // matrix validation and row/column assembly share one pass
func (m *matrixBuilder) addFrame(frame DataFrame) error {
	rowIndex := findFieldIndex(&frame, m.options.RowField)
	columnIndex := findFieldIndex(&frame, m.options.ColumnField)
	valueIndex := findFieldIndex(&frame, m.options.ValueField)
	if rowIndex < 0 || columnIndex < 0 || valueIndex < 0 {
		return fmt.Errorf("%w: row=%q column=%q value=%q",
			errMatrixFields, m.options.RowField, m.options.ColumnField, m.options.ValueField)
	}

	columns := parseAllColumns(&frame)
	if columns == nil {
		return errMatrixColumns
	}
	if rowIndex >= len(columns) || columnIndex >= len(columns) || valueIndex >= len(columns) {
		return errMatrixIncomplete
	}
	m.valueField = frame.Schema.Fields[valueIndex]

	for index, rowValue := range columns[rowIndex] {
		if index >= len(columns[columnIndex]) || index >= len(columns[valueIndex]) {
			continue
		}
		columnValue := columns[columnIndex][index]
		if columnValue == nil || rowValue == nil {
			continue
		}
		columnName := fmt.Sprint(columnValue)
		m.addColumn(columnName)
		m.addCell(rowValue, columnName, columns[valueIndex][index])
	}

	return nil
}

func (m *matrixBuilder) addColumn(name string) {
	if _, exists := m.columnIndex[name]; exists {
		return
	}
	m.columnIndex[name] = len(m.columns)
	m.columns = append(m.columns, name)
	for row := range m.values {
		m.values[row] = append(m.values[row], m.emptyValue())
	}
}

func (m *matrixBuilder) addCell(rowValue any, columnName string, value any) {
	rowKey := joinRowKey(rowValue)
	rowIndex, exists := m.rowIndex[rowKey]
	if !exists {
		rowIndex = len(m.rows)
		m.rowIndex[rowKey] = rowIndex
		m.rows = append(m.rows, rowValue)
		values := make([]any, len(m.columns))
		for index := range values {
			values[index] = m.emptyValue()
		}
		m.values = append(m.values, values)
	}
	m.values[rowIndex][m.columnIndex[columnName]] = value
}

func (m *matrixBuilder) emptyValue() any {
	if m.options.EmptyValue == "zero" {
		return float64(0)
	}

	return nil
}

func (m *matrixBuilder) dataFrame(refID string) DataFrame {
	rowFieldName := m.options.RowField + `\` + m.options.ColumnField
	fields := make([]DataFrameField, 0, len(m.columns)+1)
	fields = append(fields, DataFrameField{
		Name: rowFieldName, Type: fieldTypeString, Labels: nil, Config: nil,
	})
	values := make([]json.RawMessage, 0, len(m.columns)+1)
	values = append(values, mustMarshal(m.rows))
	for index, name := range m.columns {
		field := m.valueField
		field.Name = name
		field.Config = withoutDatasourceDisplayName(field.Config)
		fields = append(fields, field)
		column := make([]any, len(m.rows))
		for rowIndex := range m.rows {
			column[rowIndex] = m.values[rowIndex][index]
		}
		values = append(values, mustMarshal(column))
	}

	return DataFrame{
		Schema: DataFrameSchema{RefID: refID, Meta: DataFrameMeta{Type: "", TypeVersion: nil}, Fields: fields},
		Data:   DataFrameData{Values: values},
	}
}
