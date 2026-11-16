package grafana

import (
	"encoding/json"
	"fmt"
	"math"
)

const (
	unaryOperatorAbs     = "abs"
	unaryOperatorPercent = "percent"
	windowSizePercentage = "percentage"
)

type calculateUnaryOptions struct {
	Operator  string `json:"operator"`
	FieldName string `json:"fieldName"`
}

type calculateSeriesOptions struct {
	Field   string `json:"field"`
	Reducer string `json:"reducer"`
}

type calculateWindowOptions struct {
	Field           string   `json:"field"`
	Reducer         string   `json:"reducer"`
	WindowSize      *float64 `json:"windowSize"`
	WindowSizeMode  string   `json:"windowSizeMode"`
	WindowAlignment string   `json:"windowAlignment"`
}

type calculateIndexOptions struct {
	AsPercentile bool `json:"asPercentile"`
}

func prepareNonBinaryFrames(result *QueryResult, opts *calculateFieldOptions) {
	if opts.TimeSeries != nil && !*opts.TimeSeries {
		return
	}
	timeField := commonTimeField(result)
	if timeField == "" {
		return
	}
	applyJoinByField(result, mustMarshal(joinByFieldOptions{ByField: timeField, Mode: joinModeOuter}))
}

func selectedNumericValues(frame *DataFrame, name string) ([]any, bool) {
	for index, field := range frame.Schema.Fields {
		if field.Type != fieldTypeNumber || (name != "" && field.Name != name) || index >= len(frame.Data.Values) {
			continue
		}
		var values []any
		if err := json.Unmarshal(frame.Data.Values[index], &values); err == nil {
			return values, true
		}
	}

	return nil, false
}

func calculateUnaryFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	if opts.Unary.FieldName == "" {
		return false
	}
	values, ok := selectedNumericValues(frame, opts.Unary.FieldName)
	if !ok {
		return false
	}
	operator := opts.Unary.Operator
	if operator == "" {
		operator = unaryOperatorAbs
	}
	if !supportedUnaryOperator(operator) {
		return false
	}

	total := unaryPercentTotal(values, operator)
	result := calculateUnaryValues(values, total, operator)

	name := opts.Alias
	if name == "" {
		name = operator + "(" + opts.Unary.FieldName + ")"
	}
	appendCalculatedField(frame, opts, name, result)

	return true
}

func unaryPercentTotal(values []any, operator string) float64 {
	if operator != unaryOperatorPercent {
		return 0
	}
	total := 0.0
	for _, raw := range values {
		if number, valid := binaryNumber(raw); valid && !math.IsNaN(number) && !math.IsInf(number, 0) {
			total += number
		}
	}

	return total
}

func calculateUnaryValues(values []any, total float64, operator string) []any {
	result := make([]any, len(values))
	for row, raw := range values {
		number, valid := binaryNumber(raw)
		if !valid {
			continue
		}
		value := applyUnaryOperator(number, total, operator)
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			result[row] = value
		}
	}

	return result
}

func supportedUnaryOperator(operator string) bool {
	switch operator {
	case unaryOperatorAbs, "exp", "ln", "round", "floor", "ceil", unaryOperatorPercent:
		return true
	default:
		return false
	}
}

func applyUnaryOperator(value, total float64, operator string) float64 {
	switch operator {
	case unaryOperatorAbs:
		return math.Abs(value)
	case "exp":
		return math.Exp(value)
	case "ln":
		return math.Log(value)
	case "round":
		return math.Floor(value + 0.5)
	case "floor":
		return math.Floor(value)
	case "ceil":
		return math.Ceil(value)
	case unaryOperatorPercent:
		if total == 0 {
			total = 1
		}
		return value / total
	default:
		return math.NaN()
	}
}

func supportedCumulativeReducer(reducer string) bool {
	return reducer == "" || reducer == reducerSum || reducer == reducerMean
}

func calculateCumulativeFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	values, ok := selectedNumericValues(frame, opts.Cumulative.Field)
	if !ok {
		return false
	}
	reducer := opts.Cumulative.Reducer
	if reducer == "" {
		reducer = reducerSum
	}

	result := make([]any, len(values))
	total := 0.0
	for row, raw := range values {
		if number, valid := binaryNumber(raw); valid {
			total += number
		}
		value := total
		if reducer == reducerMean {
			value = total / float64(row+1)
		}
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			result[row] = value
		}
	}

	name := opts.Alias
	if name == "" {
		name = "cumulative " + reducer
		if opts.Cumulative.Field != "" {
			name += "(" + opts.Cumulative.Field + ")"
		}
	}
	appendCalculatedField(frame, opts, name, result)

	return true
}

func normalizeWindowOptions(opts *calculateFieldOptions) string {
	setWindowDefaults(opts)
	return validateWindowOptions(opts)
}

func setWindowDefaults(opts *calculateFieldOptions) {
	if opts.Window.Reducer == "" {
		opts.Window.Reducer = reducerMean
	}
	if opts.Window.WindowSizeMode == "" {
		opts.Window.WindowSizeMode = windowSizePercentage
	}
	if opts.Window.WindowAlignment == "" {
		opts.Window.WindowAlignment = "trailing"
	}
	if opts.Window.WindowSize == nil {
		size := 0.1
		opts.Window.WindowSize = &size
	}
}

func validateWindowOptions(opts *calculateFieldOptions) string {
	if *opts.Window.WindowSize <= 0 {
		return "calculateField window size must be larger than 0"
	}
	switch opts.Window.Reducer {
	case reducerMean, reducerStdDev, reducerVariance:
	default:
		return fmt.Sprintf("unsupported window reducer %q", opts.Window.Reducer)
	}
	if opts.Window.WindowSizeMode != "fixed" && opts.Window.WindowSizeMode != windowSizePercentage {
		return fmt.Sprintf("unsupported window size mode %q", opts.Window.WindowSizeMode)
	}
	if opts.Window.WindowAlignment != "trailing" && opts.Window.WindowAlignment != "centered" {
		return fmt.Sprintf("unsupported window alignment %q", opts.Window.WindowAlignment)
	}

	return ""
}

func calculateWindowFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	values, ok := selectedNumericValues(frame, opts.Window.Field)
	if !ok {
		return false
	}
	windowSize := *opts.Window.WindowSize
	if opts.Window.WindowSizeMode == windowSizePercentage {
		windowSize *= float64(len(values))
	}
	window := int(math.Ceil(windowSize))
	if window < 1 {
		return false
	}

	result := make([]any, len(values))
	for row := range values {
		first, last := windowBounds(row, window, len(values), opts.Window.WindowAlignment)
		value := calculateWindowStatistic(values[first:last], opts.Window.Reducer)
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			result[row] = value
		}
	}

	name := opts.Alias
	if name == "" {
		name = opts.Window.WindowAlignment + " moving " + opts.Window.Reducer
		if opts.Window.Field != "" {
			name += "(" + opts.Window.Field + ")"
		}
	}
	appendCalculatedField(frame, opts, name, result)

	return true
}

func windowBounds(row, window, length int, alignment string) (int, int) {
	if alignment == "centered" {
		leading := int(math.Ceil(float64(window)/2)) - 1
		trailing := window / 2
		return max(row-trailing, 0), min(row+leading+1, length)
	}

	return max(row-window+1, 0), row + 1
}

func calculateWindowStatistic(values []any, reducer string) float64 {
	if reducer == reducerMean {
		return windowMean(values)
	}
	variance := windowVariance(values)
	if reducer == reducerStdDev {
		return math.Sqrt(variance)
	}

	return variance
}

func windowMean(values []any) float64 {
	total := 0.0
	count := 0
	for _, raw := range values {
		if raw == nil {
			continue
		}
		if number, valid := binaryNumber(raw); valid {
			total += number
			count++
		}
	}
	if count == 0 {
		return 0
	}

	return total / float64(count)
}

