package grafanatui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

type alertListRenderer struct{}

type alertRenderOptions struct {
	ViewMode      string `json:"viewMode"`
	MaxItems      int    `json:"maxItems"`
	ShowInstances bool   `json:"showInstances"`
}

func (r *alertListRenderer) Render(
	result *grafana.QueryResult,
	panel grafana.Panel,
	width, height int,
	_ map[string]bool,
) string {
	if result == nil || len(result.Alerts) == 0 {
		return lipgloss.Place(
			width, height, lipgloss.Center, lipgloss.Center,
			"No alerts matching filters",
		)
	}

	var opts alertRenderOptions
	_ = json.Unmarshal(panel.Options, &opts)
	if opts.ViewMode == panelTypeStat {
		count := strconv.Itoa(len(result.Alerts))
		return lipgloss.Place(
			width, height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Bold(true).Render(count+" alert rules"),
		)
	}

	lines := renderAlertRuleLines(result.Alerts, opts, width, height)
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func renderAlertRuleLines(
	alerts []grafana.AlertRule,
	opts alertRenderOptions,
	width int,
	height int,
) []string {
	rules := alerts
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 20
	}
	if len(rules) > maxItems {
		rules = rules[:maxItems]
	}
	lines := make([]string, 0, min(len(rules), height))
	for _, rule := range rules {
		if len(lines) >= height {
			break
		}
		line := fmt.Sprintf("%-10s %s  %d instance(s)", alertStateText(rule.State), rule.Name, len(rule.Instances))
		if rule.Folder != "" {
			line += "  [" + rule.Folder + "]"
		}
		line = truncateTableText(line, width)
		lines = append(lines, alertStateStyle(rule.State).Render(line))
		lines = appendAlertInstanceLines(lines, rule, opts.ShowInstances, width, height)
	}
	if len(alerts) > len(rules) && len(lines) < height {
		lines = append(lines, fmt.Sprintf("... and %d more alert rules", len(alerts)-len(rules)))
	}

	return lines
}

func appendAlertInstanceLines(
	lines []string,
	rule grafana.AlertRule,
	show bool,
	width int,
	height int,
) []string {
	if !show {
		return lines
	}
	for _, instance := range rule.Instances {
		if len(lines) >= height {
			break
		}
		lines = append(lines, truncateTableText(
			"  "+formatAlertLabels(instance.Labels), width,
		))
	}

	return lines
}

func alertStateText(state string) string {
	state = strings.ToUpper(strings.TrimSpace(state))
	if state == "" {
		return "UNKNOWN"
	}

	return state
}

func alertStateStyle(state string) lipgloss.Style {
	color := grafanaBlue
	switch strings.ToLower(state) {
	case "firing", "alerting", "error", "execution_error":
		color = grafanaRed
	case "pending", "recovering", "no_data", "nodata":
		color = grafanaOrange
	case "normal", "inactive", "ok":
		color = grafanaGreen
	}

	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

func formatAlertLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		if key != "alertname" && key != "grafana_folder" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, labels[key]))
	}

	return "{" + strings.Join(parts, ", ") + "}"
}
