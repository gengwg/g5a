package grafanatui

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// SI magnitude thresholds and suffixes.
const (
	grafanaGreen  = "#73BF69"
	grafanaRed    = "#F2495C"
	grafanaYellow = "#FADE2A"
	grafanaOrange = "#FF9830"
	grafanaBlue   = "#5794F2"

	siKilo = 1_000.0
	siMega = 1_000_000.0
	siGiga = 1_000_000_000.0
	siTera = 1_000_000_000_000.0
)

// Binary byte thresholds.
const (
	binaryKibi = 1024.0
	binaryMebi = 1024.0 * 1024.0
	binaryGibi = 1024.0 * 1024.0 * 1024.0
	binaryTebi = 1024.0 * 1024.0 * 1024.0 * 1024.0
)

// Decimal byte thresholds.
const (
	decKilo = 1_000.0
	decMega = 1_000_000.0
	decGiga = 1_000_000_000.0
	decTera = 1_000_000_000_000.0
)

// Time and percentage constants.
const (
	secondsPerMinute      = 60
	secondsPerHour        = 3600
	millisecondsPerSecond = 1000.0
	percentMultiplier     = 100.0
)

// unitFormatters maps Grafana unit strings to their formatting functions. A nil
// decimal count preserves the unit's default precision.
var unitFormatters = map[string]func(float64, *int) string{ //nolint:gochecknoglobals // read-only registry
	"short":       formatSI,
	"":            formatSI,
	"bytes":       formatBinaryBytes,
	"decbytes":    formatDecimalBytes,
	"bits":        formatBits,
	"s":           formatSeconds,
	"ms":          formatMilliseconds,
	"ns":          formatNanoseconds,
	"percent":     formatPercent,
	"percentunit": formatPercentUnit,
	"reqps":       formatReqPerSec,
	"ops":         formatOpsPerSec,
	"none":        formatRawNumber,
	"Bps":         formatBytesPerSec,
	"binBps":      formatBinaryBytesPerSec,
	"decBps":      formatDecimalBytesPerSec,
	"bps":         formatBitsPerSec,
	"pps":         formatPacketsPerSec,
	"iops":        formatIOPS,
	"hertz":       formatHertz,
	"watt":        formatWatt,
	"kwatt":       formatKilowatt,
	"voltamp":     formatVoltAmp,
}

const (
	labelPosInf = "+Inf"
	labelNegInf = "-Inf"
)

// FormatValue formats a float64 value according to the given Grafana
// unit string.
func FormatValue(value float64, unit string) string {
	return formatValue(value, unit, nil)
}

func formatValue(value float64, unit string, decimals *int) string {
	if math.IsNaN(value) {
		return "NaN"
	}

	if math.IsInf(value, 1) {
		return labelPosInf
	}

	if math.IsInf(value, -1) {
		return labelNegInf
	}

	if formatter, ok := unitFormatters[unit]; ok {
		return formatter(value, decimals)
	}

	return formatCustomUnit(value, unit, decimals)
}

func formatCustomUnit(value float64, unit string, decimals *int) string {
	number := formatRawNumber(value, decimals)
	switch {
	case strings.HasPrefix(unit, "prefix:"):
		return strings.TrimSpace(strings.TrimPrefix(unit, "prefix:")) + number
	case strings.HasPrefix(unit, "suffix:"):
		return number + " " + strings.TrimSpace(strings.TrimPrefix(unit, "suffix:"))
	case strings.HasPrefix(unit, "count:"):
		return formatCountUnit(value, strings.TrimSpace(strings.TrimPrefix(unit, "count:")), decimals)
	case unit != "":
		return number + " " + unit
	default:
		return number
	}
}

func formatCountUnit(value float64, unit string, decimals *int) string {
	suffix := " " + unit
	switch absolute := math.Abs(value); {
	case absolute >= 1e12:
		return formatUnitNumber(value/1e12, "T"+suffix, decimals)
	case absolute >= 1e9:
		return formatUnitNumber(value/1e9, "B"+suffix, decimals)
	case absolute >= 1e6:
		return formatUnitNumber(value/1e6, "M"+suffix, decimals)
	case absolute >= 1e3:
		return formatUnitNumber(value/1e3, "K"+suffix, decimals)
	default:
		return formatRawNumber(value, decimals) + suffix
	}
}

