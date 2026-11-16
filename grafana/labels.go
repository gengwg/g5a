package grafana

import (
	"encoding/json"
	"regexp/syntax"
	"slices"
	"strings"
)

func requiredLabelFields(
	result *QueryResult,
	transformations []Transformation,
) map[string]bool {
	candidates := labelFieldRules(transformations)
	preserveAll := preserveLabelsForOrganize(transformations)
	required := make(map[string]bool)
	for _, data := range result.Results {
		for _, frame := range data.Frames {
			for _, field := range frame.Schema.Fields {
				for name := range field.Labels {
					if preserveAll || slices.Contains(candidates, name) {
						required[name] = true
					}
				}
			}
		}
	}

	return required
}

func preserveLabelsForOrganize(transformations []Transformation) bool {
	for _, transformation := range transformations {
		if transformation.ID == transformationOrganize {
			return true
		}
	}

	return false
}

func splitsLabelsForJoin(transformations []Transformation) bool {
	for _, transformation := range transformations {
		if transformation.ID == transformationJoinByField ||
			transformation.ID == transformationGroupingToMatrix {
			return true
		}
	}

	return false
}

func labelFieldRules(transformations []Transformation) []string {
	required := make(map[string]bool)
	for _, transformation := range slices.Backward(transformations) {
		collectLabelReferences(transformation, required)
	}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

//nolint:cyclop // field-reference discovery is dispatched by transformation type
func collectLabelReferences(transformation Transformation, required map[string]bool) {
	switch transformation.ID {
	case transformationJoinByField:
		var options joinByFieldOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil {
			addLabelReference(required, options.ByField)
		}
	case transformationGroupingToMatrix:
		var options groupingToMatrixOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil {
			addLabelReference(required, options.RowField, options.ColumnField)
		}
	case "filterFieldsByName":
		var options filterFieldsByNameOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil && options.Include != nil {
			addLabelReference(required, options.Include.Names...)
			addLabelReference(required, explicitPatternNames(options.Include.Pattern)...)
		}
	case "sortBy":
		var options sortByOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil {
			for _, rule := range options.Sort {
				addLabelReference(required, rule.Field)
			}
		}
	case transformationFilterByValue:
		var options filterByValueOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil {
			for _, filter := range options.Filters {
				addLabelReference(required, filter.FieldName)
			}
		}
	case "convertFieldType":
		var options convertFieldTypeOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil {
			for _, conversion := range options.Conversions {
				addLabelReference(required, conversion.TargetField)
			}
		}
	case transformationOrganize:
		collectOrganizeReferences(transformation.Options, required)
	}
}

func collectOrganizeReferences(raw json.RawMessage, required map[string]bool) {
	var options organizeOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return
	}
	for name := range options.IndexByName {
		addLabelReference(required, name)
	}
	for oldName, newName := range options.RenameByName {
		addLabelReference(required, newName)
		addLabelReference(required, oldName)
	}
	for name, excluded := range options.ExcludeByName {
		if excluded {
			addLabelReference(required, name)
		}
	}
}

func addLabelReference(required map[string]bool, names ...string) {
	for _, name := range names {
		if name != "" {
			required[name] = true
		}
	}
}

func explicitPatternNames(pattern string) []string {
	if pattern == "" {
		return nil
	}
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil
	}

	return exactPatternAlternatives(expression)
}

func exactPatternAlternatives(expression *syntax.Regexp) []string {
	if expression.Op == syntax.OpCapture {
		return exactPatternAlternatives(expression.Sub[0])
	}
	if expression.Op == syntax.OpConcat {
		return exactConcatAlternatives(expression)
	}
	if expression.Op != syntax.OpAlternate {
		name, exact := exactPatternString(expression)
		if exact {
			return []string{name}
		}
		return nil
	}

	names := make([]string, 0, len(expression.Sub))
	for _, branch := range expression.Sub {
		if name, exact := exactPatternString(branch); exact {
			names = append(names, name)
		}
	}

	return names
}

func exactConcatAlternatives(expression *syntax.Regexp) []string {
	var significant *syntax.Regexp
	for _, part := range expression.Sub {
		if isPatternAnchor(part.Op) {
			continue
		}
		if significant != nil {
			return nil
		}
		significant = part
	}
	if significant == nil {
		return nil
	}

	return exactPatternAlternatives(significant)
}

//nolint:exhaustive // non-literal regexp operators are intentionally not exact matches
func exactPatternString(expression *syntax.Regexp) (string, bool) {
	switch expression.Op {
	case syntax.OpLiteral:
		if expression.Flags&syntax.FoldCase != 0 {
			return "", false
		}
		return string(expression.Rune), true
	case syntax.OpCapture:
		return exactPatternString(expression.Sub[0])
	case syntax.OpBeginText, syntax.OpEndText, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpEmptyMatch:
		return "", true
	case syntax.OpConcat:
		return exactPatternConcat(expression)
	default:
		return "", false
	}
}

