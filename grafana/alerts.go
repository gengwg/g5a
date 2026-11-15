package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	alertStateFiring       = "firing"
	alertStateNormal       = "normal"
	alertStateNoData       = "no_data"
	alertStateError        = "error"
	alertRuleSourceGrafana = "grafana"
)

// AlertRule is one alert rule and its currently matching instances.
type AlertRule struct {
	Name         string
	State        string
	Folder       string
	DashboardUID string
	ActiveAt     time.Time
	Instances    []AlertInstance
}

// AlertInstance is one active or inactive alert series.
type AlertInstance struct {
	State    string
	Labels   map[string]string
	ActiveAt time.Time
}

type alertListOptions struct {
	ViewMode                 string           `json:"viewMode"`
	GroupMode                string           `json:"groupMode"`
	GroupBy                  []string         `json:"groupBy"`
	MaxItems                 int              `json:"maxItems"`
	SortOrder                int              `json:"sortOrder"`
	DashboardAlerts          bool             `json:"dashboardAlerts"`
	AlertName                string           `json:"alertName"`
	AlertInstanceLabelFilter string           `json:"alertInstanceLabelFilter"`
	Datasource               string           `json:"datasource"`
	Folder                   *alertListFolder `json:"folder"`
	StateFilter              map[string]bool  `json:"stateFilter"`
	ShowInactiveAlerts       bool             `json:"showInactiveAlerts"`
	ShowInstances            bool             `json:"showInstances"`
}

type alertListFolder struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
}

