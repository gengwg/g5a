package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	variableTypeCustom       = "custom"
	variableTypeInterval     = "interval"
	grafanaTimeNow           = "now"
	grafanaIntervalAutoToken = "$__auto"
)

var (
	errNoDatasource      = errors.New("no datasource for variable")
	errLabelValuesFailed = errors.New("label values query failed")
)

const datasourceVariableName = "datasource"

// ResolveVariableOptions resolves variables using a one-hour default window.
// New callers should use ResolveVariableOptionsWithTimeRange.
func (c *Client) ResolveVariableOptions(
	ctx context.Context,
	variables []TemplateVariable,
) (map[string][]string, map[string]string, error) {
	return c.ResolveVariableOptionsWithTimeRange(
		ctx,
		variables,
		TimeRange{From: "now-1h", To: grafanaTimeNow, Timezone: "", WeekStart: "", NowDelay: ""},
	)
}

// ResolveVariableOptionsWithTimeRange resolves variables using saved selections.
func (c *Client) ResolveVariableOptionsWithTimeRange(
	ctx context.Context,
	variables []TemplateVariable,
	timeRange TimeRange,
) (map[string][]string, map[string]string, error) {
	return c.ResolveVariableOptionsWithSelections(
		ctx,
		variables,
		timeRange,
		nil,
	)
}

// ResolveVariableOptionsWithSelections resolves variables in dashboard order,
// using current selections for dependent queries when supplied.
func shouldFetchQueryVariable(variable TemplateVariable, initialLoad bool) bool {
	if initialLoad && len(variable.Options) == 0 {
		return true
	}
	if initialLoad {
		return variable.Refresh != 0
	}

	return variable.Refresh == 2
}

func (c *Client) ResolveVariableOptionsWithSelections(
	ctx context.Context,
	variables []TemplateVariable,
	timeRange TimeRange,
	selections map[string]string,
) (map[string][]string, map[string]string, error) {
	return c.ResolveVariableOptionsWithSelectionsAndInitialLoad(
		ctx, variables, timeRange, selections, true,
	)
}

// ResolveVariableOptionsWithSelectionsAndInitialLoad resolves variables and
// distinguishes initial dashboard loading from a later time-range change.
//
//nolint:cyclop // variable types and selection handling share one ordered pipeline
func (c *Client) ResolveVariableOptionsWithSelectionsAndInitialLoad(
	ctx context.Context,
	variables []TemplateVariable,
	timeRange TimeRange,
	selections map[string]string,
	initialLoad bool,
) (map[string][]string, map[string]string, error) {
	options := make(map[string][]string, len(variables))
	defaults := make(map[string]string, len(variables))
	var datasources []datasourceInfo

	for _, variable := range variables {
		var opts []string
		current := resolveCurrentValue(variable)
		if selected, ok := selections[variable.Name]; ok && selected != "" {
			current = selected
		}
		current = normalizeIntervalAutoSelection(variable, current)

		switch variable.Type {
		case variableTypeCustom, "constant":
			opts = parseCustomVariableOptions(variable)
		case datasourceVariableName:
			if datasources == nil {
				if err := c.doJSON(ctx, "/api/datasources", &datasources); err != nil {
					return nil, nil, fmt.Errorf("listing datasources: %w", err)
				}
			}
			opts = datasourceVariableOptions(variable, datasources)
			current = resolveDatasourceSelection(current, datasources)
		case variableTypeInterval:
			opts = intervalVariableOptions(variable)
		case "query":
			if !shouldFetchQueryVariable(variable, initialLoad) {
				opts = parseCustomVariableOptions(variable)
				break
			}
			fetched, err := c.fetchQueryVariableOptions(
				ctx,
				variable,
				variables,
				defaults,
				timeRange,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("resolving variable %s: %w", variable.Name, err)
			}
			opts = fetched
		default:
			opts = parseCustomVariableOptions(variable)
		}

		options[variable.Name] = opts
		defaults[variable.Name] = defaultVariableSelection(variable, opts, current)
	}

	return options, defaults, nil
}

func normalizeIntervalAutoSelection(variable TemplateVariable, selected string) string {
	if variable.Type == variableTypeInterval && variable.Auto && isIntervalAutoSelection(variable, selected) {
		return grafanaIntervalAutoToken
	}

	return selected
}

func isIntervalAutoSelection(variable TemplateVariable, selected string) bool {
	return selected == grafanaIntervalAutoToken || selected == "$__auto_interval_"+variable.Name
}

