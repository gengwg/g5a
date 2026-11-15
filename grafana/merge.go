package grafana

import "encoding/json"

//nolint:cyclop // merging handles new fields and backfilled rows per frame
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
	fieldIndexes := make(map[string]int)
	columns := make([][]any, 0)
	mergedRows := 0

	for _, refID := range refIDs {
		for _, frame := range result.Results[refID].Frames {
			input := parseAllColumns(&frame)
			for _, field := range frame.Schema.Fields {
				if _, exists := fieldIndexes[field.Name]; exists {
					continue
				}
				fieldIndexes[field.Name] = len(merged.Schema.Fields)
				merged.Schema.Fields = append(merged.Schema.Fields, field)
				columns = append(columns, make([]any, mergedRows))
			}

			rowCount := frameRowCount(&frame)
			for fieldIndex, outputField := range merged.Schema.Fields {
				inputIndex := findFieldIndex(&frame, outputField.Name)
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

	merged.Data.Values = rawColumns(columns)
	//nolint:exhaustruct // query errors have no metadata here
	result.Results = map[string]QueryResultData{
		refIDs[0]: {Frames: []DataFrame{merged}},
	}
}
