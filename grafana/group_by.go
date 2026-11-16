package grafana

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	groupByOperationAggregate = "aggregate"
	groupByOperationGroup     = "groupby"
)

var errGroupByOptionType = errors.New("unexpected groupBy option type")

type groupByFieldOptions struct {
	Aggregations []string `json:"aggregations"`
	Operation    string   `json:"operation"`
}

type groupByOptions struct {
	Fields     map[string]groupByFieldOptions `json:"fields"`
	fieldOrder []string
}

func (o *groupByOptions) UnmarshalJSON(data []byte) error {
	type plain groupByOptions
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decoding groupBy options: %w", err)
	}
	*o = groupByOptions(decoded)
	order, err := groupByFieldOrder(data)
	if err != nil {
		return err
	}
	o.fieldOrder = order

	return nil
}

func groupByFieldOrder(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decoding groupBy options: %w", err)
	}
	order := make([]string, 0)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decoding groupBy option name: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("decoding groupBy option name: %w", errGroupByOptionType)
		}
		if name == "fields" {
			order, err = decodeGroupByFieldsOrder(decoder, order)
			if err != nil {
				return nil, err
			}
			continue
		}
		var discard any
		if err := decoder.Decode(&discard); err != nil {
			return nil, fmt.Errorf("decoding groupBy option value: %w", err)
		}
	}

	return order, nil
}

func decodeGroupByFieldsOrder(decoder *json.Decoder, order []string) ([]string, error) {
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decoding groupBy fields: %w", err)
	}
	for decoder.More() {
		fieldToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decoding groupBy field name: %w", err)
		}
		fieldName, ok := fieldToken.(string)
		if !ok {
			return nil, fmt.Errorf("decoding groupBy field name: %w", errGroupByOptionType)
		}
		order = append(order, fieldName)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("decoding groupBy field value: %w", err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decoding groupBy fields: %w", err)
	}

	return order, nil
}

type rowGroup struct {
	rows []int
}

func applyGroupBy(result *QueryResult, raw json.RawMessage) {
	var opts groupByOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		result.Warnings = append(result.Warnings, "invalid groupBy options")
		return
	}
	for refID, data := range result.Results {
		frames := make([]DataFrame, 0, len(data.Frames))
		for _, frame := range data.Frames {
			if grouped := groupFrameRows(result, frame, &opts); grouped != nil {
				frames = append(frames, *grouped)
			}
		}
		data.Frames = frames
		result.Results[refID] = data
	}
}

func groupFrameRows(result *QueryResult, frame DataFrame, opts *groupByOptions) *DataFrame {
	groupFields := make([]int, 0)
	hasGroupOperation := false
	for _, name := range opts.fieldOrder {
		if opts.Fields[name].Operation == groupByOperationGroup {
			hasGroupOperation = true
			if index := findGroupFieldIndex(&frame, name); index >= 0 {
				groupFields = append(groupFields, index)
			}
		}
	}
	if !hasGroupOperation {
		return &frame
	}
	if len(groupFields) == 0 {
		return nil
	}
	columns := parseAllColumns(&frame)
	groups, order := collectRowGroups(frame, columns, groupFields)
	fields, values := groupedFields(frame, columns, groupFields, groups, order)
	for fieldIndex, field := range frame.Schema.Fields {
		fieldOptions := optionsForGroupField(field, opts.Fields)
		if !shouldAggregateGroupField(fieldOptions) {
			continue
		}
		displayName := groupFieldDisplayName(field)
		for _, aggregation := range fieldOptions.Aggregations {
			column, ok := aggregateGroups(columns[fieldIndex], groups, order, aggregation, field.Type)
			if !ok {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("unsupported groupBy aggregation %q", aggregation))
				continue
			}
			fields = append(fields, DataFrameField{
				Name: displayName + " (" + aggregation + ")", Type: groupAggregationType(aggregation, field.Type),
				Labels: nil, Config: nil,
			})
			values = append(values, mustMarshal(column))
		}
	}
	frame.Schema.Fields = fields
	frame.Data.Values = values

	return &frame
}

