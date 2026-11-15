package grafana

import (
	"encoding/json"
	"slices"
)

// materializeLabelFields expands labeled series into their own frames and adds
// label-backed string columns. A wide frame may contain several label sets, so
// each labeled field gets a separate frame rather than sharing a label column.
func materializeLabelFields(result *QueryResult) {
	for refID, data := range result.Results {
		frames := make([]DataFrame, 0, len(data.Frames))
		for _, frame := range data.Frames {
			frames = append(frames, materializeFrameLabels(frame)...)
		}
		data.Frames = frames
		result.Results[refID] = data
	}
}

//nolint:cyclop // wide-frame series expansion has several small layout cases
func materializeFrameLabels(frame DataFrame) []DataFrame {
	baseFields := frame.Schema.Fields
	baseValues := frame.Data.Values
	expanded := make([]DataFrame, 0, len(baseFields))

	for fieldIndex, labeledField := range baseFields {
		if len(labeledField.Labels) == 0 || fieldIndex >= len(baseValues) {
			continue
		}

		fields := make([]DataFrameField, 0, len(baseFields)+len(labeledField.Labels))
		values := make([]json.RawMessage, 0, len(baseValues)+len(labeledField.Labels))
		for index, field := range baseFields {
			if len(field.Labels) > 0 || index >= len(baseValues) {
				continue
			}
			fields = append(fields, field)
			values = append(values, baseValues[index])
		}
		fields = append(fields, labeledField)
		values = append(values, baseValues[fieldIndex])

		labelNames := make([]string, 0, len(labeledField.Labels))
		for name := range labeledField.Labels {
			labelNames = append(labelNames, name)
		}
		slices.Sort(labelNames)

		existing := make(map[string]bool, len(fields))
		for _, field := range fields {
			existing[field.Name] = true
		}
		rowCount := frameRowCount(&frame)
		for _, name := range labelNames {
			if existing[name] {
				continue
			}
			value := labeledField.Labels[name]
			column := make([]any, rowCount)
			for row := range column {
				column[row] = value
			}
			fields = append(fields, DataFrameField{
				Name: name, Type: fieldTypeString, Labels: nil, Config: nil,
			})
			values = append(values, mustMarshal(column))
		}

		expanded = append(expanded, DataFrame{
			Schema: DataFrameSchema{
				RefID: frame.Schema.RefID, Meta: frame.Schema.Meta, Fields: fields,
			},
			Data: DataFrameData{Values: values},
		})
	}

	if len(expanded) == 0 {
		return []DataFrame{frame}
	}

	return expanded
}
