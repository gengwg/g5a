package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	errTargetNotObject      = errors.New("target is not an object")
	errUnsupportedTimeRange = errors.New("unsupported time range")
	errUnresolvedDatasource = errors.New("unresolved datasource")
)

// TimeRange represents a Grafana time range.
type TimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// QueryResult holds the result of a panel query.
type QueryResult struct {
	Results  map[string]QueryResultData `json:"results"`
	Warnings []string                   `json:"-"`
}

// QueryResultData contains frames and optional per-reference diagnostics.
type QueryResultData struct {
	Frames []DataFrame `json:"frames"`
	Error  string      `json:"error,omitempty"`
	Status int         `json:"status,omitempty"`
}

// DataFrame represents a single data frame in a query result.
type DataFrame struct {
	Schema DataFrameSchema `json:"schema"`
	Data   DataFrameData   `json:"data"`
}

// DataFrameSchema describes the structure of a data frame.
type DataFrameSchema struct {
	RefID  string           `json:"refId"`
	Meta   DataFrameMeta    `json:"meta"`
	Fields []DataFrameField `json:"fields"`
}

// DataFrameMeta holds metadata about a data frame.
type DataFrameMeta struct {
	Type        string `json:"type"`
	TypeVersion []int  `json:"typeVersion"`
}

// DataFrameField describes a single field in a data frame.
type DataFrameField struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
	Config json.RawMessage   `json:"config,omitempty"`
}

// DataFrameData holds values for each field as parallel arrays.
type DataFrameData struct {
	Values []json.RawMessage `json:"values"`
}

// QueryPanel executes all visible targets through Grafana's generic query API,
// preserving datasource-specific target fields and instant/range semantics.
//
//nolint:cyclop,funlen // orchestrates inline frames, target interpolation, and batching
func (c *Client) QueryPanel(
	ctx context.Context,
	panel Panel,
	timeRange TimeRange,
	maxDataPoints int,
	variables []TemplateVariable,
	variableOverrides map[string]string,
	allPanels []Panel,
) (*QueryResult, error) {
	panel = resolveDashboardDatasource(panel, allPanels)
	if !validTimeExpression(timeRange.From) || !validTimeExpression(timeRange.To) {
		return nil, fmt.Errorf("%w: %s to %s", errUnsupportedTimeRange, timeRange.From, timeRange.To)
	}
	result := &QueryResult{
		Results:  make(map[string]QueryResultData),
		Warnings: make([]string, 0),
	}
	if len(panel.Targets) == 0 {
		return result, nil
	}

	fromMs, toMs := resolveTimeRange(timeRange)
	intervalMs := computeIntervalMsBetween(fromMs, toMs, maxDataPoints)
	genericTargets := make([]json.RawMessage, 0, len(panel.Targets))

	for _, target := range panel.Targets {
		var metadata struct {
			Hide bool `json:"hide"`
		}
		_ = json.Unmarshal(target.Raw(), &metadata)
		if metadata.Hide {
			continue
		}

		datasource := target.Datasource
		if datasource == nil {
			datasource = panel.Datasource
		}
		if datasource == nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("target %s has no datasource", target.RefID))
			continue
		}

		resolvedDatasource := &DatasourceRef{
			UID:  ResolveVariables(datasource.UID, variables, variableOverrides),
			Type: datasource.Type,
		}
		resolvedDatasource.UID = c.ResolveDatasourceUID(
			ctx,
			resolvedDatasource.UID,
			resolvedDatasource.Type,
		)
		if unresolvedDatasourceUID(resolvedDatasource.UID) {
			return nil, fmt.Errorf("%w: %s", errUnresolvedDatasource, datasource.UID)
		}

		if frame, ok := extractInlineFrame(target, resolvedDatasource); ok {
			result.Results[target.RefID] = QueryResultData{ //nolint:exhaustruct // optional diagnostics
				Frames: []DataFrame{frame},
			}
			continue
		}

		query, err := buildDSQueryTarget(
			target,
			resolvedDatasource,
			variables,
			variableOverrides,
			intervalMs,
			toMs-fromMs,
			maxDataPoints,
			panel.Type,
		)
		if err != nil {
			return nil, fmt.Errorf("building target %q: %w", target.RefID, err)
		}
		genericTargets = append(genericTargets, query)
	}

	if len(genericTargets) > 0 {
		queryResult, err := c.queryDatasource(ctx, genericTargets, fromMs, toMs)
		if err != nil {
			return nil, fmt.Errorf("querying datasource: %w", err)
		}
		maps.Copy(result.Results, queryResult.Results)
	}

	for refID, data := range result.Results {
		if data.Error != "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("%s: %s", refID, data.Error))
		}
	}
	ApplyTransformations(result, panel.Transformations)

	return result, nil
}

