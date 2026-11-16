package grafana

import (
	"encoding/json"
	"regexp"
	"strings"
)

// grafanaAllToken is Grafana's special value indicating "All" selected.
const grafanaAllToken = "$__all"

// variablePattern matches $var, ${var}, and ${var:format} patterns.
var variablePattern = regexp.MustCompile(`\$\{(\w+)(?::([^}]*))?\}|\$(\w+)`)

// ResolveVariables replaces variable references using their current values.
// Overrides take precedence. Prometheus-style multi-value selections are
// regex-escaped and pipe-separated unless a format modifier says otherwise.
func ResolveVariables(
	expr string,
	variables []TemplateVariable,
	overrides map[string]string,
) string {
	return resolveVariables(expr, variables, overrides, 0)
}

func resolveVariables(
	expr string,
	variables []TemplateVariable,
	overrides map[string]string,
	depth int,
) string {
	variableMap := make(map[string]TemplateVariable, len(variables))
	for _, variable := range variables {
		variableMap[variable.Name] = variable
	}

	return variablePattern.ReplaceAllStringFunc(expr, func(match string) string {
		name, format := variableReference(match)
		variable, ok := variableMap[name]
		if !ok {
			return match
		}

		override := ""
		if overrides != nil {
			override = overrides[name]
		}

		return formatVariableValue(variable, override, format, variables, overrides, depth)
	})
}

func variableReference(match string) (name, format string) {
	submatches := variablePattern.FindStringSubmatch(match)
	if submatches == nil {
		return "", ""
	}
	if submatches[1] != "" {
		return submatches[1], submatches[2]
	}

	return submatches[3], ""
}

//nolint:cyclop // Grafana has several documented variable output formats
func formatVariableValue(
	variable TemplateVariable,
	override string,
	format string,
	variables []TemplateVariable,
	overrides map[string]string,
	depth int,
) string {
	values, customAll := variableValues(variable, override)
	if customAll {
		if depth >= 3 {
			return variable.AllValue
		}

		return resolveVariables(variable.AllValue, variables, overrides, depth+1)
	}
	if len(values) == 1 && values[0] == grafanaAllToken {
		values = realVariableOptions(variable)
	}
	if len(values) == 0 {
		return ""
	}

	switch format {
	case "raw":
		return strings.Join(values, ",")
	case "pipe":
		return strings.Join(values, "|")
	case "csv":
		return strings.Join(values, ",")
	case "json":
		encoded, err := json.Marshal(values)
		if err == nil {
			return string(encoded)
		}
		return ""
	case "queryparam":
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, "var-"+variable.Name+"="+value)
		}
		return strings.Join(parts, "&")
	case "regex":
		return regexValues(values)
	default:
		if len(values) > 1 || variable.Multi || variable.IncludeAll {
			return regexValues(values)
		}
		return values[0]
	}
}

//nolint:cyclop,nestif // saved Grafana selections include strings, arrays, and All forms
func variableValues(
	variable TemplateVariable,
	override string,
) (values []string, customAll bool) {
	if override != "" {
		if override == grafanaAllToken || (variable.IncludeAll && override == ".*") {
			return []string{grafanaAllToken}, variable.AllValue != ""
		}
		if variable.IncludeAll && variable.AllValue != "" && override == variable.AllValue {
			return nil, true
		}
		if variable.Multi && strings.Contains(override, ",") {
			return splitVariableValues(override), false
		}
		return []string{override}, false
	}

	if len(variable.Current.Value) > 0 {
		var many []string
		if err := json.Unmarshal(variable.Current.Value, &many); err == nil {
			if len(many) == 1 && many[0] == grafanaAllToken && variable.AllValue != "" {
				return nil, true
			}
			return many, false
		}

		var one string
		if err := json.Unmarshal(variable.Current.Value, &one); err == nil {
			if one == grafanaAllToken && variable.AllValue != "" {
				return nil, true
			}
			return []string{one}, false
		}
	}

	text := rawMessageToString(variable.Current.Text)
	if text == "" {
		return nil, false
	}

	return []string{text}, false
}

func realVariableOptions(variable TemplateVariable) []string {
	values := make([]string, 0, len(variable.Options))
	for _, option := range variable.Options {
		if option.Value != "" && option.Value != grafanaAllToken {
			values = append(values, option.Value)
		}
	}

	return values
}

func splitVariableValues(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}

	return values
}

func regexValues(values []string) string {
	escaped := make([]string, 0, len(values))
	for _, value := range values {
		if value == grafanaAllToken {
			return ".*"
		}
		escaped = append(escaped, regexp.QuoteMeta(value))
	}

	if len(escaped) == 1 {
		return escaped[0]
	}

	return "(" + strings.Join(escaped, "|") + ")"
}

// resolveCurrentValue extracts a display/query value from the saved selection.
func resolveCurrentValue(variable TemplateVariable) string {
	values, customAll := variableValues(variable, "")
	if customAll {
		return variable.AllValue
	}
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 && values[0] == grafanaAllToken && variable.AllValue != "" {
		return variable.AllValue
	}

	return strings.Join(values, ",")
}

// rawMessageToString extracts a string or joins an array of strings.
func rawMessageToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		return stringValue
	}

	var arrayValue []string
	if err := json.Unmarshal(raw, &arrayValue); err == nil {
		return strings.Join(arrayValue, ",")
	}

	return string(raw)
}
