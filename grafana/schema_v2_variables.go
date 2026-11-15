package grafana

import (
	"encoding/json"
	"errors"
	"fmt"
)

const v2CustomVariableKind = "CustomVariable"

var (
	errUnsupportedV2Variable = errors.New("unsupported dashboard v2 variable kind")
	errUnsupportedV2Hide     = errors.New("unsupported dashboard v2 hide mode")
)

type v2VariableHide int

func (h *v2VariableHide) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		switch name {
		case "dontHide":
			*h = 0
		case "inControlsMenu":
			*h = 3
		case "hideLabel":
			*h = 1
		case "hideVariable":
			*h = 2
		default:
			return fmt.Errorf("%w: %s", errUnsupportedV2Hide, name)
		}
		return nil
	}
	var number int
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("%w: %s", errUnsupportedV2Hide, string(data))
	}
	*h = v2VariableHide(number)

	return nil
}

type v2VariableOption struct {
	Text     v2StringList `json:"text"`
	Value    v2StringList `json:"value"`
	Selected bool         `json:"selected"`
}

type v2StringList []string

func (v *v2StringList) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*v = v2StringList{text}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("decoding v2 option text/value: %w", err)
	}
	*v = values

	return nil
}

type v2VariableCommon struct {
	Name       string                  `json:"name"`
	Current    TemplateVariableCurrent `json:"current"`
	Options    []v2VariableOption      `json:"options"`
	Multi      bool                    `json:"multi"`
	IncludeAll bool                    `json:"includeAll"`
	AllValue   string                  `json:"allValue"`
	Regex      string                  `json:"regex"`
	Refresh    string                  `json:"refresh"`
	Sort       string                  `json:"sort"`
	Hide       v2VariableHide          `json:"hide"`
}

func (common v2VariableCommon) classic() TemplateVariable {
	return TemplateVariable{
		Name: common.Name, Type: "", Current: common.Current, IncludeAll: common.IncludeAll,
		AllValue: common.AllValue, Multi: common.Multi, Definition: "", Query: nil,
		Datasource: nil, Options: classicV2Options(common.Options), Regex: common.Regex,
		Sort: classicVariableSort(common.Sort), Refresh: classicVariableRefresh(common.Refresh), Hide: int(common.Hide),
	}
}

func classicV2Options(options []v2VariableOption) []TemplateVariableOption {
	classic := make([]TemplateVariableOption, 0, len(options))
	for _, option := range options {
		count := max(len(option.Text), len(option.Value))
		for index := range count {
			classic = append(classic, TemplateVariableOption{
				Text:     v2OptionValue(option.Text, index),
				Value:    v2OptionValue(option.Value, index),
				Selected: option.Selected,
			})
		}
	}

	return classic
}

func v2OptionValue(values v2StringList, index int) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	if index >= len(values) {
		return ""
	}

	return values[index]
}

func (spec dashboardV2Spec) classicVariables() ([]TemplateVariable, error) {
	variables := make([]TemplateVariable, 0, len(spec.Variables))
	for _, source := range spec.Variables {
		variable, err := classicV2Variable(source)
		if err != nil {
			return nil, err
		}
		variables = append(variables, variable)
	}

	return variables, nil
}

//nolint:cyclop // dashboard variable kinds have distinct query representations
func classicV2Variable(source dashboardV2Variable) (TemplateVariable, error) {
	switch source.Kind {
	case "DatasourceVariable":
		var spec struct {
			v2VariableCommon

			PluginID string `json:"pluginId"`
		}
		if err := json.Unmarshal(source.Spec, &spec); err != nil {
			return TemplateVariable{}, fmt.Errorf("decoding %s variable: %w", source.Kind, err)
		}
		variable := spec.classic()
		variable.Type = datasourceVariableName
		variable.Query = mustMarshal(spec.PluginID)
		return variable, nil
	case "QueryVariable":
		var spec struct {
			v2VariableCommon

			Definition string `json:"definition"`
			Query      struct {
				Group      string `json:"group"`
				Datasource *struct {
					Name string `json:"name"`
				} `json:"datasource"`
				Spec json.RawMessage `json:"spec"`
			} `json:"query"`
		}
		if err := json.Unmarshal(source.Spec, &spec); err != nil {
			return TemplateVariable{}, fmt.Errorf("decoding %s variable: %w", source.Kind, err)
		}
		variable := spec.classic()
		variable.Type = "query"
		variable.Definition = spec.Definition
		variable.Query = spec.Query.Spec
		if spec.Query.Datasource != nil {
			variable.Datasource = &DatasourceRef{
				UID: spec.Query.Datasource.Name, Type: spec.Query.Group,
			}
		}
		if variable.Definition == "" {
			var query struct {
				Expr string `json:"expr"`
			}
			if err := json.Unmarshal(spec.Query.Spec, &query); err == nil {
				variable.Definition = query.Expr
			}
		}
		return variable, nil
	case v2CustomVariableKind:
		return classicSimpleV2Variable(source, variableTypeCustom)
	case "ConstantVariable":
		return classicSimpleV2Variable(source, "constant")
	case "TextVariable":
		return classicSimpleV2Variable(source, "textbox")
	case "IntervalVariable":
		return classicSimpleV2Variable(source, "interval")
	default:
		return TemplateVariable{}, fmt.Errorf("%w: %s", errUnsupportedV2Variable, source.Kind)
	}
}

func classicSimpleV2Variable(
	source dashboardV2Variable,
	variableType string,
) (TemplateVariable, error) {
	var spec struct {
		v2VariableCommon

		Query json.RawMessage `json:"query"`
	}
	if err := json.Unmarshal(source.Spec, &spec); err != nil {
		return TemplateVariable{}, fmt.Errorf("decoding %s variable: %w", source.Kind, err)
	}
	variable := spec.classic()
	variable.Type = variableType
	variable.Query = spec.Query

	return variable, nil
}

func classicVariableSort(sort string) int {
	sorts := map[string]int{
		"disabled":        0,
		"alphabeticalAsc": 1, "alphabeticalDesc": 2,
		"numericalAsc": 3, "numericalDesc": 4,
		"alphabeticalCaseInsensitiveAsc": 5, "alphabeticalCaseInsensitiveDesc": 6,
		"naturalAsc": 7, "naturalDesc": 8,
	}
	if value, ok := sorts[sort]; ok {
		return value
	}

	return 0
}

func classicVariableRefresh(refresh string) int {
	switch refresh {
	case "onDashboardLoad":
		return 1
	case "onTimeRangeChanged":
		return 2
	default:
		return 0
	}
}
