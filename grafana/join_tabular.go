package grafana

type joinFrameInput struct {
	frame     *DataFrame
	columns   [][]any
	keyField  DataFrameField
	keyValues []any
	refID     string
}

//nolint:cyclop // direct fields and wide-label normalization share one input builder
func joinFrameInputs(frame *DataFrame, byField, resultRefID string) []joinFrameInput {
	inputColumns := parseAllColumns(frame)
	refID := frame.Schema.RefID
	if refID == "" {
		refID = resultRefID
	}
	keyIdx := findFieldIndex(frame, byField)
	if keyIdx >= 0 && keyIdx < len(inputColumns) {
		return []joinFrameInput{{
			frame: frame, columns: inputColumns, keyField: frame.Schema.Fields[keyIdx],
			keyValues: inputColumns[keyIdx], refID: refID,
		}}
	}

	// Split wide series so each field retains its own label/value association.
	inputs := make([]joinFrameInput, 0)
	for fieldIdx, field := range frame.Schema.Fields {
		labelValue, exists := field.Labels[byField]
		if !exists || fieldIdx >= len(inputColumns) {
			continue
		}
		narrowFields := make([]DataFrameField, 0, 2)
		narrowColumns := make([][]any, 0, 2)
		for index, candidate := range frame.Schema.Fields {
			if candidate.Type == fieldTypeTime && index < len(inputColumns) {
				narrowFields = append(narrowFields, candidate)
				narrowColumns = append(narrowColumns, inputColumns[index])
			}
		}
		narrowFields = append(narrowFields, field)
		narrowColumns = append(narrowColumns, inputColumns[fieldIdx])
		narrowFrame := &DataFrame{
			Schema: DataFrameSchema{RefID: frame.Schema.RefID, Meta: frame.Schema.Meta, Fields: narrowFields},
			Data:   DataFrameData{Values: rawColumns(narrowColumns)},
		}
		keyValues := make([]any, len(inputColumns[fieldIdx]))
		for index := range keyValues {
			keyValues[index] = labelValue
		}
		inputs = append(inputs, joinFrameInput{
			frame: narrowFrame, columns: narrowColumns,
			keyField:  DataFrameField{Name: byField, Type: fieldTypeString, Labels: nil, Config: nil},
			keyValues: keyValues, refID: refID,
		})
	}

	return inputs
}

func applyTabularJoin(result *QueryResult, byField, mode string, refIDs []string) {
	join := newDataFrameJoin(byField, refIDs[0])
	for _, refID := range refIDs {
		rd := result.Results[refID]
		for index := range rd.Frames {
			inputs := joinFrameInputs(&rd.Frames[index], byField, refID)
			if len(inputs) == 0 && mode == joinModeInner {
				join.clearRows()
				//nolint:exhaustruct // no query error metadata
				result.Results = map[string]QueryResultData{
					refIDs[0]: {Frames: []DataFrame{join.dataFrameWithCoalesce(false)}},
				}
				return
			}
			for _, input := range inputs {
				join.addTabularInput(input, mode)
			}
		}
	}
	result.Results = map[string]QueryResultData{
		refIDs[0]: {Frames: []DataFrame{join.dataFrameWithCoalesce(false)}}, //nolint:exhaustruct // no query error metadata
	}
}

func (j *dataFrameJoin) clearRows() {
	for index := range j.columns {
		j.columns[index] = make([]any, 0)
	}
}

//nolint:cyclop // relational joins handle duplicate matches and both unmatched sides
func (j *dataFrameJoin) addTabularInput(input joinFrameInput, mode string) {
	if !j.keyFieldSet {
		j.frame.Schema.Fields[0] = input.keyField
		j.keyFieldSet = true
		j.addFirstTabularInput(input)
		return
	}

	leftColumns := j.columns
	leftRows := len(leftColumns[0])
	columnIndexes := j.addColumns(input.frame, input.columns, input.refID)
	rightRows := len(input.keyValues)
	rightIndex := make(map[string][]int, rightRows)
	for row, value := range input.keyValues {
		if value != nil {
			key := joinRowKey(value)
			rightIndex[key] = append(rightIndex[key], row)
		}
	}
	matchedRight := make([]bool, rightRows)
	nextColumns := make([][]any, len(j.columns))
	for index := range nextColumns {
		nextColumns[index] = make([]any, 0)
	}
	for leftRow := range leftRows {
		keyValue := frameValueAt(leftColumns[0], leftRow)
		matches := []int(nil)
		if keyValue != nil {
			matches = rightIndex[joinRowKey(keyValue)]
		}
		for _, rightRow := range matches {
			matchedRight[rightRow] = true
			appendTabularRow(nextColumns, leftColumns, leftRow, input, columnIndexes, rightRow)
		}
		if len(matches) == 0 && mode == joinModeOuterTabular {
			appendTabularRow(nextColumns, leftColumns, leftRow, input, columnIndexes, -1)
		}
	}
	if mode == joinModeOuterTabular {
		for rightRow, matched := range matchedRight {
			if !matched {
				appendTabularRow(nextColumns, leftColumns, -1, input, columnIndexes, rightRow)
			}
		}
	}
	j.columns = nextColumns
}

func (j *dataFrameJoin) addFirstTabularInput(input joinFrameInput) {
	columnIndexes := j.addColumns(input.frame, input.columns, input.refID)
	for row, keyValue := range input.keyValues {
		j.columns[0] = append(j.columns[0], keyValue)
		for fieldIndex, columnIndex := range columnIndexes {
			j.columns[columnIndex] = append(
				j.columns[columnIndex], frameValueAt(input.columns[fieldIndex], row),
			)
		}
	}
}

func appendTabularRow(
	columns, leftColumns [][]any,
	leftRow int,
	input joinFrameInput,
	columnIndexes map[int]int,
	rightRow int,
) {
	for index, column := range leftColumns {
		value := frameValueAt(column, leftRow)
		if index == 0 && leftRow < 0 {
			value = frameValueAt(input.keyValues, rightRow)
		}
		columns[index] = append(columns[index], value)
	}
	for fieldIndex, columnIndex := range columnIndexes {
		columns[columnIndex] = append(
			columns[columnIndex], frameValueAt(input.columns[fieldIndex], rightRow),
		)
	}
}