// formatSI formats a number with SI suffixes (K, M, G, T).
func formatSI(value float64, decimals *int) string {
	absValue := math.Abs(value)

	switch {
	case absValue >= siTera:
		return formatUnitNumber(value/siTera, "T", decimals)
	case absValue >= siGiga:
		return formatUnitNumber(value/siGiga, "G", decimals)
	case absValue >= siMega:
		return formatUnitNumber(value/siMega, "M", decimals)
	case absValue >= siKilo:
		return formatUnitNumber(value/siKilo, "K", decimals)
	default:
		return formatRawNumber(value, decimals)
	}
}

// formatBinaryBytes formats bytes using binary (IEC) prefixes.
func formatBinaryBytes(value float64, decimals *int) string {
	absValue := math.Abs(value)

	switch {
	case absValue >= binaryTebi:
		return formatUnitNumber(value/binaryTebi, " TiB", decimals)
	case absValue >= binaryGibi:
		return formatUnitNumber(value/binaryGibi, " GiB", decimals)
	case absValue >= binaryMebi:
		return formatUnitNumber(value/binaryMebi, " MiB", decimals)
	case absValue >= binaryKibi:
		return formatUnitNumber(value/binaryKibi, " KiB", decimals)
	default:
		return formatUnitNumber(value, " B", decimals)
	}
}

// formatDecimalBytes formats bytes using decimal (SI) prefixes.
func formatDecimalBytes(value float64, decimals *int) string {
	absValue := math.Abs(value)

	switch {
	case absValue >= decTera:
		return formatUnitNumber(value/decTera, " TB", decimals)
	case absValue >= decGiga:
		return formatUnitNumber(value/decGiga, " GB", decimals)
	case absValue >= decMega:
		return formatUnitNumber(value/decMega, " MB", decimals)
	case absValue >= decKilo:
		return formatUnitNumber(value/decKilo, " KB", decimals)
	default:
		return formatUnitNumber(value, " B", decimals)
	}
}

// formatSeconds formats a duration in seconds as a human-readable
// string using hours, minutes, seconds, and milliseconds.
//
//nolint:cyclop // duration formatting handles several magnitudes and decimal precision
func formatSeconds(value float64, decimals *int) string {
	if value < 0 {
		return "-" + formatSeconds(-value, decimals)
	}

	if value == 0 {
		if decimals != nil {
			return formatUnitNumber(0, "s", decimals)
		}

		return "0s"
	}

	if value < 1 {
		ms := value * millisecondsPerSecond
		if decimals != nil {
			return formatUnitNumber(ms, "ms", decimals)
		}

		return fmt.Sprintf("%.0fms", ms)
	}

	totalSeconds := int(value)
	hours := totalSeconds / secondsPerHour
	minutes := (totalSeconds % secondsPerHour) / secondsPerMinute
	seconds := float64(totalSeconds % secondsPerMinute)
	if decimals != nil {
		seconds += value - math.Trunc(value)
	}

	result := ""
	if hours > 0 {
		result += fmt.Sprintf("%dh ", hours)
	}

	if minutes > 0 {
		result += fmt.Sprintf("%dm ", minutes)
	}

	if seconds > 0 {
		if decimals != nil {
			result += formatUnitNumber(seconds, "s", decimals)
		} else {
			result += fmt.Sprintf("%ds", int(seconds))
		}
	}

	if result == "" {
		if decimals != nil {
			return formatUnitNumber(0, "s", decimals)
		}

		return "0s"
	}

	// Trim trailing space from "Xh " or "Xm " when there is no
	// smaller component.
	if result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}

	return result
}

// formatMilliseconds formats a duration in milliseconds as a
// human-readable string.
func formatMilliseconds(value float64, decimals *int) string {
	if value < 0 {
		return "-" + formatMilliseconds(-value, decimals)
	}

	if value >= millisecondsPerSecond {
		return formatUnitNumber(value/millisecondsPerSecond, "s", decimals)
	}
	if decimals != nil {
		return formatUnitNumber(value, "ms", decimals)
	}

	return fmt.Sprintf("%.0fms", value)
}