func findGroupFieldIndex(frame *DataFrame, name string) int {
	for index, field := range frame.Schema.Fields {
		if field.Name == name || groupFieldDisplayName(field) == name {
			return index
		}
	}

	return -1
}

func optionsForGroupField(field DataFrameField, options map[string]groupByFieldOptions) groupByFieldOptions {
	if option, ok := options[groupFieldDisplayName(field)]; ok {
		return option
	}

	return options[field.Name]
}

func shouldAggregateGroupField(options groupByFieldOptions) bool {
	switch options.Operation {
	case groupByOperationAggregate:
		return len(options.Aggregations) > 0
	case groupByOperationGroup:
		return len(options.Aggregations) == 1 && options.Aggregations[0] == reducerCount
	default:
		return false
	}
}

func collectRowGroups(frame DataFrame, columns [][]any, groupFields []int) (map[string]*rowGroup, []string) {
	groups := make(map[string]*rowGroup)
	order := make([]string, 0)
	for row := range frameRowCount(&frame) {
		parts := make([]string, 0, len(groupFields))
		for _, index := range groupFields {
			parts = append(parts, groupValueText(frameValueAt(columns[index], row)))
		}
		key := strings.Join(parts, ",")
		group, exists := groups[key]
		if !exists {
			group = &rowGroup{rows: make([]int, 0)}
			groups[key] = group
			order = append(order, key)
		}
		group.rows = append(group.rows, row)
	}

	return groups, order
}

func groupValueText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := valueOptionText(value); ok {
		return text
	}

	return fmt.Sprint(value)
}

func groupedFields(
	frame DataFrame,
	columns [][]any,
	groupFields []int,
	groups map[string]*rowGroup,
	order []string,
) ([]DataFrameField, []json.RawMessage) {
	fields := make([]DataFrameField, 0, len(groupFields))
	values := make([]json.RawMessage, 0, len(groupFields))
	for _, index := range groupFields {
		field := frame.Schema.Fields[index]
		fields = append(fields, field)
		column := make([]any, 0, len(order))
		for _, key := range order {
			rows := groups[key].rows
			column = append(column, frameValueAt(columns[index], rows[0]))
		}
		values = append(values, mustMarshal(column))
	}

	return fields, values
}

func groupFieldDisplayName(field DataFrameField) string {
	var config struct {
		DisplayName       string `json:"displayName"`
		DisplayNameFromDS string `json:"displayNameFromDS"` //nolint:tagliatelle // Grafana API capitalization
	}
	if err := json.Unmarshal(field.Config, &config); err == nil {
		if config.DisplayName != "" {
			return config.DisplayName
		}
		if config.DisplayNameFromDS != "" {
			return config.DisplayNameFromDS
		}
	}

	return field.Name
}

func groupAggregationType(aggregation, fallback string) string {
	switch aggregation {
	case reducerCount, "countAll", "distinctCount", "changeCount":
		return fieldTypeNumber
	default:
		return fallback
	}
}

func aggregateGroups(
	column []any,
	groups map[string]*rowGroup,
	order []string,
	aggregation string,
	fieldType string,
) ([]any, bool) {
	values := make([]any, 0, len(order))
	for _, key := range order {
		groupValues := make([]any, 0, len(groups[key].rows))
		for _, row := range groups[key].rows {
			groupValues = append(groupValues, frameValueAt(column, row))
		}
		value, ok := aggregateGroupValues(groupValues, aggregation, fieldType)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}

	return values, true
}

