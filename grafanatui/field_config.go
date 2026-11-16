package grafanatui

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/gengwg/g5a/grafana"
)

// fieldPresentation contains the renderer-independent display properties for
// one field value.
type fieldPresentation struct {
	Text        string
	DisplayName string
	Color       string
	Background  bool
	Width       int
	Align       string
}

type resolvedFieldConfig struct {
	displayName string
	unit        string
	decimals    *int
	noValue     string
	mappings    []fieldMapping
	thresholds  *thresholdConfig
	min         *float64
	max         *float64
	cellOptions json.RawMessage
	width       int
	align       string
}

type fieldMapping struct {
	Type    string          `json:"type"`
	Options json.RawMessage `json:"options"`
}

type fieldMappingResult struct {
	Text  *string `json:"text"`
	Color string  `json:"color"`
}

type fieldConfigOverride struct {
	Matcher struct {
		ID      string          `json:"id"`
		Options json.RawMessage `json:"options"`
	} `json:"matcher"`
	Properties []fieldConfigProperty `json:"properties"`
}

type fieldConfigProperty struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value"`
}

// presentField resolves panel field configuration and applies it to a value.
func presentField(
	raw json.RawMessage,
	field grafana.DataFrameField,
	value any,
) fieldPresentation {
	config := resolveFieldConfig(field.Config, field.Name)
	config = applyFieldConfig(config, raw, field.Name)
	displayName := config.displayName
	if displayName == "" {
		displayName = compactDatasourceName(datasourceDisplayName(field.Config))
	}
	if displayName == "" {
		displayName = field.Name
	}

	for name, labelValue := range field.Labels {
		displayName = strings.ReplaceAll(
			displayName,
			"${__field.labels."+name+"}",
			labelValue,
		)
	}

	presentation := fieldPresentation{
		Text:        formatFieldValue(value, config),
		DisplayName: displayName,
		Color:       "",
		Background:  isColorBackground(config.cellOptions),
		Width:       config.width,
		Align:       config.align,
	}

	if mapping, ok := matchFieldMapping(config.mappings, value); ok {
		if mapping.Text != nil {
			presentation.Text = *mapping.Text
		}

		presentation.Color = resolveGrafanaColor(mapping.Color)
	}

	if presentation.Color == "" {
		if number, ok := fieldNumber(value); ok {
			presentation.Color = fieldThresholdColor(config, number)
		}
	}

	return presentation
}

func fieldCategoryName(field grafana.DataFrameField, value any) string {
	configuredName := resolveFieldConfig(field.Config, field.Name).displayName
	presentation := presentField(nil, field, value)
	if configuredName != "" {
		return presentation.DisplayName
	}
	if name, ok := preferredCategoryLabel(field.Labels, ""); ok {
		return name
	}

	return presentation.DisplayName
}

func compactDatasourceName(name string) string {
	name = strings.TrimSpace(name)
	open := strings.Index(name, "{")
	if open < 0 {
		return name
	}
	if open > 0 {
		return name[:open]
	}

	if value, ok := firstSeriesLabelValue(name); ok {
		return value
	}

	return name
}

func datasourceDisplayName(raw json.RawMessage) string {
	var config struct {
		DisplayNameFromDS string `json:"displayNameFromDS"` //nolint:tagliatelle // Grafana API capitalization
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return ""
	}

	return config.DisplayNameFromDS
}

//nolint:exhaustruct // zero-value config is the baseline
func resolveFieldConfig(raw json.RawMessage, fieldName string) resolvedFieldConfig {
	return applyFieldConfig(
		resolvedFieldConfig{},
		raw,
		fieldName,
	)
}

func applyFieldConfig(
	config resolvedFieldConfig,
	raw json.RawMessage,
	fieldName string,
) resolvedFieldConfig {
	if len(raw) == 0 {
		return config
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return config
	}

	if defaults, ok := root["defaults"]; ok {
		applyFieldProperties(&config, defaults)
	} else {
		// Data frame field configs contain properties directly, while panel
		// configs wrap them in defaults.
		applyFieldProperties(&config, raw)
	}

	overridesJSON, ok := root["overrides"]
	if !ok {
		return config
	}

	var overrides []fieldConfigOverride
	if err := json.Unmarshal(overridesJSON, &overrides); err != nil {
		return config
	}

	for _, override := range overrides {
		if !fieldOverrideMatches(override, fieldName) {
			continue
		}

		for _, property := range override.Properties {
			applyFieldProperty(&config, property.ID, property.Value)
		}
	}

	return config
}