// formatPercent formats a value as a percentage.
func formatPercent(value float64, decimals *int) string {
	return formatUnitNumber(value, "%", decimals)
}

// formatPercentUnit formats a unit-based percentage (0-1 range).
func formatPercentUnit(value float64, decimals *int) string {
	return formatUnitNumber(value*percentMultiplier, "%", decimals)
}

// formatReqPerSec formats a value as requests per second.
func formatReqPerSec(value float64, decimals *int) string {
	return formatSI(value, decimals) + " req/s"
}

// formatOpsPerSec formats a value as operations per second.
func formatOpsPerSec(value float64, decimals *int) string {
	return formatSI(value, decimals) + " ops/s"
}

// formatBytesPerSec formats a rate in bytes per second using decimal prefixes.
func formatBytesPerSec(value float64, decimals *int) string {
	return formatDecimalBytes(value, decimals) + "/s"
}

// formatBinaryBytesPerSec formats a rate in bytes per second using binary prefixes.
func formatBinaryBytesPerSec(value float64, decimals *int) string {
	return formatBinaryBytes(value, decimals) + "/s"
}

// formatDecimalBytesPerSec formats a rate in bytes per second using decimal prefixes.
func formatDecimalBytesPerSec(value float64, decimals *int) string {
	return formatDecimalBytes(value, decimals) + "/s"
}

// formatBitsPerSec formats a rate in bits per second.
func formatBitsPerSec(value float64, decimals *int) string {
	return formatBits(value, decimals) + "/s"
}

// formatBits formats a value in bits using SI prefixes.
func formatBits(value float64, decimals *int) string {
	absValue := math.Abs(value)

	switch {
	case absValue >= siTera:
		return formatUnitNumber(value/siTera, " Tb", decimals)
	case absValue >= siGiga:
		return formatUnitNumber(value/siGiga, " Gb", decimals)
	case absValue >= siMega:
		return formatUnitNumber(value/siMega, " Mb", decimals)
	case absValue >= siKilo:
		return formatUnitNumber(value/siKilo, " Kb", decimals)
	default:
		return formatUnitNumber(value, " b", decimals)
	}
}

// formatNanoseconds formats a duration in nanoseconds.
func formatNanoseconds(value float64, decimals *int) string {
	if value < 0 {
		return "-" + formatNanoseconds(-value, decimals)
	}

	nsPerUs := 1000.0
	nsPerMs := 1_000_000.0
	nsPerSec := 1_000_000_000.0

	switch {
	case value >= nsPerSec:
		return formatSeconds(value/nsPerSec, decimals)
	case value >= nsPerMs:
		if decimals != nil {
			return formatUnitNumber(value/nsPerMs, "ms", decimals)
		}

		return fmt.Sprintf("%.0fms", value/nsPerMs)
	case value >= nsPerUs:
		if decimals != nil {
			return formatUnitNumber(value/nsPerUs, "µs", decimals)
		}

		return fmt.Sprintf("%.0fµs", value/nsPerUs)
	default:
		if decimals != nil {
			return formatUnitNumber(value, "ns", decimals)
		}

		return fmt.Sprintf("%.0fns", value)
	}
}

// formatPacketsPerSec formats a value as packets per second.
func formatPacketsPerSec(value float64, decimals *int) string {
	return formatSI(value, decimals) + " pps"
}

// formatIOPS formats a value as I/O operations per second.
func formatIOPS(value float64, decimals *int) string {
	return formatSI(value, decimals) + " iops"
}

// formatHertz formats a value in hertz.
func formatHertz(value float64, decimals *int) string {
	absValue := math.Abs(value)

	switch {
	case absValue >= siGiga:
		return formatUnitNumber(value/siGiga, " GHz", decimals)
	case absValue >= siMega:
		return formatUnitNumber(value/siMega, " MHz", decimals)
	case absValue >= siKilo:
		return formatUnitNumber(value/siKilo, " kHz", decimals)
	default:
		return formatUnitNumber(value, " Hz", decimals)
	}
}

