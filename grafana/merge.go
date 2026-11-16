package grafana

import "encoding/json"

func applyMerge(result *QueryResult) {
	refIDs := sortedResultRefIDs(result)
	if len(refIDs) == 0 {
		return
	}

	merged := DataFrame{
		Schema: DataFrameSchema{
			RefID: refIDs[0], Meta: DataFrameMeta{Type: "", TypeVersion: nil}, Fields: []DataFrameField{},
		},
		Data: DataFrameData{Values: []json.RawMessage{}},
	}
	numericDuplicates := mergeNumericDuplicates(result)
	fieldIndexes := make(map[string]int)
	fieldSources := make(map[string]string)
	fieldRefIDs := make(map[string]string)
	columns := make([][]any, 0)
	mergedRows := 0

	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			for _, field := range frame.Schema.Fields {
				name := field.Name
				if numericDuplicates[name] {
					name += " #" + refID
				}
				if _, exists := fieldIndexes[name]; exists {
					continue
				}
				fieldIndexes[name] = len(merged.Schema.Fields)
				fieldSources[name] = field.Name
				fieldRefIDs[name] = refID
				field.Name = name
				merged.Schema.Fields = append(merged.Schema.Fields, field)
				columns = append(columns, make([]any, mergedRows))
			}
		}
	}
	columns, _ = mergeRows(
		result, refIDs, merged.Schema.Fields, fieldSources, fieldRefIDs,
		numericDuplicates, columns, mergedRows,
	)

	merged.Data.Values = rawColumns(columns)
	//nolint:exhaustruct // query errors have no metadata here
	result.Results = map[string]QueryResultData{
		refIDs[0]: {Frames: []DataFrame{merged}},
	}
}

func mergeRows(
	result *QueryResult,
	refIDs []string,
	fields []DataFrameField,
	fieldSources map[string]string,
	fieldRefIDs map[string]string,
	numericDuplicates map[string]bool,
	columns [][]any,
	mergedRows int,
) ([][]any, int) {
	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			input := parseAllColumns(&frame)
			rowCount := frameRowCount(&frame)
			for fieldIndex, outputField := range fields {
				sourceName := fieldSources[outputField.Name]
				inputIndex := -1
				if !numericDuplicates[sourceName] || fieldRefIDs[outputField.Name] == refID {
					inputIndex = findFieldIndex(&frame, sourceName)
				}
				for row := range rowCount {
					var value any
					if inputIndex >= 0 && inputIndex < len(input) && row < len(input[inputIndex]) {
						value = input[inputIndex][row]
					}
					columns[fieldIndex] = append(columns[fieldIndex], value)
				}
			}
			mergedRows += rowCount
		}
	}

	return columns, mergedRows
}

func mergeNumericDuplicates(result *QueryResult) map[string]bool {
	refIDsByName := make(map[string]map[string]bool)
	nonNumericNames := make(map[string]bool)
	for refID, data := range result.Results {
		for _, frame := range data.Frames {
			for _, field := range frame.Schema.Fields {
				if field.Type != fieldTypeNumber {
					nonNumericNames[field.Name] = true
					continue
				}
				if refIDsByName[field.Name] == nil {
					refIDsByName[field.Name] = make(map[string]bool)
				}
				refIDsByName[field.Name][refID] = true
			}
		}
	}
	duplicates := make(map[string]bool)
	for name, refIDs := range refIDsByName {
		duplicates[name] = len(refIDs) > 1 && !nonNumericNames[name]
	}

	return duplicates
}
