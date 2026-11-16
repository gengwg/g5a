package grafana

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"strconv"
)

const (
	joinModeOuter                  = "outer"
	transformationOrganize         = "organize"
	transformationJoinByField      = "joinByField"
	transformationGroupingToMatrix = "groupingToMatrix"
)

// ApplyTransformations applies a panel's transformations to its query
// result, modifying data frames in place.
//
//nolint:cyclop // transformation dispatch is centralized here
func ApplyTransformations(
	result *QueryResult,
	transformations []Transformation,
) {
	materializeLabelFields(
		result,
		requiredLabelFields(result, transformations),
		preserveLabelsForOrganize(transformations) && !splitsLabelsForJoin(transformations),
	)
	for _, t := range transformations {
		switch t.ID {
		case "filterFieldsByName":
			applyFilterFieldsByName(result, t.Options)
		case transformationOrganize:
			applyOrganize(result, t.Options)
		case transformationJoinByField:
			applyJoinByField(result, t.Options)
		case "filterByValue":
			applyFilterByValue(result, t.Options)
		case "convertFieldType":
			applyConvertFieldType(result, t.Options)
		case "calculateField":
			applyCalculateField(result, t.Options)
		case "reduce":
			applyReduce(result, t.Options)
		case transformationGroupingToMatrix:
			applyGroupingToMatrix(result, t.Options)
		case "sortBy":
			applySortBy(result, t.Options)
		case "merge":
			applyMerge(result)
		default:
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("unsupported transformation %q", t.ID))
			slog.Debug("unsupported transformation, skipping",
				"id", t.ID)
		}
	}
}

// joinByFieldOptions is the options for the joinByField transformation.
type joinByFieldOptions struct {
	ByField string `json:"byField"`
	Mode    string `json:"mode"`
}

type dataFrameJoin struct {
	byField      string
	frame        DataFrame
	columns      [][]any
	rowIndexes   map[string]int
	sourceRefIDs []string
	keyFieldSet  bool
}

func applyJoinByField(result *QueryResult, raw json.RawMessage) {
	var opts joinByFieldOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		slog.Debug("failed to parse joinByField options", "error", err)
		return
	}

	if opts.ByField == "" {
		return
	}

	if opts.Mode != "" && opts.Mode != joinModeOuter {
		slog.Debug("unsupported joinByField mode, skipping", "mode", opts.Mode)
		return
	}

	refIDs := make([]string, 0, len(result.Results))
	for refID := range result.Results {
		refIDs = append(refIDs, refID)
	}

	if len(refIDs) == 0 {
		return
	}

	slices.Sort(refIDs)

	join := newDataFrameJoin(opts.ByField, refIDs[0])

	for _, refID := range refIDs {
		rd := result.Results[refID]
		for i := range rd.Frames {
			join.addFrame(&rd.Frames[i], refID)
		}
	}

	result.Results = map[string]QueryResultData{
		refIDs[0]: {Frames: []DataFrame{join.dataFrame()}}, //nolint:exhaustruct // no query error metadata
	}
}

func newDataFrameJoin(byField, refID string) *dataFrameJoin {
	keyField := DataFrameField{
		Name: byField, Type: "", Labels: nil, Config: nil,
	}

	return &dataFrameJoin{
		byField: byField,
		frame: DataFrame{
			Schema: DataFrameSchema{
				RefID:  refID,
				Meta:   DataFrameMeta{Type: "", TypeVersion: nil},
				Fields: []DataFrameField{keyField},
			},
			Data: DataFrameData{Values: nil},
		},
		columns:      [][]any{{}},
		rowIndexes:   make(map[string]int),
		sourceRefIDs: []string{""},
		keyFieldSet:  false,
	}
}

func (j *dataFrameJoin) addFrame(frame *DataFrame, resultRefID string) {
	inputColumns := parseAllColumns(frame)
	keyIdx := findFieldIndex(frame, j.byField)
	if keyIdx >= 0 && keyIdx < len(inputColumns) {
		j.addFrameWithKeys(
			frame,
			inputColumns,
			frame.Schema.Fields[keyIdx],
			inputColumns[keyIdx],
			resultRefID,
		)
		return
	}

	// A wide frame can hold several series with different values for the
	// same join label. Split those series before joining so each field keeps
	// its own label/value association.
	for fieldIdx, field := range frame.Schema.Fields {
		labelValue, exists := field.Labels[j.byField]
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
		j.addFrameWithKeys(
			narrowFrame,
			narrowColumns,
			DataFrameField{Name: j.byField, Type: fieldTypeString, Labels: nil, Config: nil},
			keyValues,
			resultRefID,
		)
	}
}