func fieldOverrideMatches(override fieldConfigOverride, fieldName string) bool {
	var option string
	if err := json.Unmarshal(override.Matcher.Options, &option); err != nil {
		return false
	}

	switch override.Matcher.ID {
	case "byName":
		return option == fieldName
	case "byRegexp":
		pattern, err := regexp.Compile(option)
		return err == nil && pattern.MatchString(fieldName)
	default:
		return false
	}
}

func applyFieldProperties(config *resolvedFieldConfig, raw json.RawMessage) {
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(raw, &properties); err != nil {
		return
	}

	for id, value := range properties {
		if id == "custom" {
			applyCustomFieldProperties(config, value)
			continue
		}

		applyFieldProperty(config, id, value)
	}
}

func applyCustomFieldProperties(config *resolvedFieldConfig, raw json.RawMessage) {
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(raw, &properties); err != nil {
		return
	}

	for id, value := range properties {
		applyFieldProperty(config, "custom."+id, value)
	}
}

//nolint:gocyclo,cyclop,funlen // Grafana field properties are dispatched in one place
func applyFieldProperty(
	config *resolvedFieldConfig,
	id string,
	raw json.RawMessage,
) {
	isNull := strings.TrimSpace(string(raw)) == "null"

	switch id {
	case "displayName":
		applyStringProperty(raw, isNull, &config.displayName)
	case "unit":
		applyStringProperty(raw, isNull, &config.unit)
	case "noValue":
		applyStringProperty(raw, isNull, &config.noValue)
	case "decimals":
		if isNull {
			config.decimals = nil
			return
		}

		if number, ok := fieldJSONNumber(raw); ok &&
			number >= 0 && number == math.Trunc(number) {
			decimals := int(number)
			config.decimals = &decimals
		}
	case "mappings":
		if isNull {
			config.mappings = nil
			return
		}

		var mappings []fieldMapping
		if err := json.Unmarshal(raw, &mappings); err == nil {
			config.mappings = mappings
		}
	case "thresholds":
		if isNull {
			config.thresholds = nil
			return
		}

		var thresholds thresholdConfig
		if err := json.Unmarshal(raw, &thresholds); err == nil {
			config.thresholds = &thresholds
		}
	case calcMin:
		config.min = fieldNumberProperty(raw, isNull)
	case calcMax:
		config.max = fieldNumberProperty(raw, isNull)
	case "custom.cellOptions":
		if isNull {
			config.cellOptions = nil
			return
		}

		var options map[string]json.RawMessage
		if err := json.Unmarshal(raw, &options); err == nil {
			config.cellOptions = raw
		}
	case "custom.width":
		if isNull {
			config.width = 0
			return
		}

		if width, ok := fieldJSONNumber(raw); ok {
			config.width = int(width)
		}
	case "custom.align":
		applyStringProperty(raw, isNull, &config.align)
	}
}

func applyStringProperty(raw json.RawMessage, isNull bool, target *string) {
	if isNull {
		*target = ""
		return
	}

	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		*target = value
	}
}

func fieldNumberProperty(raw json.RawMessage, isNull bool) *float64 {
	if isNull {
		return nil
	}

	number, ok := fieldJSONNumber(raw)
	if !ok {
		return nil
	}

	return &number
}

func fieldJSONNumber(raw json.RawMessage) (float64, bool) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, false
	}

	value, err := number.Float64()
	return value, err == nil
}

