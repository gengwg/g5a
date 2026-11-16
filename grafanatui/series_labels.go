package grafanatui

import (
	"sort"
	"strconv"
	"strings"
)

func firstSeriesLabelValue(name string) (string, bool) {
	values, firstKey := seriesLabelPairs(name)
	if firstKey == "" {
		return "", false
	}

	return values[firstKey], true
}

func preferredCategoryLabel(values map[string]string, firstKey string) (string, bool) {
	if value := values["vendor"]; value != "" {
		return value, true
	}
	if firstKey != "" {
		value := values[firstKey]

		return value, value != ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := values[key]; value != "" {
			return value, true
		}
	}

	return "", false
}

func seriesLabelPairs(name string) (map[string]string, string) {
	start := strings.Index(name, "{")
	end := strings.LastIndex(name, "}")
	if start < 0 || end <= start+1 {
		return nil, ""
	}
	values := make(map[string]string)
	firstKey := ""
	currentKey := ""
	for _, label := range splitSeriesLabels(name[start+1 : end]) {
		key, value, found := strings.Cut(label, "=")
		if !found {
			if currentKey != "" {
				values[currentKey] += "," + label
			}
			continue
		}
		key = strings.TrimSpace(key)
		if firstKey == "" {
			firstKey = key
		}
		currentKey = key
		values[currentKey] = unquoteSeriesLabel(value)
	}

	return values, firstKey
}

func unquoteSeriesLabel(value string) string {
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}

	return strings.Trim(value, "\"")
}

func splitSeriesLabels(input string) []string {
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
