package grafana

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type formatTimeOptions struct {
	TimeField    string  `json:"timeField"`
	OutputFormat *string `json:"outputFormat"`
	Timezone     string  `json:"timezone"`
	UseTimezone  bool    `json:"useTimezone"`
}

func applyFormatTime(result *QueryResult, raw json.RawMessage, dashboardTimezone string) {
	var opts formatTimeOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		result.Warnings = append(result.Warnings, "invalid formatTime options")
		return
	}
	location := TimeRangeLocation(opts.Timezone)
	if opts.Timezone == "" {
		location = TimeRangeLocation(dashboardTimezone)
	}
	for refID, data := range result.Results {
		for frameIndex := range data.Frames {
			formatFrameTime(&data.Frames[frameIndex], opts, location)
		}
		result.Results[refID] = data
	}
}

func formatFrameTime(frame *DataFrame, opts formatTimeOptions, location *time.Location) {
	if opts.TimeField == "" {
		return
	}
	for fieldIndex, field := range frame.Schema.Fields {
		if field.Type != fieldTypeTime || groupFieldDisplayName(field) != opts.TimeField ||
			fieldIndex >= len(frame.Data.Values) {
			continue
		}
		var values []any
		if err := json.Unmarshal(frame.Data.Values[fieldIndex], &values); err != nil {
			continue
		}
		formatted := make([]string, len(values))
		for index, value := range values {
			parsed, ok := parseFormatTimeValue(value, location)
			if !ok {
				formatted[index] = "Invalid date"
				continue
			}
			formatted[index] = formatMomentTime(parsed.In(location), opts.OutputFormat)
		}
		frame.Data.Values[fieldIndex] = mustMarshal(formatted)
		frame.Schema.Fields[fieldIndex].Type = fieldTypeString
	}
}

func parseFormatTimeValue(value any, location *time.Location) (time.Time, bool) {
	switch typed := value.(type) {
	case float64:
		return time.UnixMilli(int64(typed)), true
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return parsed, true
		}
		if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", typed, location); err == nil {
			return parsed, true
		}
		if milliseconds, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return time.UnixMilli(milliseconds), true
		}
	}
	return time.Time{}, false
}

func formatMomentTime(value time.Time, format *string) string {
	if format == nil {
		return value.Format(time.RFC3339)
	}
	var output strings.Builder
	for index := 0; index < len(*format); {
		if (*format)[index] == '[' {
			end := strings.IndexByte((*format)[index+1:], ']')
			if end < 0 {
				output.WriteByte((*format)[index])
				index++
				continue
			}
			output.WriteString((*format)[index+1 : index+1+end])
			index += end + 2
			continue
		}
		token := nextMomentToken((*format)[index:])
		if token == "" {
			output.WriteByte((*format)[index])
			index++
			continue
		}
		output.WriteString(momentTokenValue(value, token))
		index += len(token)
	}

	return output.String()
}

func nextMomentToken(value string) string {
	for _, token := range []string{
		"YYYY", "MMMM", "MMM", "SSS", "MM", "DD", "HH", "hh", "mm", "ss", "ZZ",
		"YY", "M", "D", "H", "h", "m", "s", "A", "a", "Z",
	} {
		if strings.HasPrefix(value, token) {
			return token
		}
	}

	return ""
}

//nolint:cyclop // token-to-value mapping is intentionally explicit
func momentTokenValue(value time.Time, token string) string {
	if formatted, ok := momentDateTokenValue(value, token); ok {
		return formatted
	}
	hour12 := value.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	switch token {
	case "HH":
		return fmt.Sprintf("%02d", value.Hour())
	case "H":
		return strconv.Itoa(value.Hour())
	case "hh":
		return fmt.Sprintf("%02d", hour12)
	case "h":
		return strconv.Itoa(hour12)
	case "mm":
		return fmt.Sprintf("%02d", value.Minute())
	case "m":
		return strconv.Itoa(value.Minute())
	case "ss":
		return fmt.Sprintf("%02d", value.Second())
	case "s":
		return strconv.Itoa(value.Second())
	case "SSS":
		return fmt.Sprintf("%03d", value.Nanosecond()/int(time.Millisecond))
	case "A":
		return strings.ToUpper(value.Format("PM"))
	case "a":
		return strings.ToLower(value.Format("PM"))
	case "ZZ", "Z":
		_, offset := value.Zone()
		sign := "+"
		if offset < 0 {
			sign = "-"
			offset = -offset
		}
		separator := ""
		if token == "Z" {
			separator = ":"
		}
		return fmt.Sprintf("%s%02d%s%02d", sign, offset/3600, separator, offset%3600/60)
	default:
		return token
	}
}

func momentDateTokenValue(value time.Time, token string) (string, bool) {
	switch token {
	case "YYYY":
		return fmt.Sprintf("%04d", value.Year()), true
	case "YY":
		return fmt.Sprintf("%02d", value.Year()%100), true
	case "MMMM":
		return value.Month().String(), true
	case "MMM":
		return value.Month().String()[:3], true
	case "MM":
		return fmt.Sprintf("%02d", int(value.Month())), true
	case "M":
		return strconv.Itoa(int(value.Month())), true
	case "DD":
		return fmt.Sprintf("%02d", value.Day()), true
	case "D":
		return strconv.Itoa(value.Day()), true
	default:
		return "", false
	}
}
