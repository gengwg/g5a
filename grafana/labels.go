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
	required := make(map[string]bool)
	for _, data := range result.Results {
		for _, frame := range data.Frames {
			for _, field := range frame.Schema.Fields {
				for name := range field.Labels {
					if slices.Contains(candidates, name) {
						required[name] = true
					}
				}
			}
		}
	}

	return required
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
	case "joinByField":
		var options joinByFieldOptions
		if err := json.Unmarshal(transformation.Options, &options); err == nil {
			addLabelReference(required, options.ByField)
		}
	case "groupingToMatrix":
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
	case "filterByValue":
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
	case "organize":
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

// materializeLabelFields expands only labels needed by a transformation. Other
// labels stay on their fields instead of becoming sparse table columns.
func materializeLabelFields(result *QueryResult, required map[string]bool) {
	if len(required) == 0 {
		return
	}
	for refID, data := range result.Results {
		frames := make([]DataFrame, 0, len(data.Frames))
		for _, frame := range data.Frames {
			frames = append(frames, materializeFrameLabels(frame, required)...)
		}
		data.Frames = frames
		result.Results[refID] = data
	}
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
		fields = append(fields, labeledField)
		values = append(values, baseValues[fieldIndex])

		labelNames := make([]string, 0, len(required))
		for name := range labeledField.Labels {
			if required[name] {
				labelNames = append(labelNames, name)
			}
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

func hasRequiredLabel(labels map[string]string, required map[string]bool) bool {
	for name := range labels {
		if required[name] {
			return true
		}
	}

	return false
}