func formatFieldValue(value any, config resolvedFieldConfig) string {
	switch typed := value.(type) {
	case nil:
		return config.noValue
	case float64:
		return formatFieldNumber(typed, config)
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return typed.String()
		}

		return formatFieldNumber(number, config)
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func formatFieldNumber(number float64, config resolvedFieldConfig) string {
	return formatValue(number, config.unit, config.decimals)
}

func fieldNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func naturalFieldValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case json.Number:
		return typed.String()
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func matchFieldMapping(
	mappings []fieldMapping,
	value any,
) (fieldMappingResult, bool) {
	for _, mapping := range mappings {
		var result fieldMappingResult
		var matched bool

		switch mapping.Type {
		case mappingTypeValue:
			result, matched = matchValueMapping(mapping.Options, value)
		case "range":
			result, matched = matchRangeMapping(mapping.Options, value)
		case "regex":
			result, matched = matchRegexMapping(mapping.Options, value)
		case "special":
			result, matched = matchSpecialMapping(mapping.Options, value)
		}

		if matched {
			return result, true
		}
	}

	return fieldMappingResult{Text: nil, Color: ""}, false
}

func matchValueMapping(
	raw json.RawMessage,
	value any,
) (fieldMappingResult, bool) {
	var options map[string]fieldMappingResult
	if err := json.Unmarshal(raw, &options); err != nil {
		return fieldMappingResult{Text: nil, Color: ""}, false
	}

	for _, key := range fieldValueMappingKeys(value) {
		if result, ok := options[key]; ok {
			return result, true
		}
	}

	return fieldMappingResult{Text: nil, Color: ""}, false
}

func fieldValueMappingKeys(value any) []string {
	switch typed := value.(type) {
	case json.Number:
		keys := []string{typed.String()}
		if number, err := typed.Float64(); err == nil {
			canonical := strconv.FormatFloat(number, 'g', -1, 64)
			if canonical != typed.String() {
				keys = append(keys, canonical)
			}
		}

		return keys
	default:
		return []string{naturalFieldValue(value)}
	}
}

func matchRangeMapping(
	raw json.RawMessage,
	value any,
) (fieldMappingResult, bool) {
	var options struct {
		From   *float64           `json:"from"`
		To     *float64           `json:"to"`
		Result fieldMappingResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &options); err != nil ||
		options.From == nil || options.To == nil {
		return fieldMappingResult{Text: nil, Color: ""}, false
	}

	number, ok := fieldNumber(value)
	if !ok || math.IsNaN(number) || number < *options.From || number > *options.To {
		return fieldMappingResult{Text: nil, Color: ""}, false
	}

	return options.Result, true
}

func matchRegexMapping(
	raw json.RawMessage,
	value any,
) (fieldMappingResult, bool) {
	var options struct {
		Pattern string             `json:"pattern"`
		Result  fieldMappingResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &options); err != nil {
		return fieldMappingResult{Text: nil, Color: ""}, false
	}

	pattern, err := regexp.Compile(options.Pattern)
	if err != nil || !pattern.MatchString(naturalFieldValue(value)) {
		return fieldMappingResult{Text: nil, Color: ""}, false
	}

	return options.Result, true
}

func matchSpecialMapping(
	raw json.RawMessage,
	value any,
) (fieldMappingResult, bool) {
	var options struct {
		Match  string             `json:"match"`
		Result fieldMappingResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &options); err != nil {
		return fieldMappingResult{Text: nil, Color: ""}, false
	}

	number, numeric := fieldNumber(value)
	isNull := value == nil
	isNaN := numeric && math.IsNaN(number)

	matched := false
	switch options.Match {
	case "null":
		matched = isNull
	case "nan":
		matched = isNaN
	case "null+nan":
		matched = isNull || isNaN
	}

	return options.Result, matched
}

//nolint:cyclop // threshold evaluation handles absolute, percentage, and fallback forms
func fieldThresholdColor(config resolvedFieldConfig, number float64) string {
	if config.thresholds == nil || len(config.thresholds.Steps) == 0 {
		return ""
	}

	compareValue := number
	if config.thresholds.Mode == "percentage" &&
		config.min != nil && config.max != nil && *config.max > *config.min {
		compareValue = (number - *config.min) / (*config.max - *config.min) * 100
	}

	baseColor := ""
	bestValue := math.Inf(-1)
	bestColor := ""

	for _, step := range config.thresholds.Steps {
		if step.Value == nil {
			baseColor = step.Color
			continue
		}

		if compareValue >= *step.Value && *step.Value >= bestValue {
			bestValue = *step.Value
			bestColor = step.Color
		}
	}

	if !math.IsInf(bestValue, -1) {
		return resolveGrafanaColor(bestColor)
	}

	return resolveGrafanaColor(baseColor)
}

func isColorBackground(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var options map[string]json.RawMessage
	if err := json.Unmarshal(raw, &options); err != nil {
		return false
	}

	if typeJSON, ok := options["type"]; ok {
		var optionType string
		if err := json.Unmarshal(typeJSON, &optionType); err == nil {
			return optionType == "color-background"
		}

		if isColorBackground(typeJSON) {
			return true
		}
	}

	for _, key := range []string{"mode", "basic"} {
		if nested, ok := options[key]; ok && isColorBackground(nested) {
			return true
		}
	}

	return false
}