func (j *dataFrameJoin) addFrameWithKeys(
	frame *DataFrame,
	inputColumns [][]any,
	keyField DataFrameField,
	keyValues []any,
	resultRefID string,
) {
	if !j.keyFieldSet {
		j.frame.Schema.Fields[0] = keyField
		j.keyFieldSet = true
	}

	refID := frame.Schema.RefID
	if refID == "" {
		refID = resultRefID
	}
	columnIndexes := j.addColumns(frame, inputColumns, refID)
	for rowIdx, keyValue := range keyValues {
		j.addRow(keyValue, rowIdx, inputColumns, columnIndexes)
	}
}

func rawColumns(columns [][]any) []json.RawMessage {
	values := make([]json.RawMessage, len(columns))
	for index, column := range columns {
		values[index] = mustMarshal(column)
	}

	return values
}

func (j *dataFrameJoin) addColumns(
	frame *DataFrame, inputColumns [][]any, refID string,
) map[int]int {
	columnIndexes := make(map[int]int, len(frame.Schema.Fields)-1)

	for fieldIdx, field := range frame.Schema.Fields {
		if field.Name == j.byField || fieldIdx >= len(inputColumns) {
			continue
		}

		column := make([]any, len(j.columns[0]))
		j.columns = append(j.columns, column)
		j.frame.Schema.Fields = append(j.frame.Schema.Fields, field)
		j.sourceRefIDs = append(j.sourceRefIDs, refID)
		columnIndexes[fieldIdx] = len(j.columns) - 1
	}

	return columnIndexes
}

func (j *dataFrameJoin) addRow(
	keyValue any, sourceRow int, inputColumns [][]any,
	columnIndexes map[int]int,
) {
	key := joinRowKey(keyValue)
	joinedRow, ok := j.rowIndexes[key]

	if !ok {
		joinedRow = len(j.columns[0])
		j.rowIndexes[key] = joinedRow

		for columnIdx := range j.columns {
			j.columns[columnIdx] = append(j.columns[columnIdx], nil)
		}

		j.columns[0][joinedRow] = keyValue
	}

	for fieldIdx, columnIdx := range columnIndexes {
		if sourceRow < len(inputColumns[fieldIdx]) {
			j.columns[columnIdx][joinedRow] = inputColumns[fieldIdx][sourceRow]
		}
	}
}

func (j *dataFrameJoin) dataFrame() DataFrame {
	nameCounts := make(map[string]int, len(j.frame.Schema.Fields))
	for _, field := range j.frame.Schema.Fields[1:] {
		nameCounts[field.Name]++
	}

	for i := 1; i < len(j.frame.Schema.Fields); i++ {
		field := &j.frame.Schema.Fields[i]
		if nameCounts[field.Name] > 1 {
			field.Name += " #" + j.sourceRefIDs[i]
		}
	}

	j.frame.Data.Values = make([]json.RawMessage, len(j.columns))
	for i, column := range j.columns {
		j.frame.Data.Values[i] = mustMarshal(column)
	}
	j.coalesceDuplicateFields()

	return j.frame
}

func (j *dataFrameJoin) coalesceDuplicateFields() {
	fields := make([]DataFrameField, 0, len(j.frame.Schema.Fields))
	columns := make([]json.RawMessage, 0, len(j.frame.Data.Values))
	indexes := make(map[string]int, len(j.frame.Schema.Fields))

	for index, field := range j.frame.Schema.Fields {
		if existing, duplicate := indexes[field.Name]; duplicate {
			var left, right []any
			if err := json.Unmarshal(columns[existing], &left); err != nil {
				continue
			}
			if err := json.Unmarshal(j.frame.Data.Values[index], &right); err != nil {
				continue
			}
			for row := range left {
				if row < len(right) && left[row] == nil && right[row] != nil {
					left[row] = right[row]
				}
			}
			columns[existing] = mustMarshal(left)
			continue
		}

		indexes[field.Name] = len(fields)
		fields = append(fields, field)
		columns = append(columns, j.frame.Data.Values[index])
	}

	j.frame.Schema.Fields = fields
	j.frame.Data.Values = columns
}

