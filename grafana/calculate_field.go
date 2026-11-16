package grafana

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

const (
	reducerSum      = "sum"
	reducerMean     = "mean"
	reducerMin      = "min"
	reducerMax      = "max"
	reducerCount    = "count"
	reducerFirst    = "first"
	reducerLast     = "last"
	reducerRange    = "range"
	reducerStdDev   = "stdDev"
	reducerVariance = "variance"
)

type calculateFieldOptions struct {
	Mode          string                 `json:"mode"`
	Alias         string                 `json:"alias"`
	ReplaceFields bool                   `json:"replaceFields"`
	TimeSeries    *bool                  `json:"timeSeries"`
	Binary        calculateBinaryOptions `json:"binary"`
	Reduce        calculateReduceOptions `json:"reduce"`
	Unary         calculateUnaryOptions  `json:"unary"`
	Cumulative    calculateSeriesOptions `json:"cumulative"`
	Window        calculateWindowOptions `json:"window"`
	Index         calculateIndexOptions  `json:"index"`
}

type calculateBinaryOptions struct {
	Left     calculateBinaryValue `json:"left"`
	Operator string               `json:"operator"`
	Right    calculateBinaryValue `json:"right"`
}

type calculateBinaryValue struct {
	Fixed   string `json:"fixed"`
	Matcher *struct {
		ID      string `json:"id"`
		Options string `json:"options"`
	} `json:"matcher"`
	Legacy string `json:"-"`
}

func (v *calculateBinaryValue) UnmarshalJSON(data []byte) error {
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		v.Legacy = legacy
		return nil
	}

	type valueAlias calculateBinaryValue
	var decoded valueAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decoding binary operand: %w", err)
	}
	*v = calculateBinaryValue(decoded)

	return nil
}

type calculateReduceOptions struct {
	Include       []string `json:"include"`
	Reducer       string   `json:"reducer"`
	NullValueMode string   `json:"nullValueMode"`
}

func applyCalculateField(result *QueryResult, raw json.RawMessage) {
	var opts calculateFieldOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		result.Warnings = append(result.Warnings, "invalid calculateField options")
		return
	}

	switch opts.Mode {
	case "", "reduceRow":
		prepareNonBinaryFrames(result, &opts)
		applyCalculateFieldToFrames(result, &opts, calculateReducedFrame)
	case "binary":
		applyBinaryCalculation(result, &opts)
	case "unary":
		prepareNonBinaryFrames(result, &opts)
		applyCalculateFieldToFrames(result, &opts, calculateUnaryFrame)
	case "cumulativeFunctions":
		applyCumulativeCalculation(result, &opts)
	case "windowFunctions":
		applyWindowCalculation(result, &opts)
	case "index":
		prepareNonBinaryFrames(result, &opts)
		applyCalculateFieldToFrames(result, &opts, calculateIndexFrame)
	default:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("unsupported calculateField mode %q", opts.Mode))
	}
}

func applyBinaryCalculation(result *QueryResult, opts *calculateFieldOptions) {
	if opts.Binary.Operator == "" {
		opts.Binary.Operator = "+"
	}
	if !supportedBinaryOperator(opts.Binary.Operator) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("unsupported calculateField operator %q", opts.Binary.Operator))
		return
	}
	prepareBinaryFrames(result, opts)
	if opts.Binary.Left.Matcher != nil && opts.Binary.Left.Matcher.ID == "byType" {
		applyCalculateFieldToFrames(result, opts, calculateBinaryByTypeFrame)
	} else {
		applyCalculateFieldToFrames(result, opts, calculateBinaryFrame)
	}
}

func applyCumulativeCalculation(result *QueryResult, opts *calculateFieldOptions) {
	if !supportedCumulativeReducer(opts.Cumulative.Reducer) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("unsupported cumulative reducer %q", opts.Cumulative.Reducer))
		return
	}
	prepareNonBinaryFrames(result, opts)
	applyCalculateFieldToFrames(result, opts, calculateCumulativeFrame)
}

func applyWindowCalculation(result *QueryResult, opts *calculateFieldOptions) {
	if warning := normalizeWindowOptions(opts); warning != "" {
		result.Warnings = append(result.Warnings, warning)
		return
	}
	prepareNonBinaryFrames(result, opts)
	applyCalculateFieldToFrames(result, opts, calculateWindowFrame)
}

