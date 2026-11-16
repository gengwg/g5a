package grafana

import (
	"encoding/json"
	"testing"
)

func TestEvaluateValueFilterConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   any
		id      string
		options string
		want    bool
	}{
		{name: "is null", value: nil, id: valueConditionIsNull, options: `{}`, want: true},
		{name: "zero is not null", value: float64(0), id: valueConditionIsNull, options: `{}`, want: false},
		{name: "empty string is not null", value: "", id: valueConditionIsNotNull, options: `{}`, want: true},
		{name: "regex string", value: "alpha", id: valueConditionRegex, options: `{"value":"^a"}`, want: true},
		{name: "regex number", value: float64(12), id: valueConditionRegex, options: `{"value":"^12$"}`, want: true},
		{name: "regex empty", value: "", id: valueConditionRegex, options: `{"value":".*"}`, want: true},
		{name: "regex null", value: nil, id: valueConditionRegex, options: `{"value":".*"}`, want: false},
		{
			name: "substring case insensitive", value: "Warning", id: valueConditionSubstring,
			options: `{"value":"warn"}`, want: true,
		},
		{name: "substring empty option", value: nil, id: valueConditionSubstring, options: `{"value":""}`, want: true},
		{name: "not substring", value: "healthy", id: valueConditionNotSubstring, options: `{"value":"warn"}`, want: true},
		{name: "not substring null", value: nil, id: valueConditionNotSubstring, options: `{"value":"warn"}`, want: false},
		{
			name: "not substring empty cell", value: "", id: valueConditionNotSubstring,
			options: `{"value":"warn"}`, want: false,
		},
		{
			name: "not substring empty option", value: "healthy", id: valueConditionNotSubstring,
			options: `{"value":""}`, want: false,
		},
		{name: "string equal", value: "prod", id: valueConditionEqual, options: `{"value":"prod"}`, want: true},
		{name: "numeric string equal", value: "01", id: valueConditionEqual, options: `{"value":1}`, want: true},
		{name: "string not equal", value: "dev", id: valueConditionNotEqual, options: `{"value":"prod"}`, want: true},
		{
			name: "between exclusive", value: float64(1000), id: valueConditionBetween,
			options: `{"from":1000,"to":2000}`, want: false,
		},
		{
			name: "between inclusive", value: float64(1000), id: valueConditionBetween,
			options: `{"from":"1000","to":"2000","inclusive":true}`, want: true,
		},
		{
			name: "between null", value: nil, id: valueConditionBetween,
			options: `{"from":0,"to":2000}`, want: false,
		},
		{
			name: "between invalid bound", value: float64(1000), id: valueConditionBetween,
			options: `{"from":"bad","to":2000}`, want: false,
		},
		{name: "numeric comparison", value: float64(3), id: valueConditionGreater, options: `{"value":"2"}`, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valueFilterConfig{ID: test.id, Options: json.RawMessage(test.options)}
			if got := evaluateCondition(test.value, config); got != test.want {
				t.Errorf("evaluateCondition(%#v) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