//nolint:cyclop // each supported reducer has distinct null and type semantics
func aggregateGroupValues(values []any, aggregation, fieldType string) (any, bool) {
	if value, ok := aggregateNonNumericGroupValues(values, aggregation); ok {
		return value, true
	}
	numbers := numericGroupValues(values, fieldType)
	if len(numbers) == 0 {
		if aggregation == reducerSum || aggregation == reducerVariance || aggregation == reducerStdDev {
			return float64(0), true
		}
		if numericGroupAggregation(aggregation) {
			return nil, true
		}
	}

	switch aggregation {
	case reducerSum, reducerMin, reducerMax, reducerMean, reducerFirst, reducerLast:
		return finiteGroupValue(applyReducer(numbers, aggregation)), true
	case reducerRange:
		return finiteGroupValue(reduceMax(numbers) - reduceMin(numbers)), true
	case "median":
		return medianGroupValue(numbers), true
	case "delta", "diff":
		return finiteGroupValue(numbers[len(numbers)-1] - numbers[0]), true
	case "diffperc":
		if numbers[0] == 0 {
			return nil, true
		}
		return (numbers[len(numbers)-1] - numbers[0]) / numbers[0] * 100, true
	case "step":
		return stepGroupValue(numbers), true
	case reducerVariance:
		variance := groupVariance(numbers)
		return variance, true
	case reducerStdDev:
		return math.Sqrt(groupVariance(numbers)), true
	case "logmin":
		return logMinGroupValue(numbers), true
	default:
		return nil, false
	}
}

func aggregateNonNumericGroupValues(values []any, aggregation string) (any, bool) {
	switch aggregation {
	case reducerCount:
		return float64(countNonNull(values)), true
	case "countAll":
		return float64(len(values)), true
	case "distinctCount":
		return float64(countDistinct(values)), true
	case "changeCount":
		return float64(countChanges(values)), true
	case reducerFirst:
		return firstGroupValue(values, false), true
	case reducerLast:
		return lastGroupValue(values, false), true
	case "firstNotNull":
		return firstGroupValue(values, true), true
	case "lastNotNull":
		return lastGroupValue(values, true), true
	default:
		return nil, false
	}
}

func numericGroupAggregation(aggregation string) bool {
	switch aggregation {
	case reducerMin, reducerMax, reducerMean, reducerRange, "median", "delta", "diff", "diffperc", "step", "logmin":
		return true
	default:
		return false
	}
}

func numericGroupValues(values []any, fieldType string) []float64 {
	if fieldType != fieldTypeNumber && fieldType != fieldTypeTime {
		return nil
	}
	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		if number, ok := toFloat64(value); ok {
			numbers = append(numbers, number)
		}
	}

	return numbers
}

func countNonNull(values []any) int {
	count := 0
	for _, value := range values {
		if value != nil {
			count++
		}
	}

	return count
}

func countDistinct(values []any) int {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[joinRowKey(value)] = true
	}

	return len(seen)
}

func countChanges(values []any) int {
	changes := 0
	for index := 1; index < len(values); index++ {
		if joinRowKey(values[index]) != joinRowKey(values[index-1]) {
			changes++
		}
	}

	return changes
}

func firstGroupValue(values []any, nonNull bool) any {
	for _, value := range values {
		if !nonNull || value != nil {
			return value
		}
	}

	return nil
}

func lastGroupValue(values []any, nonNull bool) any {
	for index := range slices.Backward(values) {
		if !nonNull || values[index] != nil {
			return values[index]
		}
	}

	return nil
}

func finiteGroupValue(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}

	return value
}

func medianGroupValue(values []float64) any {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	slices.Sort(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2
	}

	return sorted[middle]
}

func stepGroupValue(values []float64) any {
	if len(values) < 2 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	slices.Sort(sorted)
	step := math.Inf(1)
	for index := 1; index < len(sorted); index++ {
		step = min(step, sorted[index]-sorted[index-1])
	}

	return finiteGroupValue(step)
}

func groupVariance(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := reduceMean(values)
	sum := 0.0
	for _, value := range values {
		sum += (value - mean) * (value - mean)
	}

	return sum / float64(len(values))
}

func logMinGroupValue(values []float64) any {
	minimum := math.Inf(1)
	for _, value := range values {
		if value > 0 && value < minimum {
			minimum = value
		}
	}

	return finiteGroupValue(minimum)
}