func intervalVariableOptions(variable TemplateVariable) []string {
	options := parseCustomVariableOptions(variable)
	if !variable.Auto {
		return options
	}
	result := make([]string, 0, len(options)+1)
	for _, option := range options {
		if !isIntervalAutoSelection(variable, option) {
			result = append(result, option)
		}
	}

	return append(result, grafanaIntervalAutoToken)
}

func parseCustomVariableOptions(v TemplateVariable) []string {
	if len(v.Options) > 0 {
		opts := make([]string, 0, len(v.Options))
		for _, option := range v.Options {
			opts = append(opts, option.Value)
		}
		return opts
	}

	query := queryDefinition(v)
	if query == "" {
		return []string{}
	}

	return splitVariableValues(query)
}

func datasourceVariableOptions(
	variable TemplateVariable,
	datasources []datasourceInfo,
) []string {
	wantedType := strings.Trim(queryDefinition(variable), `"`)
	matcher := compileVariableRegex(variable.Regex)
	options := make([]string, 0, len(datasources))

	for _, datasource := range datasources {
		if wantedType != "" && datasource.Type != wantedType {
			continue
		}
		if matcher != nil && !matcher.MatchString(datasource.Name) {
			continue
		}
		options = append(options, datasource.UID)
	}

	slices.Sort(options)

	return options
}

func resolveDatasourceSelection(
	current string,
	datasources []datasourceInfo,
) string {
	for _, datasource := range datasources {
		if current == datasource.UID || current == datasource.Name {
			return datasource.UID
		}
	}

	return current
}

//nolint:cyclop // saved selection precedence includes multi and All forms
func defaultVariableSelection(
	variable TemplateVariable,
	options []string,
	current string,
) string {
	if isAllSelection(variable, current) {
		if variable.AllValue != "" && current == variable.AllValue {
			return current
		}
		if current != "" && current != ".*" {
			return current
		}
		return grafanaAllToken
	}

	if variable.Multi && strings.Contains(current, ",") {
		selected := splitVariableValues(current)
		valid := true
		for _, value := range selected {
			if !slices.Contains(options, value) {
				valid = false
				break
			}
		}
		if valid && len(selected) > 0 {
			return strings.Join(selected, ",")
		}
	}

	if current != "" && (len(options) == 0 || slices.Contains(options, current)) {
		return current
	}
	if variable.IncludeAll {
		if variable.AllValue != "" {
			return variable.AllValue
		}
		return grafanaAllToken
	}
	if len(options) > 0 {
		return options[0]
	}

	return ""
}

//nolint:cyclop,funlen // supports label_values, generic query_result, and native query types
func (c *Client) fetchQueryVariableOptions(
	ctx context.Context,
	variable TemplateVariable,
	variables []TemplateVariable,
	resolved map[string]string,
	timeRange TimeRange,
) ([]string, error) {
	rawQuery := queryDefinition(variable)
	if rawQuery == "" {
		return []string{}, nil
	}
	if !validTimeExpression(timeRange.From) || !validTimeExpression(timeRange.To) {
		return nil, fmt.Errorf("%w: %s to %s", errUnsupportedTimeRange, timeRange.From, timeRange.To)
	}
	fromMs, toMs := resolveTimeRange(timeRange)
	resolved = resolveIntervalAutoOverrides(variables, resolved, toMs-fromMs)
	query := ResolveVariables(rawQuery, variables, resolved)
	if query == "" {
		return []string{}, nil
	}

	datasourceUID := ""
	datasourceType := ""
	if variable.Datasource != nil {
		datasourceUID = ResolveVariables(variable.Datasource.UID, variables, resolved)
		datasourceType = variable.Datasource.Type
	}
	if datasourceUID == "" {
		datasourceUID = resolved[datasourceVariableName]
	}
	if datasourceUID == "" {
		return nil, fmt.Errorf("%w: %s", errNoDatasource, variable.Name)
	}
	datasourceUID = c.ResolveDatasourceUID(ctx, datasourceUID, datasourceType)
	if unresolvedDatasourceUID(datasourceUID) {
		return nil, fmt.Errorf("%w: %s", errNoDatasource, variable.Name)
	}

	if metric, label, ok := parseLabelValuesQuery(query); ok {
		values, err := c.queryLabelValues(ctx, datasourceUID, label, metric, fromMs, toMs)
		if err != nil {
			return nil, err
		}
		return finishVariableOptions(values, variable), nil
	}

	if queryType := datasourceQueryType(variable); queryType != "" {
		target := map[string]any{
			"refId":      "vars",
			"queryType":  queryType,
			"datasource": DatasourceRef{UID: datasourceUID, Type: datasourceType},
		}
		rawTarget, err := json.Marshal(target)
		if err != nil {
			return nil, fmt.Errorf("encoding variable query type: %w", err)
		}
		result, err := c.queryDatasource(
			ctx,
			[]json.RawMessage{rawTarget},
			fromMs,
			toMs,
		)
		if err != nil {
			return nil, err
		}
		return finishVariableOptions(variableOptionsFromFrames(result), variable), nil
	}

	expression := query
	if strings.HasPrefix(query, "query_result(") && strings.HasSuffix(query, ")") {
		expression = strings.TrimSuffix(strings.TrimPrefix(query, "query_result("), ")")
	}

	target := map[string]any{
		"refId":      "vars",
		"expr":       expression,
		"instant":    true,
		"range":      false,
		"format":     "time_series",
		"datasource": DatasourceRef{UID: datasourceUID, Type: datasourceType},
	}
	rawTarget, err := json.Marshal(target)
	if err != nil {
		return nil, fmt.Errorf("encoding variable query: %w", err)
	}

	result, err := c.queryDatasource(
		ctx,
		[]json.RawMessage{rawTarget},
		fromMs,
		toMs,
	)
	if err != nil {
		return nil, err
	}

	values := variableOptionsFromFrames(result)
	return finishVariableOptions(values, variable), nil
}

