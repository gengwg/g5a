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
	allPanels []grafana.Panel,
) (*grafana.QueryResult, error) {
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
