package grafana

import "testing"

func TestGroupingMatrixEmptyValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setting string
		want    any
	}{
		{name: "default", setting: "", want: ""},
		{name: "null", setting: matrixEmptyNull, want: nil},
		{name: "true", setting: matrixEmptyTrue, want: true},
		{name: "false", setting: matrixEmptyFalse, want: false},
		{name: "zero", setting: matrixEmptyZero, want: float64(0)},
		{name: "empty", setting: "empty", want: ""},
		{name: "unknown", setting: "other", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			matrix := newMatrixBuilder(groupingToMatrixOptions{
				RowField: "", ColumnField: "", ValueField: "", EmptyValue: test.setting,
			})
			if got := matrix.emptyValue(); got != test.want {
				t.Errorf("emptyValue() = %#v, want %#v", got, test.want)
			}
		})
	}
}