//nolint:cyclop // label_values parsing handles nested selectors and quoted delimiters
func parseLabelValuesQuery(query string) (metric, label string, ok bool) {
	const functionName = "label_values"

	query = strings.TrimSpace(query)
	if !strings.HasPrefix(query, functionName) {
		return "", "", false
	}
	arguments := strings.TrimSpace(query[len(functionName):])
	if len(arguments) < 2 || arguments[0] != '(' || arguments[len(arguments)-1] != ')' {
		return "", "", false
	}

	parts, ok := splitTopLevelComma(arguments[1 : len(arguments)-1])
	if !ok {
		return "", "", false
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
		if parts[index] == "" {
			return "", "", false
		}
	}

	switch len(parts) {
	case 1:
		return "", parts[0], true
	case 2:
		return parts[0], parts[1], true
	default:
		return "", "", false
	}
}

// splitTopLevelComma splits arguments without splitting selectors or quoted strings.
//
//nolint:gocyclo,cyclop,funlen // top-level delimiter tracking has several small states
func splitTopLevelComma(input string) ([]string, bool) {
	parts := make([]string, 0, 2)
	start := 0
	var quote byte
	var braces, brackets, parentheses int

	for index := 0; index < len(input); index++ {
		char := input[index]
		if quote != 0 {
			if char == '\\' && quote != '`' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}

		switch char {
		case '"', '\'', '`':
			quote = char
		case '{':
			braces++
		case '}':
			if braces == 0 {
				return nil, false
			}
			braces--
		case '[':
			brackets++
		case ']':
			if brackets == 0 {
				return nil, false
			}
			brackets--
		case '(':
			parentheses++
		case ')':
			if parentheses == 0 {
				return nil, false
			}
			parentheses--
		case ',':
			if braces == 0 && brackets == 0 && parentheses == 0 {
				parts = append(parts, input[start:index])
				start = index + 1
			}
		}
	}

	if quote != 0 || braces != 0 || brackets != 0 || parentheses != 0 {
		return nil, false
	}

	return append(parts, input[start:]), true
}

func (c *Client) queryLabelValues(
	ctx context.Context,
	datasourceUID string,
	label string,
	metric string,
	fromMs int64,
	toMs int64,
) ([]string, error) {
	path := "/api/datasources/proxy/uid/" + url.PathEscape(datasourceUID) +
		"/api/v1/label/" + url.PathEscape(label) + "/values"
	params := url.Values{}
	if metric != "" {
		params.Set("match[]", metric)
	}
	params.Set("start", strconv.FormatInt(fromMs/1000, 10))
	params.Set("end", strconv.FormatInt(toMs/1000, 10))
	path += "?" + params.Encode()

	var result promLabelValuesResult
	if err := c.doJSON(ctx, path, &result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("%w with status %q", errLabelValuesFailed, result.Status)
	}

	return result.Data, nil
}

type promLabelValuesResult struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

func variableOptionsFromFrames(result *QueryResult) []string {
	values := make([]string, 0)
	for _, refID := range sortedResultRefIDs(result) {
		for _, frame := range result.Results[refID].Frames {
			for fieldIndex, field := range frame.Schema.Fields {
				if fieldIndex >= len(frame.Data.Values) {
					continue
				}
				if field.Type == fieldTypeString {
					column, err := parseStringValues(frame.Data.Values[fieldIndex])
					if err == nil {
						values = append(values, column...)
					}
					continue
				}
				if field.Type == fieldTypeNumber {
					values = append(values, fieldSampleValues(frame, fieldIndex)...)
					continue
				}
			}
		}
	}

	return values
}

