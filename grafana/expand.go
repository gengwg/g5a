package grafana

import (
	"encoding/json"
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
		expanded, height := expandRowGroup(group, variableByName, options, selections)
		for index := range expanded {
			expanded[index].Panel.GridPos.Y += yOffset
			expanded[index].Key = "group-" + strconv.Itoa(groupIndex) + ":" + expanded[index].Key
		}
		instances = append(instances, expanded...)
		if height > 0 {
			yOffset += height - group.height
		}
	}

	return instances
}

func expandRowGroup(
	group panelExpansionGroup,
	variableByName map[string]TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) ([]PanelInstance, int) {
	values, variable, repeated := rowRepeatValues(group.row, variableByName, options, selections)
	if len(values) == 0 {
		return nil, 0
	}
	instances := make([]PanelInstance, 0, len(group.panels))
	groupOffset := 0
	for _, value := range values {
		rowSelections := cloneStrings(selections)
		if repeated {
			rowSelections[variable.Name] = value
		}
		expanded, heightOffset := expandRowGroupValue(group, variableByName, options, rowSelections)
		height := group.height + heightOffset
		for index := range expanded {
			expanded[index].Panel.GridPos.Y += groupOffset
			if repeated {
				expanded[index].Key = group.row.key + ":" + value + ":" + expanded[index].Key
			}
		}
		instances = append(instances, expanded...)
		groupOffset += height
	}

	return instances, groupOffset
}

//nolint:cyclop // direct leaves and nested child groups share one ordered layout pipeline
func expandRowGroupValue(
	group panelExpansionGroup,
	variableByName map[string]TemplateVariable,
	options map[string][]string,
	selections map[string]string,
) ([]PanelInstance, int) {
	variables := make([]TemplateVariable, 0, len(variableByName))
	for _, variable := range variableByName {
		variables = append(variables, variable)
	}
	direct := make([]Panel, 0, len(group.panels))
	nested := make([]Panel, 0, len(group.panels))
	for _, panel := range group.panels {
		if group.row != nil && group.row.parent == nil && panel.row == group.row {
			direct = append(direct, panel)
		} else {
			nested = append(nested, panel)
		}
	}
	if group.row == nil || len(nested) == 0 {
		expanded := expandPanelRepeats(group.panels, variables, options, selections)
		return expanded, expandedGroupHeight(group, expanded) - group.height
	}

	instances := make([]PanelInstance, 0, len(group.panels))
	childOffset := 0
	if len(direct) > 0 {
		expanded, height := expandDirectPanels(direct, variables, options, selections, group.baseY)
		instances = append(instances, expanded...)
		childOffset += height
	}

	childGroups := groupPanelsAtRow(nested)
	for _, child := range childGroups {
		expanded, height := expandRowGroup(child, variableByName, options, selections)
		for index := range expanded {
			expanded[index].Panel.GridPos.Y += childOffset
		}
		instances = append(instances, expanded...)
		if height > 0 {
			childOffset += height - child.height
		}
	}

	return instances, childOffset
}

func expandDirectPanels(
	panels []Panel,
	variables []TemplateVariable,
	options map[string][]string,
	selections map[string]string,
	baseY int,
) ([]PanelInstance, int) {
	group := panelExpansionGroup{row: nil, panels: panels, baseY: baseY, height: 0}
	bottom := baseY
	for _, panel := range panels {
		bottom = max(bottom, panel.GridPos.Y+panel.GridPos.H)
	}
	height := max(0, bottom-baseY)
	expanded := expandPanelRepeats(panels, variables, options, selections)

	return expanded, expandedGroupHeight(group, expanded) - height
}

func groupPanelsAtRow(panels []Panel) []panelExpansionGroup {
	groups := make([]panelExpansionGroup, 0, len(panels))
	for _, panel := range panels {
		if panel.row == nil || panel.row.parent == nil {
			continue
		}
		childRow := panel.row.parent
		if len(groups) == 0 || groups[len(groups)-1].row.key != childRow.key {
			groups = append(groups, panelExpansionGroup{
				row: childRow, panels: nil, baseY: childRow.baseY, height: childRow.height,
			})
		}
		panel.row = childRow
		groups[len(groups)-1].panels = append(groups[len(groups)-1].panels, panel)
	}

	return groups
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
		if len(groups) == 0 || groups[len(groups)-1].row == nil || panel.row == nil ||
			groups[len(groups)-1].row.key != panel.row.key {
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

func withoutAllToken(options []string) []string {
	values := make([]string, 0, len(options))
	for _, value := range options {
		if value != grafanaAllToken {
			values = append(values, value)
		}
	}

	return values
}

// RepeatValues returns the concrete values for a repeated panel variable.
func RepeatValues(
	variable TemplateVariable,
	options []string,
	selected string,
) []string {
	options = withoutAllToken(options)
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
	resolveTextPanelOptions(&panel, variables, overrides)

	return PanelInstance{
		Key:          key,
		Panel:        panel,
		Variables:    overrides,
		RepeatIndex:  repeatIndex,
		RepeatLength: max(repeatLength, 1),
	}
}

func resolveTextPanelOptions(panel *Panel, variables []TemplateVariable, overrides map[string]string) {
	if panel.Type != "text" || len(panel.Options) == 0 {
		return
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(panel.Options, &options); err != nil {
		return
	}
	var content string
	raw, ok := options["content"]
	if !ok || json.Unmarshal(raw, &content) != nil {
		return
	}
	options["content"] = mustMarshal(ResolveVariables(content, variables, overrides))
	panel.Options = mustMarshal(options)
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