type alertRuleDatasource struct {
	UID      string `json:"uid"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	JSONData struct {
		ManageAlerts *bool `json:"manageAlerts"`
	} `json:"jsonData"`
}

type alertRuleSource struct {
	UID  string
	Name string
}

type prometheusRulesResponse struct {
	Data struct {
		Groups []struct {
			Name  string `json:"name"`
			File  string `json:"file"`
			Rules []struct {
				Name        string            `json:"name"`
				State       string            `json:"state"`
				Health      string            `json:"health"`
				Type        string            `json:"type"`
				Annotations map[string]string `json:"annotations"`
				Alerts      []struct {
					State    string            `json:"state"`
					Labels   map[string]string `json:"labels"`
					ActiveAt time.Time         `json:"activeAt"`
				} `json:"alerts"`
			} `json:"rules"`
		} `json:"groups"`
	} `json:"data"`
}

// QueryAlertList loads current alert rules without changing alerting state.
func (c *Client) QueryAlertList(
	ctx context.Context,
	panel Panel,
	dashboardUID string,
	variables []TemplateVariable,
	variableOverrides map[string]string,
) (*QueryResult, error) {
	var opts alertListOptions
	if err := json.Unmarshal(panel.Options, &opts); err != nil {
		return nil, fmt.Errorf("decoding alert-list options: %w", err)
	}
	opts.AlertName = ResolveVariables(opts.AlertName, variables, variableOverrides)
	opts.AlertInstanceLabelFilter = ResolveVariables(
		opts.AlertInstanceLabelFilter, variables, variableOverrides,
	)

	rules, warnings, err := c.fetchAlertRules(ctx, opts, dashboardUID)
	if err != nil {
		return nil, err
	}
	rules, err = filterAlertRules(rules, opts, dashboardUID)
	if err != nil {
		return nil, err
	}
	if opts.GroupMode == "custom" && opts.ViewMode != "stat" {
		rules = groupAlertRules(rules, opts.GroupBy)
	}
	sortAlertRules(rules, opts.SortOrder)

	result := &QueryResult{
		Results: map[string]QueryResultData{}, Alerts: rules, Warnings: warnings,
	}

	return result, nil
}

func hasGrafanaFolderFilter(opts alertListOptions) bool {
	return opts.Folder != nil && opts.Folder.UID != ""
}

func alertRulesPath(sourceUID string, opts alertListOptions, dashboardUID string) string {
	query := url.Values{}
	if sourceUID == alertRuleSourceGrafana && hasGrafanaFolderFilter(opts) {
		query.Set("folder_uid", opts.Folder.UID)
	}
	if opts.DashboardAlerts && dashboardUID != "" {
		query.Set("dashboard_uid", dashboardUID)
	}
	path := "/api/prometheus/" + url.PathEscape(sourceUID) + "/api/v1/rules"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	return path
}

func (c *Client) fetchAlertRules(
	ctx context.Context,
	opts alertListOptions,
	dashboardUID string,
) ([]AlertRule, []string, error) {
	if opts.Datasource == "" {
		return c.fetchAllAlertRules(ctx, opts, dashboardUID)
	}

	sourceUID := alertRuleSourceGrafana
	if !strings.EqualFold(opts.Datasource, alertRuleSourceGrafana) {
		sourceUID = c.ResolveDatasourceUID(ctx, opts.Datasource, "")
	}
	rules, err := c.fetchAlertRulesFromSource(ctx, sourceUID, opts, dashboardUID)
	if err == nil && sourceUID != alertRuleSourceGrafana && hasGrafanaFolderFilter(opts) {
		return []AlertRule{}, nil, nil
	}

	return rules, nil, err
}

func (c *Client) fetchAllAlertRules(
	ctx context.Context,
	opts alertListOptions,
	dashboardUID string,
) ([]AlertRule, []string, error) {
	sources := []alertRuleSource{{UID: alertRuleSourceGrafana, Name: alertRuleSourceGrafana}}
	warnings := make([]string, 0)
	sourceErrs := make([]error, 0)

	if !hasGrafanaFolderFilter(opts) {
		var datasources []alertRuleDatasource
		if err := c.doJSON(ctx, "/api/datasources", &datasources); err != nil {
			warnings = append(warnings, fmt.Sprintf("listing alert rule datasources: %v", err))
			sourceErrs = append(sourceErrs, fmt.Errorf("listing alert rule datasources: %w", err))
		} else {
			for _, datasource := range datasources {
				if supportedAlertRuleDatasource(datasource) {
					sources = append(sources, alertRuleSource{UID: datasource.UID, Name: datasource.Name})
				}
			}
		}
	}

	rules := make([]AlertRule, 0)
	succeeded := 0
	for _, source := range sources {
		sourceRules, err := c.fetchAlertRulesFromSource(ctx, source.UID, opts, dashboardUID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, warnings, err
			}
			warnings = append(warnings, fmt.Sprintf("querying alert rules from %s: %v", source.Name, err))
			sourceErrs = append(sourceErrs, fmt.Errorf("%s: %w", source.Name, err))
			continue
		}
		succeeded++
		rules = append(rules, sourceRules...)
	}
	if succeeded == 0 && len(sourceErrs) > 0 {
		return nil, warnings, fmt.Errorf("querying alert rules: %w", errors.Join(sourceErrs...))
	}

	return rules, warnings, nil
}

func supportedAlertRuleDatasource(datasource alertRuleDatasource) bool {
	if datasource.UID == "" ||
		(datasource.JSONData.ManageAlerts != nil && !*datasource.JSONData.ManageAlerts) {
		return false
	}

	switch datasource.Type {
	case "prometheus", "loki",
		"grafana-amazonprometheus-datasource", "grafana-azureprometheus-datasource":
		return true
	default:
		return false
	}
}

func (c *Client) fetchAlertRulesFromSource(
	ctx context.Context,
	sourceUID string,
	opts alertListOptions,
	dashboardUID string,
) ([]AlertRule, error) {
	path := alertRulesPath(sourceUID, opts, dashboardUID)
	var response prometheusRulesResponse
	err := c.doJSON(ctx, path, &response)
	if err != nil {
		var statusErr *apiError
		if sourceUID == alertRuleSourceGrafana && errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
			return c.fetchLegacyAlerts(ctx)
		}
		return nil, fmt.Errorf("querying alert rules: %w", err)
	}

	return convertPrometheusRules(response), nil
}

func convertPrometheusRules(response prometheusRulesResponse) []AlertRule {
	rules := make([]AlertRule, 0)
	for _, group := range response.Data.Groups {
		for _, rule := range group.Rules {
			if rule.Type != "" && rule.Type != "alerting" {
				continue
			}
			converted := AlertRule{
				Name: rule.Name, State: rule.State, Folder: group.File,
				DashboardUID: rule.Annotations["__dashboardUid__"],
				ActiveAt:     time.Time{}, Instances: make([]AlertInstance, 0, len(rule.Alerts)),
			}
			for _, alert := range rule.Alerts {
				converted.Instances = append(converted.Instances, AlertInstance{
					State: alert.State, Labels: alert.Labels, ActiveAt: alert.ActiveAt,
				})
			}
			converted.ActiveAt = earliestAlertTime(converted.Instances)
			rules = append(rules, converted)
		}
	}

	return rules
}

func (c *Client) fetchLegacyAlerts(ctx context.Context) ([]AlertRule, error) {
	var response []struct {
		Name         string    `json:"name"`
		State        string    `json:"state"`
		DashboardUID string    `json:"dashboardUId"`
		NewStateDate time.Time `json:"newStateDate"`
	}
	if err := c.doJSON(ctx, "/api/alerts", &response); err != nil {
		return nil, fmt.Errorf("querying legacy alerts: %w", err)
	}
	rules := make([]AlertRule, 0, len(response))
	for _, alert := range response {
		instance := AlertInstance{
			State: alert.State, Labels: map[string]string{}, ActiveAt: alert.NewStateDate,
		}
		rules = append(rules, AlertRule{
			Name: alert.Name, State: alert.State, Folder: "", DashboardUID: alert.DashboardUID,
			ActiveAt: alert.NewStateDate, Instances: []AlertInstance{instance},
		})
	}

	return rules, nil
}

func filterAlertRules(
	rules []AlertRule,
	opts alertListOptions,
	dashboardUID string,
) ([]AlertRule, error) {
	matchers, err := parseAlertLabelMatchers(opts.AlertInstanceLabelFilter)
	if err != nil {
		return nil, err
	}
	filtered := make([]AlertRule, 0, len(rules))
	for _, rule := range rules {
		if !alertRuleMetadataMatches(rule, opts, dashboardUID) {
			continue
		}
		if !alertRuleStateMatches(rule.State, opts.StateFilter) {
			continue
		}
		instances := filterAlertInstances(rule.Instances, opts.StateFilter, matchers)
		if len(instances) == 0 && !includeInactiveAlertRule(rule, opts, len(matchers)) {
			continue
		}
		rule.Instances = instances
		rule.ActiveAt = earliestAlertTime(instances)
		filtered = append(filtered, rule)
	}

	return filtered, nil
}

func groupAlertRules(rules []AlertRule, groupBy []string) []AlertRule {
	groups := make(map[string]*AlertRule)
	for _, rule := range rules {
		hasLabelCoverage := alertRuleHasLabelCoverage(rule, groupBy)
		for _, instance := range rule.Instances {
			name := "No grouping"
			if hasLabelCoverage {
				name = alertGroupName(groupBy, instance.Labels)
			}
			group := groups[name]
			if group == nil {
				group = &AlertRule{
					Name: name, State: "", Folder: "", DashboardUID: "",
					ActiveAt: time.Time{}, Instances: make([]AlertInstance, 0),
				}
				groups[name] = group
			}
			group.Instances = append(group.Instances, instance)
		}
	}

	grouped := make([]AlertRule, 0, len(groups))
	for _, group := range groups {
		group.State = worstAlertState(group.Instances)
		group.ActiveAt = earliestAlertTime(group.Instances)
		sortAlertInstances(group.Instances)
		grouped = append(grouped, *group)
	}
	slices.SortStableFunc(grouped, func(left, right AlertRule) int {
		return strings.Compare(left.Name, right.Name)
	})

	return grouped
}

func alertRuleHasLabelCoverage(rule AlertRule, groupBy []string) bool {
	for _, label := range groupBy {
		found := false
		for _, instance := range rule.Instances {
			if instance.Labels[label] != "" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func alertGroupName(groupBy []string, labels map[string]string) string {
	if len(groupBy) == 0 {
		return "No grouping"
	}
	parts := make([]string, 0, len(groupBy))
	for _, label := range groupBy {
		parts = append(parts, url.QueryEscape(label)+"="+url.QueryEscape(labels[label]))
	}

	return strings.Join(parts, "&")
}

func worstAlertState(alerts []AlertInstance) string {
	worst := ""
	for _, alert := range alerts {
		state := normalizeAlertState(alert.State)
		if state == "" {
			state = "unknown"
		}
		if worst == "" || alertStateImportance(state) < alertStateImportance(worst) ||
			(alertStateImportance(state) == alertStateImportance(worst) && state < worst) {
			worst = state
		}
	}

	return worst
}

func sortAlertInstances(alerts []AlertInstance) {
	slices.SortStableFunc(alerts, func(left, right AlertInstance) int {
		if labels := strings.Compare(alertLabelsSortKey(left.Labels), alertLabelsSortKey(right.Labels)); labels != 0 {
			return labels
		}
		if state := strings.Compare(normalizeAlertState(left.State), normalizeAlertState(right.State)); state != 0 {
			return state
		}

		return left.ActiveAt.Compare(right.ActiveAt)
	})
}

func alertLabelsSortKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strconv.Quote(key)+"="+strconv.Quote(labels[key]))
	}

	return strings.Join(parts, ",")
}

func alertRuleMetadataMatches(rule AlertRule, opts alertListOptions, dashboardUID string) bool {
	if opts.DashboardAlerts && dashboardUID != "" && rule.DashboardUID != dashboardUID {
		return false
	}
	if opts.Folder != nil && opts.Folder.UID == "" &&
		opts.Folder.Title != "" && rule.Folder != opts.Folder.Title {
		return false
	}
	if opts.AlertName != "" &&
		!strings.Contains(strings.ToLower(rule.Name), strings.ToLower(opts.AlertName)) {
		return false
	}

	return true
}

func includeInactiveAlertRule(rule AlertRule, opts alertListOptions, matcherCount int) bool {
	return opts.ShowInactiveAlerts &&
		normalizeAlertState(rule.State) == alertStateNormal &&
		alertStateMatches(rule.State, opts.StateFilter) &&
		matcherCount == 0
}

func filterAlertInstances(
	alerts []AlertInstance,
	stateFilter map[string]bool,
	matchers []alertLabelMatcher,
) []AlertInstance {
	filtered := make([]AlertInstance, 0, len(alerts))
	for _, alert := range alerts {
		if !alertStateMatches(alert.State, stateFilter) {
			continue
		}
		if !alertLabelsMatch(alert.Labels, matchers) {
			continue
		}
		filtered = append(filtered, alert)
	}

	return filtered
}

func alertRuleStateMatches(state string, filter map[string]bool) bool {
	switch normalizeAlertState(state) {
	case alertStateError, alertStateNoData:
		return true
	default:
		return alertStateMatches(state, filter)
	}
}

func alertStateMatches(state string, filter map[string]bool) bool {
	if len(filter) == 0 {
		return true
	}
	state = normalizeAlertState(state)
	for allowed, enabled := range filter {
		if enabled && normalizeAlertState(allowed) == state {
			return true
		}
	}

	return false
}

func normalizeAlertState(state string) string {
	normalized := strings.ToLower(strings.TrimSpace(state))
	if reason := strings.Index(normalized, " ("); reason >= 0 {
		normalized = normalized[:reason]
	}
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "alerting", alertStateFiring:
		return alertStateFiring
	case "inactive", "ok", alertStateNormal:
		return alertStateNormal
	case alertStateNoData, "nodata":
		return alertStateNoData
	case "execution_error", "executionerror", alertStateError:
		return alertStateError
	default:
		return normalized
	}
}

func earliestAlertTime(alerts []AlertInstance) time.Time {
	var earliest time.Time
	for _, alert := range alerts {
		if normalizeAlertState(alert.State) == alertStateNormal || alert.ActiveAt.IsZero() {
			continue
		}
		if earliest.IsZero() || alert.ActiveAt.Before(earliest) {
			earliest = alert.ActiveAt
		}
	}

	return earliest
}

func sortAlertRules(rules []AlertRule, order int) {
	slices.SortStableFunc(rules, func(left, right AlertRule) int {
		switch order {
		case 2:
			return strings.Compare(strings.ToLower(right.Name), strings.ToLower(left.Name))
		case 3:
			return alertStateImportance(left.State) - alertStateImportance(right.State)
		case 4:
			return left.ActiveAt.Compare(right.ActiveAt)
		case 5:
			return right.ActiveAt.Compare(left.ActiveAt)
		default:
			return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
		}
	})
}

func alertStateImportance(state string) int {
	switch normalizeAlertState(state) {
	case alertStateFiring, alertStateError:
		return 1
	case alertStateNoData:
		return 2
	case "pending", "recovering":
		return 3
	case alertStateNormal:
		return 4
	default:
		return 5
	}
}

type alertLabelMatcher struct {
	name  string
	op    string
	value string
	re    *regexp.Regexp
}

var (
	errInvalidAlertLabelFilter = errors.New("invalid alert instance label filter")
	alertMatcherOperator       = regexp.MustCompile(`\s*(=~|!~|!=|=)\s*`)
)

func parseAlertLabelMatchers(input string) ([]alertLabelMatcher, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	input = strings.TrimPrefix(input, "{")
	input = strings.TrimSuffix(input, "}")
	parts := splitAlertMatchers(input)
	matchers := make([]alertLabelMatcher, 0, len(parts))
	for _, part := range parts {
		location := alertMatcherOperator.FindStringIndex(part)
		if location == nil {
			return nil, fmt.Errorf("%w: %s", errInvalidAlertLabelFilter, input)
		}
		name := strings.TrimSpace(part[:location[0]])
		op := strings.TrimSpace(part[location[0]:location[1]])
		value := strings.TrimSpace(part[location[1]:])
		if name == "" || value == "" {
			return nil, fmt.Errorf("%w: %s", errInvalidAlertLabelFilter, input)
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		matcher := alertLabelMatcher{name: name, op: op, value: value, re: nil}
		if op == "=~" || op == "!~" {
			compiled, err := regexp.Compile("^(?:" + value + ")$")
			if err != nil {
				return nil, fmt.Errorf("%w: %s", errInvalidAlertLabelFilter, input)
			}
			matcher.re = compiled
		}
		matchers = append(matchers, matcher)
	}

	return matchers, nil
}

func splitAlertMatchers(input string) []string {
	parts := make([]string, 0, 2)
	start := 0
	quoted := false
	escaped := false
	for index, char := range input {
		switch {
		case escaped:
			escaped = false
		case char == '\\' && quoted:
			escaped = true
		case char == '"':
			quoted = !quoted
		case char == ',' && !quoted:
			parts = append(parts, input[start:index])
			start = index + 1
		}
	}
	parts = append(parts, input[start:])

	return parts
}

func alertLabelsMatch(labels map[string]string, matchers []alertLabelMatcher) bool {
	for _, matcher := range matchers {
		value := labels[matcher.name]
		switch matcher.op {
		case "=":
			if value != matcher.value {
				return false
			}
		case "!=":
			if value == matcher.value {
				return false
			}
		case "=~":
			if !matcher.re.MatchString(value) {
				return false
			}
		case "!~":
			if matcher.re.MatchString(value) {
				return false
			}
		}
	}

	return true
}