func fieldSampleValues(frame DataFrame, fieldIndex int) []string {
	field := frame.Schema.Fields[fieldIndex]
	numbers, err := parseNumberValues(frame.Data.Values[fieldIndex])
	if err != nil {
		return nil
	}
	metric := field.Name
	if metric == "" {
		metric = "Value"
	}
	if name := field.Labels["__name__"]; name != "" {
		metric = name
	}
	timestamps := fieldTimestamps(frame)
	values := make([]string, 0, len(numbers))
	for index, number := range numbers {
		value := strconv.FormatFloat(number, 'g', -1, 64)
		switch {
		case index < len(timestamps):
			values = append(values, fmt.Sprintf("%s%s %s %d", metric, fieldLabelText(field), value, timestamps[index]))
		case len(field.Labels) > 0:
			values = append(values, fieldLabelText(field))
		default:
			values = append(values, value)
		}
	}

	return values
}

func fieldTimestamps(frame DataFrame) []int64 {
	for index, field := range frame.Schema.Fields {
		if field.Type != fieldTypeTime {
			continue
		}
		timestamps, err := parseTimeValues(frame.Data.Values[index])
		if err == nil {
			return timestamps
		}
	}

	return nil
}

func fieldLabelText(field DataFrameField) string {
	keys := make([]string, 0, len(field.Labels))
	for key := range field.Labels {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, field.Labels[key]))
	}

	return "{" + strings.Join(parts, ",") + "}"
}

func finishVariableOptions(
	values []string,
	variable TemplateVariable,
) []string {
	matcher := compileVariableRegex(variable.Regex)
	seen := make(map[string]bool, len(values))
	options := make([]string, 0, len(values))

	for _, value := range values {
		extracted := applyVariableRegex(matcher, value)
		for _, option := range extracted {
			if option == "" || seen[option] {
				continue
			}
			seen[option] = true
			options = append(options, option)
		}
	}

	sortVariableOptions(options, variable.Sort)

	return options
}

func compileVariableRegex(expression string) *regexp.Regexp {
	if len(expression) >= 2 && strings.HasPrefix(expression, "/") && strings.HasSuffix(expression, "/") {
		expression = strings.TrimSuffix(strings.TrimPrefix(expression, "/"), "/")
	}
	if expression == "" {
		return nil
	}
	matcher, err := regexp.Compile(expression)
	if err != nil {
		slog.Warn("invalid dashboard variable regex", "error", err)
		return nil
	}

	return matcher
}

func applyVariableRegex(matcher *regexp.Regexp, value string) []string {
	if matcher == nil {
		return []string{value}
	}
	matches := matcher.FindStringSubmatch(value)
	if matches == nil {
		return nil
	}
	if len(matches) > 1 {
		return []string{matches[1]}
	}

	return []string{matches[0]}
}

func sortVariableOptions(options []string, mode int) {
	switch mode {
	case 1:
		slices.Sort(options)
	case 2:
		slices.SortFunc(options, func(a, b string) int { return strings.Compare(b, a) })
	case 3, 4:
		sort.SliceStable(options, func(i, j int) bool {
			left, leftErr := strconv.ParseFloat(options[i], 64)
			right, rightErr := strconv.ParseFloat(options[j], 64)
			if leftErr != nil || rightErr != nil {
				return options[i] < options[j]
			}
			if mode == 4 {
				return left > right
			}
			return left < right
		})
	case 5, 6:
		sort.SliceStable(options, func(i, j int) bool {
			left := strings.ToLower(options[i])
			right := strings.ToLower(options[j])
			if mode == 6 {
				return left > right
			}
			return left < right
		})
	}
}

func datasourceQueryType(variable TemplateVariable) string {
	var query struct {
		QueryType string `json:"queryType"`
	}
	if err := json.Unmarshal(variable.Query, &query); err != nil {
		return ""
	}

	return query.QueryType
}

func queryDefinition(v TemplateVariable) string {
	if v.Definition != "" {
		return v.Definition
	}
	if len(v.Query) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(v.Query, &text); err == nil {
		return text
	}
	var object struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(v.Query, &object); err == nil {
		return object.Query
	}

	return ""
}

func sortedResultRefIDs(result *QueryResult) []string {
	refIDs := make([]string, 0, len(result.Results))
	for refID := range result.Results {
		refIDs = append(refIDs, refID)
	}
	slices.Sort(refIDs)

	return refIDs
}
