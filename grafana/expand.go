package grafana

import (
	"maps"
	"sort"
	"strconv"
	"strings"
)

// PanelInstance is a concrete panel after repeating and variable scoping.
// Key remains stable across refreshes when the selected variables do not change.
type PanelInstance struct {
	Key          string
	Panel        Panel
	Variables    map[string]string
	RepeatIndex  int
	RepeatLength int
}

// ExpandPanels flattens rows and expands repeated panels using the resolved
// variable options and current selections. Subsequent panels are moved down to
// make room for repeated rows, matching Grafana's runtime layout.
//
//nolint:funlen // repeat expansion and downstream layout offsets share one pass
func ExpandPanels(
	panels []Panel,
	variables []TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) []PanelInstance {
	flat := FlattenPanels(panels)
	sort.SliceStable(flat, func(i, j int) bool {
		if flat[i].GridPos.Y == flat[j].GridPos.Y {
			return flat[i].GridPos.X < flat[j].GridPos.X
		}

		return flat[i].GridPos.Y < flat[j].GridPos.Y
	})

	instances := make([]PanelInstance, 0, len(flat))
	variableByName := make(map[string]TemplateVariable, len(variables))
	for _, variable := range variables {
		variableByName[variable.Name] = variable
	}

	yOffset := 0
	for index, panel := range flat {
		baseOverrides := cloneStrings(selections)
		panel.GridPos.Y += yOffset

		if panel.Repeat == "" {
			instances = append(instances, newPanelInstance(
				panel,
				variables,
				baseOverrides,
				instanceKey(panel, index, ""),
				0,
				1,
			))
			continue
		}

		variable, ok := variableByName[panel.Repeat]
		if !ok {
			instances = append(instances, newPanelInstance(
				panel,
				variables,
				baseOverrides,
				instanceKey(panel, index, ""),
				0,
				1,
			))
			continue
		}

		selected := selections[variable.Name]
		if selected == "" {
			selected = resolveCurrentValue(variable)
		}
		values := RepeatValues(variable, options[variable.Name], selected)
		perRow := repeatPerRow(panel, len(values))
		rows := 0
		if perRow > 0 {
			rows = (len(values) + perRow - 1) / perRow
		}
		if rows > 1 {
			yOffset += (rows - 1) * panel.GridPos.H
		}

		for valueIndex, value := range values {
			repeated := panel
			repeated.GridPos.Y = panel.GridPos.Y + (valueIndex/perRow)*panel.GridPos.H
			overrides := cloneStrings(baseOverrides)
			overrides[variable.Name] = value

			instances = append(instances, newPanelInstance(
				repeated,
				variables,
				overrides,
				instanceKey(panel, index, value),
				valueIndex%perRow,
				perRow,
			))
		}
	}

	return instances
}

// RepeatValues returns the concrete values for a repeated panel variable.
func RepeatValues(
	variable TemplateVariable,
	options []string,
	selected string,
) []string {
	if isAllSelection(variable, selected) {
		return append([]string(nil), options...)
	}

	if variable.Multi && strings.Contains(selected, ",") {
		parts := strings.Split(selected, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if value := strings.TrimSpace(part); value != "" {
				values = append(values, value)
			}
		}
		return values
	}

	if selected == "" {
		return []string{}
	}

	return []string{selected}
}

func newPanelInstance(
	panel Panel,
	variables []TemplateVariable,
	overrides map[string]string,
	key string,
	repeatIndex int,
	repeatLength int,
) PanelInstance {
	panel.Title = ResolveVariables(panel.Title, variables, overrides)

	return PanelInstance{
		Key:          key,
		Panel:        panel,
		Variables:    overrides,
		RepeatIndex:  repeatIndex,
		RepeatLength: max(repeatLength, 1),
	}
}

func repeatPerRow(panel Panel, valueCount int) int {
	if valueCount == 0 {
		return 1
	}
	if panel.RepeatDirection == "v" {
		return 1
	}
	if panel.MaxPerRow > 0 {
		return min(panel.MaxPerRow, valueCount)
	}
	if panel.GridPos.W > 0 && panel.GridPos.W < 24 {
		return min(24/panel.GridPos.W, valueCount)
	}

	return 1
}

func isAllSelection(variable TemplateVariable, selected string) bool {
	if !variable.IncludeAll {
		return false
	}
	if selected == "" || selected == grafanaAllToken || selected == ".*" {
		return true
	}

	return variable.AllValue != "" && selected == variable.AllValue
}

func instanceKey(panel Panel, index int, repeatValue string) string {
	id := panel.ID
	if id == 0 {
		id = -index - 1
	}
	if repeatValue != "" {
		return strconv.Itoa(id) + ":" + repeatValue
	}

	return strconv.Itoa(id)
}

func cloneStrings(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	maps.Copy(output, input)

	return output
}