func windowVariance(values []any) float64 {
	if len(values) == 0 {
		return 0
	}
	mean := 0.0
	squareSum := 0.0
	count := 0
	for _, raw := range values {
		if raw == nil {
			continue
		}
		number, valid := binaryNumber(raw)
		if !valid {
			continue
		}
		count++
		oldMean := mean
		mean += (number - oldMean) / float64(count)
		squareSum += (number - oldMean) * (number - mean)
	}
	if count == 0 {
		return 0
	}
	variance := squareSum / float64(count)
	return variance
}

func calculateIndexFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	rowCount := frameRowCount(frame)
	values := make([]any, rowCount)
	for row := range values {
		values[row] = float64(row)
		if opts.Index.AsPercentile && rowCount > 0 {
			values[row] = float64(row) / float64(rowCount)
		}
	}

	name := opts.Alias
	if name == "" {
		name = "Row"
	}
	config := map[string]any{}
	if opts.Index.AsPercentile {
		config["unit"] = "percentunit"
	}
	if opts.Alias != "" {
		config["displayName"] = opts.Alias
	}
	field := DataFrameField{
		Name: name, Type: fieldTypeNumber, Labels: nil, Config: mustMarshal(config),
	}
	if opts.ReplaceFields {
		frame.Schema.Fields = []DataFrameField{field}
		frame.Data.Values = []json.RawMessage{mustMarshal(values)}
	} else {
		frame.Schema.Fields = append(frame.Schema.Fields, field)
		frame.Data.Values = append(frame.Data.Values, mustMarshal(values))
	}

	return true
}

func calculateBinaryByTypeFrame(frame *DataFrame, opts *calculateFieldOptions) bool {
	if opts.Binary.Left.Matcher.Options != fieldTypeNumber {
		return false
	}
	right, rightName, ok := binaryOperand(frame, opts.Binary.Right)
	if !ok {
		return false
	}

	originalFields := frame.Schema.Fields
	originalValues := frame.Data.Values
	fields, columns := binaryByTypeBaseFields(originalFields, originalValues, opts)
	calculated := appendBinaryByTypeFields(
		&fields, &columns, originalFields, originalValues, right, opts.Binary.Operator, rightName, opts.ReplaceFields,
	)
	frame.Schema.Fields = fields
	frame.Data.Values = columns

	return calculated
}

func binaryByTypeBaseFields(
	originalFields []DataFrameField,
	originalValues []json.RawMessage,
	opts *calculateFieldOptions,
) ([]DataFrameField, []json.RawMessage) {
	fields := make([]DataFrameField, 0, len(originalFields)*2)
	columns := make([]json.RawMessage, 0, len(originalValues)*2)
	if !opts.ReplaceFields {
		return fields, columns
	}
	for index, field := range originalFields {
		if (opts.TimeSeries == nil || *opts.TimeSeries) && field.Type == fieldTypeTime && index < len(originalValues) {
			fields = append(fields, field)
			columns = append(columns, originalValues[index])
			break
		}
	}

	return fields, columns
}

func appendBinaryByTypeFields(
	fields *[]DataFrameField,
	columns *[]json.RawMessage,
	originalFields []DataFrameField,
	originalValues []json.RawMessage,
	right []any,
	operator string,
	rightName string,
	replaceFields bool,
) bool {
	calculated := false
	for index, field := range originalFields {
		if !replaceFields {
			*fields = append(*fields, field)
			if index < len(originalValues) {
				*columns = append(*columns, originalValues[index])
			}
		}
		if field.Type != fieldTypeNumber || index >= len(originalValues) {
			continue
		}
		var left []any
		if err := json.Unmarshal(originalValues[index], &left); err != nil {
			continue
		}
		values := calculateBinaryValues(left, right, operator)
		field.Name = fmt.Sprintf("%s %s %s", field.Name, operator, rightName)
		*fields = append(*fields, field)
		*columns = append(*columns, mustMarshal(values))
		calculated = true
	}

	return calculated
}