// formatWatt formats a value in watts.
func formatWatt(value float64, decimals *int) string {
	absValue := math.Abs(value)

	switch {
	case absValue >= siKilo:
		return formatUnitNumber(value/siKilo, " kW", decimals)
	default:
		return formatUnitNumber(value, " W", decimals)
	}
}

// formatKilowatt formats a value in kilowatts.
func formatKilowatt(value float64, decimals *int) string {
	return formatUnitNumber(value, " kW", decimals)
}

// formatVoltAmp formats a value in volt-amperes.
func formatVoltAmp(value float64, decimals *int) string {
	return formatUnitNumber(value, " VA", decimals)
}

// formatRawNumber formats a number with appropriate decimal places.
func formatRawNumber(value float64, decimals *int) string {
	if decimals != nil {
		return strconv.FormatFloat(value, 'f', *decimals, 64)
	}

	if value == math.Trunc(value) && math.Abs(value) < 1e15 {
		return fmt.Sprintf("%.0f", value)
	}

	return fmt.Sprintf("%.2f", value)
}

func formatUnitNumber(value float64, suffix string, decimals *int) string {
	if decimals != nil {
		return strconv.FormatFloat(value, 'f', *decimals, 64) + suffix
	}

	return trimTrailingZeros(value, suffix)
}

// trimTrailingZeros formats a number to one decimal place and
// appends a suffix.
func trimTrailingZeros(value float64, suffix string) string {
	return fmt.Sprintf("%.1f%s", value, suffix)
}

// fieldConfigDefaults is the parsed structure for extracting the
// unit, thresholds, and min/max from a panel's FieldConfig JSON.
type fieldConfigDefaults struct {
	Defaults struct {
		Unit       string           `json:"unit"`
		Min        *float64         `json:"min,omitempty"`
		Max        *float64         `json:"max,omitempty"`
		Thresholds *thresholdConfig `json:"thresholds,omitempty"`
	} `json:"defaults"`
}

// thresholdConfig represents Grafana's threshold configuration.
type thresholdConfig struct {
	Mode  string          `json:"mode"` // "absolute" or "percentage"
	Steps []thresholdStep `json:"steps"`
}

// thresholdStep represents a single threshold step.
type thresholdStep struct {
	Color string   `json:"color"`
	Value *float64 `json:"value"` // nil for the base step
}

// GaugeConfig holds the parsed gauge-relevant settings from a
// panel's fieldConfig.
type GaugeConfig struct {
	Min           float64
	Max           float64
	MinSet        bool // true if min was explicitly configured
	MaxSet        bool // true if max was explicitly configured
	Thresholds    []thresholdStep
	ThresholdMode string // "absolute" or "percentage"
}

// ExtractGaugeConfig reads min, max, and thresholds from a panel's
// FieldConfig JSON.
func ExtractGaugeConfig(fieldConfig json.RawMessage) GaugeConfig {
	cfg := GaugeConfig{
		Min:           0,
		Max:           100, //nolint:mnd // Grafana default
		MinSet:        false,
		MaxSet:        false,
		Thresholds:    nil,
		ThresholdMode: "absolute",
	}

	if len(fieldConfig) == 0 {
		return cfg
	}

	var parsed fieldConfigDefaults
	if err := json.Unmarshal(fieldConfig, &parsed); err != nil {
		return cfg
	}

	if parsed.Defaults.Min != nil {
		cfg.Min = *parsed.Defaults.Min
		cfg.MinSet = true
	}

	if parsed.Defaults.Max != nil {
		cfg.Max = *parsed.Defaults.Max
		cfg.MaxSet = true
	}

	if parsed.Defaults.Thresholds != nil {
		cfg.Thresholds = parsed.Defaults.Thresholds.Steps

		if parsed.Defaults.Thresholds.Mode != "" {
			cfg.ThresholdMode = parsed.Defaults.Thresholds.Mode
		}
	}

	return cfg
}