// inlineFrameFields represents the Static datasource's inline frame field.
type inlineFrameFields struct {
	Fields []inlineField `json:"fields"`
}

type inlineField struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Values json.RawMessage `json:"values"`
}

//nolint:exhaustruct // empty frames and optional schema metadata are intentional
func extractInlineFrame(
	target Target,
	datasource *DatasourceRef,
) (DataFrame, bool) {
	if datasource == nil {
		return DataFrame{}, false
	}
	raw := target.Raw()
	if len(raw) == 0 {
		return DataFrame{}, false
	}

	var wrapper struct {
		Frame *inlineFrameFields `json:"frame"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Frame == nil {
		return DataFrame{}, false
	}
	if len(wrapper.Frame.Fields) == 0 {
		return DataFrame{}, false
	}

	frame := DataFrame{
		Schema: DataFrameSchema{
			RefID:  target.RefID,
			Fields: make([]DataFrameField, len(wrapper.Frame.Fields)),
		},
		Data: DataFrameData{Values: make([]json.RawMessage, len(wrapper.Frame.Fields))},
	}
	for index, field := range wrapper.Frame.Fields {
		frame.Schema.Fields[index] = DataFrameField{
			Name: field.Name,
			Type: field.Type,
		}
		frame.Data.Values[index] = field.Values
	}

	return frame, true
}

// resolveDashboardDatasource resolves the special dashboard panel reference.
func resolveDashboardDatasource(panel Panel, allPanels []Panel) Panel {
	if len(panel.Targets) == 0 || len(allPanels) == 0 {
		return panel
	}

	datasource := panel.Targets[0].Datasource
	if datasource == nil {
		datasource = panel.Datasource
	}
	if datasource == nil || datasource.UID != "-- Dashboard --" {
		return panel
	}

	panelID := extractPanelID(panel.Targets[0])
	for _, candidate := range allPanels {
		if candidate.ID == panelID {
			panel.Targets = candidate.Targets
			panel.Datasource = candidate.Datasource
			return panel
		}
	}

	return panel
}

func extractPanelID(target Target) int {
	var fields struct {
		PanelID int `json:"panelId"`
	}
	if err := json.Unmarshal(target.Raw(), &fields); err != nil {
		return 0
	}

	return fields.PanelID
}

func unresolvedDatasourceUID(uid string) bool {
	return uid == "" || (strings.HasPrefix(uid, "${") && strings.HasSuffix(uid, "}"))
}

func buildDSQueryTarget(
	target Target,
	datasource *DatasourceRef,
	variables []TemplateVariable,
	overrides map[string]string,
	intervalMs int,
	rangeMs int64,
	maxDataPoints int,
	panelType string,
) (json.RawMessage, error) {
	raw := target.Raw()
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(target)
		if err != nil {
			return nil, fmt.Errorf("marshaling target fallback: %w", err)
		}
	}

	var targetMap map[string]any
	if err := json.Unmarshal(raw, &targetMap); err != nil {
		return nil, fmt.Errorf("decoding raw target: %w", err)
	}

	interpolated := interpolateTargetValue(
		targetMap,
		variables,
		overrides,
		intervalMs,
		rangeMs,
	)
	targetMap, ok := interpolated.(map[string]any)
	if !ok {
		return nil, errTargetNotObject
	}
	_, hasInstant := targetMap["instant"]
	_, hasRange := targetMap["range"]
	if !hasInstant && !hasRange && (panelType == "timeseries" || panelType == "graph") {
		// Legacy dashboards omit these fields for the normal range-query mode.
		targetMap["range"] = true
		targetMap["instant"] = false
	}
	targetMap["datasource"] = datasource
	targetMap["intervalMs"] = intervalMs
	targetMap["maxDataPoints"] = maxDataPoints

	encoded, err := json.Marshal(targetMap)
	if err != nil {
		return nil, fmt.Errorf("encoding target: %w", err)
	}

	return encoded, nil
}

func interpolateTargetValue(
	value any,
	variables []TemplateVariable,
	overrides map[string]string,
	intervalMs int,
	rangeMs int64,
) any {
	switch typed := value.(type) {
	case string:
		resolved := ResolveVariables(typed, variables, overrides)
		return resolveBuiltinVariables(resolved, intervalMs, rangeMs)
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = interpolateTargetValue(
				child,
				variables,
				overrides,
				intervalMs,
				rangeMs,
			)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = interpolateTargetValue(
				child,
				variables,
				overrides,
				intervalMs,
				rangeMs,
			)
		}
		return result
	default:
		return value
	}
}

type dsQueryRequest struct {
	Queries []json.RawMessage `json:"queries"`
	From    string            `json:"from"`
	To      string            `json:"to"`
}

func (c *Client) queryDatasource(
	ctx context.Context,
	queries []json.RawMessage,
	fromMs int64,
	toMs int64,
) (*QueryResult, error) {
	request := dsQueryRequest{
		Queries: queries,
		From:    strconv.FormatInt(fromMs, 10),
		To:      strconv.FormatInt(toMs, 10),
	}
	result := &QueryResult{
		Results:  make(map[string]QueryResultData),
		Warnings: make([]string, 0),
	}
	if err := c.doPostJSON(ctx, "/api/ds/query", request, result); err != nil {
		return nil, err
	}
	if result.Results == nil {
		result.Results = make(map[string]QueryResultData)
	}

	return result, nil
}

var autoIntervalPattern = regexp.MustCompile(`\$__auto_interval_\w+`)

func resolveBuiltinVariables(
	expression string,
	intervalMs int,
	rangeMs int64,
) string {
	expression = autoIntervalPattern.ReplaceAllString(
		expression,
		formatDuration(intervalMs),
	)
	const defaultScrapeIntervalMs = 15000
	rateIntervalMs := max(
		intervalMs+defaultScrapeIntervalMs,
		4*defaultScrapeIntervalMs,
	)
	replacements := []struct {
		name  string
		value string
	}{
		{"$__rate_interval", formatDuration(rateIntervalMs)},
		{"$__interval_ms", strconv.Itoa(intervalMs)},
		{"$__range_ms", strconv.FormatInt(rangeMs, 10)},
		{"$__range_s", strconv.FormatInt(rangeMs/1000, 10)},
		{"$__interval", formatDuration(intervalMs)},
		{"$__range", formatDuration(int(rangeMs))},
		{"$__auto", formatDuration(intervalMs)},
	}
	for _, replacement := range replacements {
		expression = strings.ReplaceAll(
			expression,
			replacement.name,
			replacement.value,
		)
	}

	return expression
}

func formatDuration(milliseconds int) string {
	switch {
	case milliseconds >= 86400000 && milliseconds%86400000 == 0:
		return strconv.Itoa(milliseconds/86400000) + "d"
	case milliseconds >= 3600000 && milliseconds%3600000 == 0:
		return strconv.Itoa(milliseconds/3600000) + "h"
	case milliseconds >= 60000 && milliseconds%60000 == 0:
		return strconv.Itoa(milliseconds/60000) + "m"
	case milliseconds >= 1000 && milliseconds%1000 == 0:
		return strconv.Itoa(milliseconds/1000) + "s"
	default:
		return strconv.Itoa(milliseconds) + "ms"
	}
}

var relativeTimePattern = regexp.MustCompile(
	`^now(?:-(\d+)([smhdwMy]))?(?:/([smhdwMy]))?$`,
)

func computeIntervalMsBetween(fromMs, toMs int64, maxDataPoints int) int {
	if maxDataPoints <= 0 {
		maxDataPoints = 1
	}
	rangeMs := toMs - fromMs
	if rangeMs <= 0 {
		return 1
	}

	return max(int(rangeMs)/maxDataPoints, 1)
}

func validTimeExpression(value string) bool {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return true
	}

	return relativeTimePattern.MatchString(value)
}

func resolveTimeRange(timeRange TimeRange) (fromMs, toMs int64) {
	return resolveTimeRangeAt(timeRange, time.Now())
}

func resolveTimeRangeAt(timeRange TimeRange, now time.Time) (fromMs, toMs int64) {
	return resolveTimeToMsAtWithRounding(timeRange.From, now, false),
		resolveTimeToMsAtWithRounding(timeRange.To, now, true)
}

func resolveTimeToMsAt(value string, now time.Time) int64 {
	return resolveTimeToMsAtWithRounding(value, now, false)
}

func resolveTimeToMsAtWithRounding(value string, now time.Time, roundUp bool) int64 {
	if epochMs, err := strconv.ParseInt(value, 10, 64); err == nil {
		return epochMs
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UnixMilli()
	}

	matches := relativeTimePattern.FindStringSubmatch(value)
	if matches == nil {
		return now.UnixMilli()
	}

	resolved := now
	if matches[1] != "" {
		resolved = subtractDuration(resolved, matches[1], matches[2])
	}
	if matches[3] != "" {
		resolved = roundTimeToUnit(resolved, matches[3], roundUp)
	}

	return resolved.UnixMilli()
}

func roundTimeToUnit(value time.Time, unit string, roundUp bool) time.Time {
	var start, next time.Time
	location := value.Location()

	switch unit {
	case "s":
		start = time.Date(
			value.Year(), value.Month(), value.Day(),
			value.Hour(), value.Minute(), value.Second(), 0, location,
		)
		next = start.Add(time.Second)
	case "m":
		start = time.Date(
			value.Year(), value.Month(), value.Day(),
			value.Hour(), value.Minute(), 0, 0, location,
		)
		next = start.Add(time.Minute)
	case "h":
		start = time.Date(
			value.Year(), value.Month(), value.Day(),
			value.Hour(), 0, 0, 0, location,
		)
		next = start.Add(time.Hour)
	case "d":
		start = time.Date(
			value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location,
		)
		next = start.AddDate(0, 0, 1)
	case "w":
		start = time.Date(
			value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, location,
		)
		start = start.AddDate(0, 0, -int(start.Weekday()))
		next = start.AddDate(0, 0, 7)
	case "M":
		start = time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, location)
		next = start.AddDate(0, 1, 0)
	case "y":
		start = time.Date(value.Year(), 1, 1, 0, 0, 0, 0, location)
		next = start.AddDate(1, 0, 0)
	default:
		return value
	}

	if roundUp {
		return next.Add(-time.Millisecond)
	}

	return start
}

func subtractDuration(base time.Time, amountText, unit string) time.Time {
	amount, _ := strconv.Atoi(amountText)
	durations := map[string]time.Duration{
		"s": time.Second,
		"m": time.Minute,
		"h": time.Hour,
		"d": 24 * time.Hour,
		"w": 7 * 24 * time.Hour,
	}
	if duration, ok := durations[unit]; ok {
		return base.Add(-time.Duration(amount) * duration)
	}
	if unit == "M" {
		return base.AddDate(0, -amount, 0)
	}
	if unit == "y" {
		return base.AddDate(-amount, 0, 0)
	}

	return base
}
