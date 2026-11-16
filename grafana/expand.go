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
func ExpandPanels(
	panels []Panel,
	variables []TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) []PanelInstance {
	flat := FlattenPanels(panels)
	if hasRowRepeat(flat) {
		return expandRepeatedRows(flat, variables, options, selections)
	}

	return expandPanelRepeats(flat, variables, options, selections)
}

//nolint:funlen // repeat expansion and downstream layout offsets share one pass
func expandPanelRepeats(
	panels []Panel,
	variables []TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) []PanelInstance {
	flat := panels
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

type panelExpansionGroup struct {
	row    *panelRow
	panels []Panel
	baseY  int
	height int
}

func hasRowRepeat(panels []Panel) bool {
	for _, panel := range panels {
		if panel.row != nil && panel.row.repeat != "" {
			return true
		}
	}

	return false
}

func expandRepeatedRows(
	panels []Panel,
	variables []TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) []PanelInstance {
	groups := groupPanelsForExpansion(panels)
	variableByName := make(map[string]TemplateVariable, len(variables))
	for _, variable := range variables {
		variableByName[variable.Name] = variable
	}

	instances := make([]PanelInstance, 0, len(panels))
	yOffset := 0
	for groupIndex, group := range groups {
		values, variable, repeated := rowRepeatValues(
			group.row, variableByName, options, selections,
		)
		if len(values) == 0 {
			continue
		}
		groupOffset := 0
		for _, value := range values {
			rowSelections := cloneStrings(selections)
			if repeated {
				rowSelections[variable.Name] = value
			}
			expanded := expandPanelRepeats(
				group.panels, variables, options, rowSelections,
			)
			height := expandedGroupHeight(group, expanded)
			for index := range expanded {
				expanded[index].Panel.GridPos.Y += yOffset + groupOffset
				expanded[index].Key = "group-" + strconv.Itoa(groupIndex) + ":" + expanded[index].Key
				if repeated {
					expanded[index].Key = group.row.key + ":" + value + ":" + expanded[index].Key
				}
			}
			instances = append(instances, expanded...)
			groupOffset += height
		}
		if groupOffset > 0 {
			yOffset += groupOffset - group.height
		}
	}

	return instances
}

func groupPanelsForExpansion(panels []Panel) []panelExpansionGroup {
	sort.SliceStable(panels, func(i, j int) bool {
		if panels[i].GridPos.Y == panels[j].GridPos.Y {
			return panels[i].GridPos.X < panels[j].GridPos.X
		}

		return panels[i].GridPos.Y < panels[j].GridPos.Y
	})
	groups := make([]panelExpansionGroup, 0, len(panels))
	for _, panel := range panels {
		if panel.row == nil {
			groups = append(groups, panelExpansionGroup{
				row: nil, panels: []Panel{panel}, baseY: panel.GridPos.Y, height: panel.GridPos.H,
			})
			continue
		}
		if len(groups) == 0 || groups[len(groups)-1].row != panel.row {
			groups = append(groups, panelExpansionGroup{
				row: panel.row, panels: nil, baseY: panel.row.baseY, height: panel.row.height,
			})
		}
		groups[len(groups)-1].panels = append(groups[len(groups)-1].panels, panel)
	}

	return groups
}

func rowRepeatValues(
	row *panelRow,
	variableByName map[string]TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) ([]string, TemplateVariable, bool) {
	var empty TemplateVariable
	if row == nil || row.repeat == "" {
		return []string{""}, empty, false
	}
	variable, ok := variableByName[row.repeat]
	if !ok {
		return []string{""}, empty, false
	}
	selected := selections[variable.Name]
	if selected == "" {
		selected = resolveCurrentValue(variable)
	}

	return RepeatValues(variable, options[variable.Name], selected), variable, true
}

func expandedGroupHeight(group panelExpansionGroup, instances []PanelInstance) int {
	bottom := group.baseY
	for _, instance := range instances {
		bottom = max(bottom, instance.Panel.GridPos.Y+instance.Panel.GridPos.H)
	}

	return max(group.height, bottom-group.baseY)
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