func applyCalculateFieldToFrames(
	result *QueryResult,
	opts *calculateFieldOptions,
	calculate func(*DataFrame, *calculateFieldOptions) bool,
) {
	calculatedAny := false
	for refID, data := range result.Results {
		frames := make([]DataFrame, 0, len(data.Frames))
		for i := range data.Frames {
			frame := &data.Frames[i]
			calculated := calculate(frame, opts)
			calculatedAny = calculatedAny || calculated
			if !opts.ReplaceFields || calculated {
				frames = append(frames, *frame)
			}
		}
		data.Frames = frames
		result.Results[refID] = data
	}
	if !calculatedAny {
		result.Warnings = append(result.Warnings, "calculateField matched no fields")
	}
}

func prepareBinaryFrames(result *QueryResult, opts *calculateFieldOptions) {
	if opts.TimeSeries != nil && !*opts.TimeSeries {
		return
	}
	if _, fixed := fixedBinaryNumber(opts.Binary.Right); fixed {
		return
	}

	timeField := commonTimeField(result)
	if timeField == "" {
		return
	}

	applyJoinByField(result, mustMarshal(joinByFieldOptions{
		ByField: timeField,
		Mode:    joinModeOuter,
	}))
}

func commonTimeField(result *QueryResult) string {
	timeField := ""
	frameCount := 0
	for _, refID := range sortedResultRefIDs(result) {
		for _, frame := range result.Results[refID].Frames {
			frameCount++
			current := firstTimeField(frame)
			if current == "" || (timeField != "" && current != timeField) {
				return ""
			}
			timeField = current
		}
	}
	if frameCount < 2 {
		return ""
	}

	return timeField
}

func firstTimeField(frame DataFrame) string {
	for _, field := range frame.Schema.Fields {
		if field.Type == fieldTypeTime {
			return field.Name
		}
	}

	return ""
}

func calculateBinaryFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	left, leftName, ok := binaryOperand(frame, opts.Binary.Left)
	if !ok {
		return false
	}
	right, rightName, ok := binaryOperand(frame, opts.Binary.Right)
	if !ok {
		return false
	}

	values := calculateBinaryValues(left, right, opts.Binary.Operator)

	name := opts.Alias
	if name == "" {
		name = strings.TrimSpace(leftName + " " + opts.Binary.Operator + " " + rightName)
	}
	appendCalculatedField(frame, opts, name, values)

	return true
}

func calculateBinaryValues(left, right []any, operator string) []any {
	values := make([]any, len(left))
	for row := range left {
		leftNumber, leftOK := binaryNumber(left[row])
		var rightValue any
		if row < len(right) {
			rightValue = right[row]
		}
		rightNumber, rightOK := binaryNumber(rightValue)
		if !leftOK || !rightOK {
			continue
		}
		value := applyBinaryOperator(leftNumber, operator, rightNumber)
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			values[row] = value
		}
	}

	return values
}

func binaryOperand(
	frame *DataFrame,
	value calculateBinaryValue,
) ([]any, string, bool) {
	if value.Matcher != nil && value.Matcher.ID == "byName" {
		index := findFieldIndex(frame, value.Matcher.Options)
		if index < 0 || index >= len(frame.Data.Values) {
			return nil, value.Matcher.Options, false
		}
		var values []any
		if err := json.Unmarshal(frame.Data.Values[index], &values); err != nil {
			return nil, value.Matcher.Options, false
		}

		return values, value.Matcher.Options, true
	}

	if value.Legacy != "" {
		legacy := calculateBinaryValue{Fixed: "", Matcher: nil, Legacy: ""}
		legacy.Matcher = &struct {
			ID      string `json:"id"`
			Options string `json:"options"`
		}{ID: "byName", Options: value.Legacy}
		if values, name, ok := binaryOperand(frame, legacy); ok {
			return values, name, true
		}
	}

	if number, ok := fixedBinaryNumber(value); ok {
		return constantColumn(frameRowCount(frame), number), binaryValueName(value), true
	}

	return nil, "", false
}

func fixedBinaryNumber(value calculateBinaryValue) (float64, bool) {
	text := value.Fixed
	if text == "" && value.Matcher == nil {
		text = value.Legacy
	}
	if text == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(text, 64)

	return number, err == nil
}

func binaryValueName(value calculateBinaryValue) string {
	if value.Fixed != "" {
		return value.Fixed
	}

	return value.Legacy
}

func constantColumn(length int, value float64) []any {
	column := make([]any, length)
	for row := range column {
		column[row] = value
	}

	return column
}

func binaryNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case nil:
		return 0, true
	case float64:
		return value, true
	case bool:
		if value {
			return 1, true
		}
		return 0, true
	case string:
		number, err := strconv.ParseFloat(value, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func supportedBinaryOperator(operator string) bool {
	switch operator {
	case "+", "-", "*", "/":
		return true
	default:
		return false
	}
}

func applyBinaryOperator(left float64, operator string, right float64) float64 {
	switch operator {
	case "+":
		return left + right
	case "-":
		return left - right
	case "*":
		return left * right
	case "/":
		return left / right
	default:
		return math.NaN()
	}
}

func calculateReducedFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	columns := parseAllColumns(frame)
	reducer := opts.Reduce.Reducer
	if reducer == "" {
		reducer = reducerSum
	}
	if !supportedRowReducer(reducer) {
		return false
	}

	selected := selectedReduceColumns(frame, columns, opts.Reduce.Include)
	if len(selected) == 0 {
		return false
	}

	values := reducedRowValues(frame, columns, selected, reducer, opts.Reduce.NullValueMode)
	name := opts.Alias
	if name == "" {
		name = rowReducerName(reducer)
	}
	appendCalculatedField(frame, opts, name, values)

	return true
}

func selectedReduceColumns(frame *DataFrame, columns [][]any, include []string) []int {
	selected := make([]int, 0, len(frame.Schema.Fields))
	for index, field := range frame.Schema.Fields {
		if field.Type != fieldTypeNumber || index >= len(columns) {
			continue
		}
		if len(include) > 0 && !slices.Contains(include, field.Name) {
			continue
		}
		selected = append(selected, index)
	}

	return selected
}

func reducedRowValues(
	frame *DataFrame,
	columns [][]any,
	selected []int,
	reducer string,
	nullValueMode string,
) []any {
	values := make([]any, frameRowCount(frame))
	for row := range values {
		rowValues := reduceRowNumbers(columns, selected, row, nullValueMode)
		if len(rowValues) == 0 {
			if reducer == reducerSum || reducer == reducerCount {
				values[row] = 0
			}
			continue
		}
		value := applyReducer(rowValues, reducer)
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			values[row] = value
		}
	}

	return values
}

func reduceRowNumbers(
	columns [][]any,
	selected []int,
	row int,
	nullValueMode string,
) []float64 {
	values := make([]float64, 0, len(selected))
	for _, index := range selected {
		var value any
		if row < len(columns[index]) {
			value = columns[index][row]
		}
		number, ok := binaryNumber(value)
		switch {
		case value == nil && nullValueMode == "asZero":
			values = append(values, 0)
		case value == nil && nullValueMode == "ignore":
			continue
		case value == nil || !ok:
			values = append(values, math.NaN())
		default:
			values = append(values, number)
		}
	}

	return values
}

func supportedRowReducer(reducer string) bool {
	switch reducer {
	case reducerSum, reducerMean, reducerMin, reducerMax, reducerCount, reducerFirst, reducerLast:
		return true
	default:
		return false
	}
}

func rowReducerName(reducer string) string {
	names := map[string]string{
		reducerSum: "Total", reducerMean: "Mean", reducerMin: "Min", reducerMax: "Max",
		reducerCount: "Count", reducerFirst: "First", reducerLast: "Last",
	}
	if name, ok := names[reducer]; ok {
		return name
	}

	return reducer
}

func appendCalculatedField(
	frame *DataFrame,
	opts *calculateFieldOptions,
	name string,
	values []any,
) {
	field := DataFrameField{Name: name, Type: fieldTypeNumber, Labels: nil, Config: nil}
	if opts.Alias != "" {
		field.Config = mustMarshal(map[string]string{"displayName": opts.Alias})
	}

	if !opts.ReplaceFields {
		frame.Schema.Fields = append(frame.Schema.Fields, field)
		frame.Data.Values = append(frame.Data.Values, mustMarshal(values))
		return
	}

	fields := make([]DataFrameField, 0, 2)
	columns := make([]json.RawMessage, 0, 2)
	if opts.TimeSeries == nil || *opts.TimeSeries {
		for index, existing := range frame.Schema.Fields {
			if existing.Type == fieldTypeTime && index < len(frame.Data.Values) {
				fields = append(fields, existing)
				columns = append(columns, frame.Data.Values[index])
				break
			}
		}
	}
	fields = append(fields, field)
	columns = append(columns, mustMarshal(values))
	frame.Schema.Fields = fields
	frame.Data.Values = columns
}