func isPatternAnchor(op syntax.Op) bool {
	return slices.Contains([]syntax.Op{
		syntax.OpBeginText, syntax.OpEndText, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpEmptyMatch,
	}, op)
}

func exactPatternConcat(expression *syntax.Regexp) (string, bool) {
	var name strings.Builder
	for _, part := range expression.Sub {
		text, exact := exactPatternString(part)
		if !exact {
			return "", false
		}
		name.WriteString(text)
	}

	return name.String(), true
}

// materializeLabelFields expands labels required by the transformation pipeline.
// Organize preserves all labels; selective transformations keep unrelated labels
// attached to their fields instead of creating sparse table columns.
func materializeLabelFields(result *QueryResult, required map[string]bool, unify bool) {
	if len(required) == 0 {
		return
	}
	for refID, data := range result.Results {
		frames := make([]DataFrame, 0, len(data.Frames))
		for _, frame := range data.Frames {
			if unify {
				frames = append(frames, materializeFrameLabelsUnified(frame, required))
				continue
			}
			frames = append(frames, materializeFrameLabels(frame, required)...)
		}
		data.Frames = frames
		result.Results[refID] = data
	}
}

//nolint:cyclop // unified expansion preserves field and value ordering
func materializeFrameLabelsUnified(frame DataFrame, required map[string]bool) DataFrame {
	shared := sharedRequiredLabels(frame, required)
	if len(shared) == 0 {
		return frame
	}
	names := make([]string, 0, len(shared))
	for name := range shared {
		names = append(names, name)
	}
	slices.Sort(names)

	baseFields := frame.Schema.Fields
	baseValues := frame.Data.Values
	fields := make([]DataFrameField, 0, len(baseFields)+len(names))
	values := make([]json.RawMessage, 0, len(baseValues)+len(names))
	for index, field := range baseFields {
		if len(field.Labels) == 0 && index < len(baseValues) {
			fields = append(fields, field)
			values = append(values, baseValues[index])
		}
	}
	rowCount := frameRowCount(&frame)
	for _, name := range names {
		column := make([]any, rowCount)
		for row := range column {
			column[row] = shared[name]
		}
		fields = append(fields, DataFrameField{Name: name, Type: fieldTypeString, Labels: nil, Config: nil})
		values = append(values, mustMarshal(column))
	}
	for index, field := range baseFields {
		if len(field.Labels) > 0 && index < len(baseValues) {
			fields = append(fields, field)
			values = append(values, baseValues[index])
		}
	}
	frame.Schema.Fields = fields
	frame.Data.Values = values

	return frame
}

func sharedRequiredLabels(frame DataFrame, required map[string]bool) map[string]string {
	shared := make(map[string]string)
	first := true
	for _, field := range frame.Schema.Fields {
		if field.Type != fieldTypeNumber || len(field.Labels) == 0 {
			continue
		}
		if first {
			for name, value := range field.Labels {
				if required[name] {
					shared[name] = value
				}
			}
			first = false
			continue
		}
		for name, value := range shared {
			if field.Labels[name] != value {
				delete(shared, name)
			}
		}
	}

	return shared
}

//nolint:cyclop // wide-frame series expansion has several small layout cases
func materializeFrameLabels(frame DataFrame, required map[string]bool) []DataFrame {
	baseFields := frame.Schema.Fields
	baseValues := frame.Data.Values
	expanded := make([]DataFrame, 0, len(baseFields))

	for fieldIndex, labeledField := range baseFields {
		if !hasRequiredLabel(labeledField.Labels, required) || fieldIndex >= len(baseValues) {
			continue
		}

		fields := make([]DataFrameField, 0, len(baseFields)+len(required))
		values := make([]json.RawMessage, 0, len(baseValues)+len(required))
		for index, field := range baseFields {
			if hasRequiredLabel(field.Labels, required) || index >= len(baseValues) {
				continue
			}
			fields = append(fields, field)
			values = append(values, baseValues[index])
		}
		labelNames := make([]string, 0, len(required))
		for name := range labeledField.Labels {
			if required[name] {
				labelNames = append(labelNames, name)
			}
		}
		slices.Sort(labelNames)

		existing := make(map[string]bool, len(fields)+1)
		for _, field := range fields {
			existing[field.Name] = true
		}
		existing[labeledField.Name] = true
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
		fields = append(fields, labeledField)
		values = append(values, baseValues[fieldIndex])

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

func hasRequiredLabel(labels map[string]string, required map[string]bool) bool {
	for name := range labels {
		if required[name] {
			return true
		}
	}

	return false
}
