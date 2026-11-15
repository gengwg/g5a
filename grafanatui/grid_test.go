//nolint:exhaustruct,testpackage // internal layout API and partial fixtures need direct tests
package grafanatui

import (
	"reflect"
	"testing"

	"github.com/lovromazgon/grafana-tui/grafana"
)

func TestLayoutPanelInstancesOrdinaryPlacement(t *testing.T) {
	t.Parallel()

	instances := []grafana.PanelInstance{
		{Panel: grafana.Panel{GridPos: grafana.GridPos{X: 0, Y: 8, W: 24, H: 4}}},
		{Panel: grafana.Panel{GridPos: grafana.GridPos{X: 12, Y: 0, W: 12, H: 4}}},
		{Panel: grafana.Panel{GridPos: grafana.GridPos{X: 0, Y: 0, W: 12, H: 8}}},
	}
	want := []gridRect{
		{X: 0, Y: 0, W: 60, H: 12, InstanceIndex: 2},
		{X: 60, Y: 0, W: 60, H: 6, InstanceIndex: 1},
		{X: 0, Y: 12, W: 120, H: 6, InstanceIndex: 0},
	}

	assertGridRects(t, layoutPanelInstances(instances, 120), want)
}

func TestLayoutPanelInstancesHorizontalRepeatWithOrdinaryPanel(t *testing.T) {
	t.Parallel()

	instances := []grafana.PanelInstance{
		{
			Panel: grafana.Panel{
				ID: 1, Repeat: "cluster", RepeatDirection: "h",
				GridPos: grafana.GridPos{X: 0, Y: 0, W: 12, H: 4},
			},
			RepeatIndex: 0, RepeatLength: 2,
		},
		{
			Panel: grafana.Panel{
				ID: 1, Repeat: "cluster", RepeatDirection: "h",
				GridPos: grafana.GridPos{X: 0, Y: 0, W: 12, H: 4},
			},
			RepeatIndex: 1, RepeatLength: 2,
		},
		{Panel: grafana.Panel{ID: 2, GridPos: grafana.GridPos{X: 12, Y: 0, W: 12, H: 4}}},
	}
	want := []gridRect{
		{X: 0, Y: 0, W: 60, H: 6, InstanceIndex: 0},
		{X: 60, Y: 0, W: 60, H: 6, InstanceIndex: 1},
		{X: 60, Y: 6, W: 60, H: 6, InstanceIndex: 2},
	}

	got := layoutPanelInstances(instances, 120)
	assertNoGridRectOverlaps(t, got)
	assertGridRects(t, got, want)
}

func TestLayoutPanelInstancesSeparatesHorizontalRepeatGroups(t *testing.T) {
	t.Parallel()

	instances := []grafana.PanelInstance{
		{
			Panel: grafana.Panel{
				ID: 1, Repeat: "cluster", RepeatDirection: "h",
				GridPos: grafana.GridPos{X: 0, Y: 0, W: 12, H: 4},
			},
			RepeatLength: 1,
		},
		{
			Panel: grafana.Panel{
				ID: 2, Repeat: "host", RepeatDirection: "h",
				GridPos: grafana.GridPos{X: 12, Y: 0, W: 12, H: 4},
			},
			RepeatLength: 1,
		},
	}
	want := []gridRect{
		{X: 0, Y: 0, W: 120, H: 6, InstanceIndex: 0},
		{X: 0, Y: 6, W: 120, H: 6, InstanceIndex: 1},
	}

	got := layoutPanelInstances(instances, 120)
	assertNoGridRectOverlaps(t, got)
	assertGridRects(t, got, want)
}

func TestLayoutPanelInstancesHorizontalRepeatPartialLastRow(t *testing.T) {
	t.Parallel()

	want := []gridRect{
		{X: 0, Y: 0, W: 40, H: 6, InstanceIndex: 0},
		{X: 40, Y: 0, W: 40, H: 6, InstanceIndex: 1},
		{X: 80, Y: 0, W: 40, H: 6, InstanceIndex: 2},
		{X: 0, Y: 6, W: 40, H: 6, InstanceIndex: 3},
		{X: 40, Y: 6, W: 40, H: 6, InstanceIndex: 4},
		{X: 0, Y: 12, W: 120, H: 6, InstanceIndex: 5},
	}

	assertGridRects(t, layoutPanelInstances(fiveHorizontalRepeats(), 120), want)
}