func joinRowKey(value any) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		return "string:" + value
	case float64:
		if value == 0 {
			value = 0 // Normalize negative zero.
		}

		return "number:" + strconv.FormatFloat(value, 'g', -1, 64)
	default:
		return "other:" + string(mustMarshal(value))
	}
}

// filterFieldsByNameOptions is the options for the filterFieldsByName
// transformation.
type filterFieldsByNameOptions struct {
	Include *fieldNameMatcher `json:"include,omitempty"`
	Exclude *fieldNameMatcher `json:"exclude,omitempty"`
}

type fieldNameMatcher struct {
	Names   []string `json:"names,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
}

func applyFilterFieldsByName(
	result *QueryResult, raw json.RawMessage,
) {
	var opts filterFieldsByNameOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		slog.Debug("failed to parse filterFieldsByName options",
			"error", err)
		return
	}

	for refID, rd := range result.Results {
		for i := range rd.Frames {
			filterFrameFields(&rd.Frames[i], &opts)
		}

		result.Results[refID] = rd
	}
}

func filterFrameFields(frame *DataFrame, opts *filterFieldsByNameOptions) {
	keep := make([]bool, len(frame.Schema.Fields))

	for i, field := range frame.Schema.Fields {
		keep[i] = shouldKeepField(field.Name, opts)
	}

	applyFieldFilter(frame, keep)
}

func shouldKeepField(name string, opts *filterFieldsByNameOptions) bool {
	if opts.Include != nil {
		return matchesFieldName(name, opts.Include)
	}

	if opts.Exclude != nil {
		return !matchesFieldName(name, opts.Exclude)
	}

	return true
}

func matchesFieldName(name string, matcher *fieldNameMatcher) bool {
	if slices.Contains(matcher.Names, name) {
		return true
	}

	if matcher.Pattern != "" {
		if re, err := regexp.Compile(matcher.Pattern); err == nil {
			return re.MatchString(name)
		}
	}

	return false
}

// organizeOptions is the options for the organize transformation.
type organizeOptions struct {
	ExcludeByName map[string]bool   `json:"excludeByName,omitempty"`
	RenameByName  map[string]string `json:"renameByName,omitempty"`
	IndexByName   map[string]int    `json:"indexByName,omitempty"`
}

func applyOrganize(result *QueryResult, raw json.RawMessage) {
	var opts organizeOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		slog.Debug("failed to parse organize options", "error", err)
		return
	}

	for refID, rd := range result.Results {
		for i := range rd.Frames {
			organizeFrame(&rd.Frames[i], &opts)
		}

		result.Results[refID] = rd
	}
}

func organizeFrame(frame *DataFrame, opts *organizeOptions) {
	// Apply exclusions.
	if len(opts.ExcludeByName) > 0 {
		keep := make([]bool, len(frame.Schema.Fields))

		for i, field := range frame.Schema.Fields {
			keep[i] = !opts.ExcludeByName[field.Name]
		}

		applyFieldFilter(frame, keep)
	}

	// Apply renames.
	for i, field := range frame.Schema.Fields {
		if newName, ok := opts.RenameByName[field.Name]; ok && newName != "" {
			frame.Schema.Fields[i].Name = newName
			frame.Schema.Fields[i].Config = withoutDatasourceDisplayName(field.Config)
		}
	}

	if len(opts.IndexByName) > 0 {
		orderFrameFields(frame, opts.IndexByName)
	}
}

func withoutDatasourceDisplayName(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return raw
	}
	delete(config, "displayNameFromDS")
	updated, err := json.Marshal(config)
	if err != nil {
		return raw
	}

	return updated
}

//nolint:cyclop // organize ordering handles sparse indices and unspecified fields
func orderFrameFields(frame *DataFrame, indexByName map[string]int) {
	order := make([]int, len(frame.Schema.Fields))
	for i := range order {
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int {
		aIndex, aIndexed := indexByName[frame.Schema.Fields[a].Name]
		bIndex, bIndexed := indexByName[frame.Schema.Fields[b].Name]

		switch {
		case aIndexed && bIndexed && aIndex < bIndex:
			return -1
		case aIndexed && bIndexed && aIndex > bIndex:
			return 1
		case aIndexed && !bIndexed:
			return -1
		case !aIndexed && bIndexed:
			return 1
		default:
			return 0
		}
	})

	fields := make([]DataFrameField, 0, len(order))
	values := make([]json.RawMessage, 0, len(frame.Data.Values))

	for _, fieldIdx := range order {
		fields = append(fields, frame.Schema.Fields[fieldIdx])
		if fieldIdx < len(frame.Data.Values) {
			values = append(values, frame.Data.Values[fieldIdx])
		}
	}

	frame.Schema.Fields = fields
	frame.Data.Values = values
}

// convertFieldTypeOptions is the options for the convertFieldType
// transformation.
type convertFieldTypeOptions struct {
	Conversions []fieldTypeConversion `json:"conversions"`
}

type fieldTypeConversion struct {
	TargetField     string `json:"targetField"`
	DestinationType string `json:"destinationType"`
}

func applyConvertFieldType(result *QueryResult, raw json.RawMessage) {
	var opts convertFieldTypeOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		slog.Debug("failed to parse convertFieldType options",
			"error", err)
		return
	}

	for refID, rd := range result.Results {
		for i := range rd.Frames {
			for _, conv := range opts.Conversions {
				convertField(&rd.Frames[i], conv)
			}
		}

		result.Results[refID] = rd
	}
}

func convertField(frame *DataFrame, conv fieldTypeConversion) {
	fieldIdx := findFieldIndex(frame, conv.TargetField)
	if fieldIdx < 0 || fieldIdx >= len(frame.Data.Values) {
		return
	}

	if conv.DestinationType != fieldTypeNumber {
		slog.Debug("unsupported field type conversion",
			"field", conv.TargetField,
			"destinationType", conv.DestinationType)
		return
	}

	// Parse string values and convert to numbers.
	var stringValues []any
	if err := json.Unmarshal(frame.Data.Values[fieldIdx], &stringValues); err != nil {
		return
	}

	numbers := make([]*float64, len(stringValues))

	for i, v := range stringValues {
		str, ok := v.(string)
		if !ok || str == "" {
			continue
		}

		num, err := strconv.ParseFloat(str, 64)
		if err != nil {
			continue
		}

		numbers[i] = &num
	}

	data, err := json.Marshal(numbers)
	if err != nil {
		return
	}

	frame.Data.Values[fieldIdx] = data
	frame.Schema.Fields[fieldIdx].Type = fieldTypeNumber
}

const matchAll = "all"

// filterByValueOptions is the options for the filterByValue
// transformation.
type filterByValueOptions struct {
	Type    string             `json:"type"`  // "include" or "exclude"
	Match   string             `json:"match"` // "any" or "all"
	Filters []valueFilterEntry `json:"filters"`
}

type valueFilterEntry struct {
	FieldName string            `json:"fieldName"`
	Config    valueFilterConfig `json:"config"`
}

type valueFilterConfig struct {
	ID      string          `json:"id"`
	Options json.RawMessage `json:"options"`
}

func applyFilterByValue(result *QueryResult, raw json.RawMessage) {
	var opts filterByValueOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		slog.Debug("failed to parse filterByValue options",
			"error", err)
		return
	}

	for refID, rd := range result.Results {
		for i := range rd.Frames {
			filterFrameRows(&rd.Frames[i], &opts)
		}

		result.Results[refID] = rd
	}
}

func filterFrameRows(frame *DataFrame, opts *filterByValueOptions) {
	if len(opts.Filters) == 0 || len(frame.Schema.Fields) == 0 {
		return
	}

	rowCount := frameRowCount(frame)
	if rowCount == 0 {
		return
	}

	// Parse all columns into generic slices for row-level access.
	columns := parseAllColumns(frame)
	if len(columns) == 0 {
		return
	}

	keepRows := make([]bool, rowCount)

	for row := range rowCount {
		matches := evaluateRowFilters(
			frame, columns, row, opts.Filters, opts.Match,
		)

		switch opts.Type {
		case "exclude":
			keepRows[row] = !matches
		default: // "include"
			keepRows[row] = matches
		}
	}

	rebuildFrameData(frame, columns, keepRows)
}

// evaluateRowFilters checks if a row matches the filter conditions.
func evaluateRowFilters(
	frame *DataFrame,
	columns [][]any,
	row int,
	filters []valueFilterEntry,
	matchMode string,
) bool {
	for _, f := range filters {
		fieldIdx := findFieldIndex(frame, f.FieldName)
		if fieldIdx < 0 || fieldIdx >= len(columns) {
			continue
		}

		val := columns[fieldIdx][row]
		matched := evaluateCondition(val, f.Config)

		if matchMode == matchAll && !matched {
			return false
		}

		if matchMode != "all" && matched {
			return true
		}
	}

	// "all" mode: all matched. "any" mode: none matched.
	return matchMode == matchAll
}

// evaluateCondition checks a single value against a filter condition.
func evaluateCondition(val any, config valueFilterConfig) bool {
	num, ok := toFloat64(val)
	if !ok {
		return false
	}

	threshold := parseThreshold(config.Options)

	switch config.ID {
	case "greater":
		return num > threshold
	case "greaterOrEqual":
		return num >= threshold
	case "lower":
		return num < threshold
	case "lowerOrEqual":
		return num <= threshold
	case "equal":
		return num == threshold
	case "notEqual":
		return num != threshold
	default:
		slog.Debug("unsupported filterByValue condition",
			"id", config.ID)
		return false
	}
}

func parseThreshold(raw json.RawMessage) float64 {
	var opts struct {
		Value json.RawMessage `json:"value"`
	}

	if err := json.Unmarshal(raw, &opts); err != nil {
		return 0
	}

	// Try as number first, then as string.
	var num float64
	if err := json.Unmarshal(opts.Value, &num); err == nil {
		return num
	}

	var str string
	if err := json.Unmarshal(opts.Value, &str); err == nil {
		if v, parseErr := strconv.ParseFloat(str, 64); parseErr == nil {
			return v
		}
	}

	return 0
}

func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		if math.IsNaN(v) {
			return 0, false
		}

		return v, true
	case nil:
		return 0, false
	default:
		return 0, false
	}
}

func findFieldIndex(frame *DataFrame, name string) int {
	for i, field := range frame.Schema.Fields {
		if field.Name == name {
			return i
		}
	}

	return -1
}

func frameRowCount(frame *DataFrame) int {
	if len(frame.Data.Values) == 0 {
		return 0
	}

	// Parse the first column to determine length.
	var values []any
	if err := json.Unmarshal(frame.Data.Values[0], &values); err != nil {
		return 0
	}

	return len(values)
}

func parseAllColumns(frame *DataFrame) [][]any {
	columns := make([][]any, len(frame.Data.Values))

	for i, raw := range frame.Data.Values {
		var values []any
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil
		}

		columns[i] = values
	}

	return columns
}

func rebuildFrameData(
	frame *DataFrame, columns [][]any, keepRows []bool,
) {
	for colIdx := range columns {
		var filtered []any

		for row, keep := range keepRows {
			if keep && row < len(columns[colIdx]) {
				filtered = append(filtered, columns[colIdx][row])
			}
		}

		data, err := json.Marshal(filtered)
		if err != nil {
			continue
		}

		frame.Data.Values[colIdx] = data
	}
}

// reduceOptions is the options for the reduce transformation.
type reduceOptions struct {
	Reducers         []string `json:"reducers"`
	Mode             string   `json:"mode"`
	IncludeTimeField bool     `json:"includeTimeField"`
}

func applyReduce(result *QueryResult, raw json.RawMessage) {
	var opts reduceOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		slog.Debug("failed to parse reduce options", "error", err)
		return
	}

	if len(opts.Reducers) == 0 {
		return
	}

	for refID, rd := range result.Results {
		for i := range rd.Frames {
			switch opts.Mode {
			case "reduceFields":
				reduceFields(&rd.Frames[i], opts)
			default: // "seriesToRows" is the default
				reduceSeriesToRows(&rd.Frames[i], opts)
			}
		}

		result.Results[refID] = rd
	}
}

// reduceFields reduces each numeric field to a single value per
// reducer, keeping the same field structure but with one row.
func reduceFields(frame *DataFrame, opts reduceOptions) {
	for fieldIdx, field := range frame.Schema.Fields {
		if field.Type == fieldTypeTime && !opts.IncludeTimeField {
			continue
		}

		if field.Type != fieldTypeNumber {
			continue
		}

		values, err := parseNumberValues(frame.Data.Values[fieldIdx])
		if err != nil || len(values) == 0 {
			continue
		}

		// Apply first reducer (most common case is a single reducer).
		reduced := applyReducer(values, opts.Reducers[0])

		data, marshalErr := json.Marshal([]float64{reduced})
		if marshalErr != nil {
			continue
		}

		frame.Data.Values[fieldIdx] = data
	}

	// Remove time fields if not included.
	if !opts.IncludeTimeField {
		keep := make([]bool, len(frame.Schema.Fields))

		for i, f := range frame.Schema.Fields {
			keep[i] = f.Type != fieldTypeTime
		}

		applyFieldFilter(frame, keep)
	}
}

// reduceSeriesToRows converts each numeric field into a row in a new
// two-column frame: "Field" (string) and the reducer name (number).
func reduceSeriesToRows(frame *DataFrame, opts reduceOptions) {
	reducerName := opts.Reducers[0]

	var fieldNames []string

	var reducedValues []float64

	for fieldIdx, field := range frame.Schema.Fields {
		if field.Type != fieldTypeNumber {
			continue
		}

		values, err := parseNumberValues(frame.Data.Values[fieldIdx])
		if err != nil || len(values) == 0 {
			continue
		}

		fieldNames = append(fieldNames, field.Name)
		reducedValues = append(reducedValues, applyReducer(values, reducerName))
	}

	namesJSON := mustMarshal(fieldNames)
	valuesJSON := mustMarshal(reducedValues)

	frame.Schema.Fields = []DataFrameField{
		{Name: "Field", Type: fieldTypeString},     //nolint:exhaustruct // no labels/config
		{Name: reducerName, Type: fieldTypeNumber}, //nolint:exhaustruct // no labels/config
	}
	frame.Data.Values = []json.RawMessage{namesJSON, valuesJSON}
}

// applyReducer computes a single value from a slice using the named
// reducer function.
func applyReducer(values []float64, reducer string) float64 {
	switch reducer {
	case reducerLast:
		return values[len(values)-1]
	case reducerFirst:
		return values[0]
	case reducerMin:
		return reduceMin(values)
	case reducerMax:
		return reduceMax(values)
	case reducerMean:
		return reduceMean(values)
	case reducerSum:
		return reduceSum(values)
	case reducerCount:
		return float64(len(values))
	default:
		slog.Debug("unsupported reducer, skipping", "reducer", reducer)
		return math.NaN()
	}
}

func reduceMin(values []float64) float64 {
	result := math.Inf(1)

	for _, v := range values {
		if !math.IsNaN(v) && v < result {
			result = v
		}
	}

	return result
}

func reduceMax(values []float64) float64 {
	result := math.Inf(-1)

	for _, v := range values {
		if !math.IsNaN(v) && v > result {
			result = v
		}
	}

	return result
}

func reduceMean(values []float64) float64 {
	sum := 0.0
	count := 0

	for _, v := range values {
		if !math.IsNaN(v) {
			sum += v
			count++
		}
	}

	if count == 0 {
		return math.NaN()
	}

	return sum / float64(count)
}

func reduceSum(values []float64) float64 {
	sum := 0.0

	for _, v := range values {
		if !math.IsNaN(v) {
			sum += v
		}
	}

	return sum
}

// applyFieldFilter removes fields (and their corresponding data
// columns) where keep[i] is false.
func applyFieldFilter(frame *DataFrame, keep []bool) {
	var newFields []DataFrameField

	var newValues []json.RawMessage

	for i, k := range keep {
		if !k {
			continue
		}

		newFields = append(newFields, frame.Schema.Fields[i])

		if i < len(frame.Data.Values) {
			newValues = append(newValues, frame.Data.Values[i])
		}
	}

	frame.Schema.Fields = newFields
	frame.Data.Values = newValues
}