// ThresholdColor returns the hex color for a value based on the
// threshold steps. Steps are evaluated in reverse order (highest
// first). In percentage mode, thresholds are relative to the min/max
// range. Grafana named colors are resolved to hex.
func (g *GaugeConfig) ThresholdColor(value float64) string {
	if len(g.Thresholds) == 0 {
		return grafanaGreen // default green
	}

	// In percentage mode, convert value to a percentage of the range.
	compareValue := value
	if g.ThresholdMode == "percentage" {
		rangeVal := g.Max - g.Min
		if rangeVal > 0 {
			compareValue = (value - g.Min) / rangeVal * 100 //nolint:mnd // percentage
		}
	}

	// Walk steps in reverse to find the highest matching threshold.
	for i := range slices.Backward(g.Thresholds) {
		step := g.Thresholds[i]
		if step.Value == nil {
			return resolveGrafanaColor(step.Color)
		}

		if compareValue >= *step.Value {
			return resolveGrafanaColor(step.Color)
		}
	}

	// Fallback to base step color if present.
	if g.Thresholds[0].Value == nil {
		return resolveGrafanaColor(g.Thresholds[0].Color)
	}

	return "#00FF00"
}

// grafanaColors maps Grafana's named color tokens to hex values.
var grafanaColors = map[string]string{ //nolint:gochecknoglobals // read-only lookup
	"green":             grafanaGreen,
	"dark-green":        "#1A7311",
	"semi-dark-green":   "#37872D",
	"light-green":       "#96D98D",
	"super-light-green": "#C8F2C2",

	"red":             grafanaRed,
	"dark-red":        "#AD0317",
	"semi-dark-red":   "#E02F44",
	"light-red":       "#FF7383",
	"super-light-red": "#FFA6B0",

	"blue":             grafanaBlue,
	"dark-blue":        "#1F60C4",
	"semi-dark-blue":   "#3274D9",
	"light-blue":       "#8AB8FF",
	"super-light-blue": "#C0D8FF",

	"yellow":             grafanaYellow,
	"dark-yellow":        "#CC9D00",
	"semi-dark-yellow":   "#E0B400",
	"light-yellow":       "#FFEE52",
	"super-light-yellow": "#FFF899",

	"orange":             grafanaOrange,
	"dark-orange":        "#C4641A",
	"semi-dark-orange":   "#E0752D",
	"light-orange":       "#FFB357",
	"super-light-orange": "#FFCB7D",

	"purple":             "#B877D9",
	"dark-purple":        "#7C2EA3",
	"semi-dark-purple":   "#8F3BB8",
	"light-purple":       "#CA95E5",
	"super-light-purple": "#DEB6F2",

	"white": colorWhite,
	"gray":  "#808080",
}

// resolveGrafanaColor converts a Grafana color (named or hex) to a
// hex string usable by lipgloss.
func resolveGrafanaColor(color string) string {
	if len(color) > 0 && color[0] == '#' {
		return color
	}

	if hex, ok := grafanaColors[color]; ok {
		return hex
	}

	return color
}

// gaugeDisplayModes lists the table cell display modes that should
// render as gauges rather than plain text.
var gaugeDisplayModes = map[string]bool{ //nolint:gochecknoglobals // read-only set
	"basic":          true,
	"gradient-gauge": true,
	"lcd-gauge":      true,
}

// isGaugeDisplayMode checks if the panel's field config has a
// gauge-like display mode set.
func isGaugeDisplayMode(fieldConfig json.RawMessage) bool {
	if len(fieldConfig) == 0 {
		return false
	}

	var config struct {
		Defaults struct {
			Custom struct {
				DisplayMode string `json:"displayMode"`
			} `json:"custom"`
		} `json:"defaults"`
	}

	if err := json.Unmarshal(fieldConfig, &config); err != nil {
		return false
	}

	return gaugeDisplayModes[config.Defaults.Custom.DisplayMode]
}

// ExtractUnit reads the unit string from a panel's FieldConfig JSON.
// It parses the path: {"defaults": {"unit": "bytes"}}.
func ExtractUnit(fieldConfig json.RawMessage) string {
	if len(fieldConfig) == 0 {
		return ""
	}

	var config fieldConfigDefaults
	if err := json.Unmarshal(fieldConfig, &config); err != nil {
		return ""
	}

	return config.Defaults.Unit
}
