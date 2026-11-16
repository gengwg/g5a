package grafanatui

import (
	"context"
	"fmt"

	"github.com/gengwg/g5a/grafana"
)

func queryPanelData(
	ctx context.Context,
	client *grafana.Client,
	dashboardUID string,
	panel grafana.Panel,
	timeRange grafana.TimeRange,
	maxDataPoints int,
	variables []grafana.TemplateVariable,
	variableOverrides map[string]string,
	variableOptions map[string][]string,
	allPanels []grafana.Panel,
) (*grafana.QueryResult, error) {
	variables = withResolvedVariableOptions(variables, variableOptions)
	if panel.Type == "alertlist" {
		result, err := client.QueryAlertList(ctx, panel, dashboardUID, variables, variableOverrides)
		if err != nil {
			return nil, fmt.Errorf("querying alert list: %w", err)
		}
		return result, nil
	}

	result, err := client.QueryPanel(
		ctx, panel, timeRange, maxDataPoints,
		variables, variableOverrides, allPanels,
	)
	if err != nil {
		return nil, fmt.Errorf("querying panel data: %w", err)
	}

	return result, nil
}

func withResolvedVariableOptions(
	variables []grafana.TemplateVariable,
	options map[string][]string,
) []grafana.TemplateVariable {
	resolved := append([]grafana.TemplateVariable(nil), variables...)
	for index := range resolved {
		values, ok := options[resolved[index].Name]
		if !ok {
			continue
		}
		resolved[index].Options = make([]grafana.TemplateVariableOption, 0, len(values))
		for _, value := range values {
			resolved[index].Options = append(resolved[index].Options, grafana.TemplateVariableOption{
				Text: value, Value: value, Selected: false,
			})
		}
	}

	return resolved
}