func TestLayoutPanelInstancesHorizontalRepeatNarrowTerminal(t *testing.T) {
	t.Parallel()

	want := []gridRect{
		{X: 0, Y: 0, W: 30, H: 6, InstanceIndex: 0},
		{X: 30, Y: 0, W: 30, H: 6, InstanceIndex: 1},
		{X: 0, Y: 6, W: 30, H: 6, InstanceIndex: 2},
		{X: 0, Y: 12, W: 30, H: 6, InstanceIndex: 3},
		{X: 30, Y: 12, W: 30, H: 6, InstanceIndex: 4},
		{X: 0, Y: 18, W: 60, H: 6, InstanceIndex: 5},
	}

	assertGridRects(t, layoutPanelInstances(fiveHorizontalRepeats(), 60), want)
}

func TestLayoutPanelInstancesVerticalRepeat(t *testing.T) {
	t.Parallel()

	instances := []grafana.PanelInstance{
		{Panel: grafana.Panel{GridPos: grafana.GridPos{X: 0, Y: 8, W: 24, H: 4}}},
		{
			Panel: grafana.Panel{
				Repeat: "host", RepeatDirection: "v",
				GridPos: grafana.GridPos{X: 6, Y: 4, W: 12, H: 4},
			},
			RepeatLength: 1,
		},
		{
			Panel: grafana.Panel{
				Repeat: "host", RepeatDirection: "v",
				GridPos: grafana.GridPos{X: 6, Y: 0, W: 12, H: 4},
			},
			RepeatLength: 1,
		},
	}
	want := []gridRect{
		{X: 30, Y: 0, W: 60, H: 6, InstanceIndex: 2},
		{X: 30, Y: 6, W: 60, H: 6, InstanceIndex: 1},
		{X: 0, Y: 12, W: 120, H: 6, InstanceIndex: 0},
	}

	assertGridRects(t, layoutPanelInstances(instances, 120), want)
}

func TestLayoutPanelInstancesTinyWidths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		width    int
		position grafana.GridPos
		want     gridRect
	}{
		{
			name: "one cell", width: 1,
			position: grafana.GridPos{X: 23, Y: 2, W: 1, H: 1},
			want:     gridRect{X: 0, Y: 0, W: 1, H: 4},
		},
		{
			name: "two cells", width: 2,
			position: grafana.GridPos{X: 23, Y: 2, W: 1, H: 1},
			want:     gridRect{X: 1, Y: 0, W: 1, H: 4},
		},
		{
			name: "fractional cell", width: 5,
			position: grafana.GridPos{X: 23, Y: 2, W: 1, H: 1},
			want:     gridRect{X: 4, Y: 0, W: 1, H: 4},
		},
		{
			name: "clamp right edge", width: 10,
			position: grafana.GridPos{X: 22, W: 8, H: 2},
			want:     gridRect{X: 9, Y: 0, W: 1, H: 4},
		},
		{
			name: "full width", width: 3,
			position: grafana.GridPos{X: 0, W: 24, H: 3},
			want:     gridRect{X: 0, Y: 0, W: 3, H: 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instances := []grafana.PanelInstance{{Panel: grafana.Panel{GridPos: tt.position}}}
			assertGridRects(t, layoutPanelInstances(instances, tt.width), []gridRect{tt.want})
		})
	}
}

func TestTotalGridHeight(t *testing.T) {
	t.Parallel()

	if got := totalGridHeight(nil); got != 0 {
		t.Fatalf("totalGridHeight(nil) = %d, want 0", got)
	}
	rects := []gridRect{{Y: 4, H: 6}, {Y: 0, H: 4}, {Y: 12, H: 3}}
	if got := totalGridHeight(rects); got != 15 {
		t.Errorf("totalGridHeight() = %d, want 15", got)
	}
}

func fiveHorizontalRepeats() []grafana.PanelInstance {
	instances := make([]grafana.PanelInstance, 0, 6)
	for index := range 5 {
		instances = append(instances, grafana.PanelInstance{
			Panel: grafana.Panel{
				ID: 1, Repeat: "cluster", RepeatDirection: "h",
				GridPos: grafana.GridPos{X: 0, Y: (index / 3) * 4, W: 8, H: 4},
			},
			RepeatIndex:  index % 3,
			RepeatLength: 3,
		})
	}

	return append(instances, grafana.PanelInstance{
		Panel: grafana.Panel{
			ID: 2, GridPos: grafana.GridPos{X: 0, Y: 8, W: 24, H: 4},
		},
	})
}

func assertNoGridRectOverlaps(t *testing.T, rects []gridRect) {
	t.Helper()
	for index, a := range rects {
		for offset, b := range rects[index+1:] {
			overlaps := a.X < b.X+b.W && b.X < a.X+a.W &&
				a.Y < b.Y+b.H && b.Y < a.Y+a.H
			if overlaps {
				t.Errorf("rectangles %d and %d overlap: %+v and %+v", index, index+offset+1, a, b)
			}
		}
	}
}

func assertGridRects(t *testing.T, got []gridRect, want []gridRect) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("layoutPanelInstances() =\n%+v\nwant:\n%+v", got, want)
	}
}
