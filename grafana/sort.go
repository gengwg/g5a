package grafana

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type sortByOptions struct {
	Sort []struct {
		Field string `json:"field"`
		Desc  bool   `json:"desc"`
	} `json:"sort"`
}

func applySortBy(result *QueryResult, raw json.RawMessage) {
	var options sortByOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		result.Warnings = append(result.Warnings, "invalid sortBy options")
		return
	}
	for refID, data := range result.Results {
		for index := range data.Frames {
			sortFrameRows(&data.Frames[index], options)
		}
		result.Results[refID] = data
	}
}

//nolint:cyclop // multi-field row ordering and ragged-column handling
func sortFrameRows(frame *DataFrame, options sortByOptions) {
	if len(options.Sort) == 0 {
		return
	}
	columns := parseAllColumns(frame)
	if len(columns) == 0 {
		return
	}
	rowCount := frameRowCount(frame)
	rows := make([]int, rowCount)
	for index := range rows {
		rows[index] = index
	}

	sort.SliceStable(rows, func(i, j int) bool {
		for _, rule := range options.Sort {
			fieldIndex := findFieldIndex(frame, rule.Field)
			if fieldIndex < 0 || fieldIndex >= len(columns) {
				continue
			}
			comparison := compareFrameValues(
				frameValueAt(columns[fieldIndex], rows[i]),
				frameValueAt(columns[fieldIndex], rows[j]),
			)
			if comparison == 0 {
				continue
			}
			if rule.Desc {
				return comparison > 0
			}
			return comparison < 0
		}

		return false
	})

	for fieldIndex := range columns {
		column := make([]any, rowCount)
		for index, rowIndex := range rows {
			column[index] = frameValueAt(columns[fieldIndex], rowIndex)
		}
		frame.Data.Values[fieldIndex] = mustMarshal(column)
	}
}

func frameValueAt(column []any, index int) any {
	if index < 0 || index >= len(column) {
		return nil
	}

	return column[index]
}

func compareFrameValues(left, right any) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}

	leftNumber, leftOK := left.(float64)
	rightNumber, rightOK := right.(float64)
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

	return strings.Compare(fmt.Sprint(left), fmt.Sprint(right))
}
