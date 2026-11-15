package grafanatui

import (
	"sort"

	"github.com/lovromazgon/grafana-tui/grafana"
)

const (
	grafanaGridColumns = 24
	minimumPanelHeight = 4
	minimumRepeatWidth = 24
)

type gridRect struct {
	X, Y, W, H    int
	InstanceIndex int
}

type indexedPanelInstance struct {
	instance grafana.PanelInstance
	index    int
}

func layoutPanelInstances(instances []grafana.PanelInstance, terminalWidth int) []gridRect {
	width := max(terminalWidth, 1)
	ordered := make([]indexedPanelInstance, len(instances))
	for index, instance := range instances {
		ordered[index] = indexedPanelInstance{instance: instance, index: index}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a := ordered[i].instance.Panel.GridPos
		b := ordered[j].instance.Panel.GridPos
		if a.Y == b.Y {
			return a.X < b.X
		}

		return a.Y < b.Y
	})

	rects := make([]gridRect, 0, len(instances))
	terminalY := 0
	for start := 0; start < len(ordered); {
		end := start + 1
		for end < len(ordered) &&
			ordered[end].instance.Panel.GridPos.Y == ordered[start].instance.Panel.GridPos.Y {
			end++
		}

		row := ordered[start:end]
		for len(row) > 0 {
			group := make([]indexedPanelInstance, 0, len(row))
			remaining := make([]indexedPanelInstance, 0, len(row))
			source := row[0].instance
			for _, item := range row {
				if sameLayoutGroup(source, item.instance) {
					group = append(group, item)
				} else {
					remaining = append(remaining, item)
				}
			}

			if isHorizontalRepeat(group[0].instance) {
				rects, terminalY = layoutHorizontalRepeatRow(rects, group, terminalY, width)
			} else {
				rects, terminalY = layoutGridRow(rects, group, terminalY, width)
			}
			row = remaining
		}
		start = end
	}

	return rects
}

func layoutGridRow(
	rects []gridRect,
	row []indexedPanelInstance,
	terminalY int,
	terminalWidth int,
) ([]gridRect, int) {
	rowHeight := 0
	for _, item := range row {
		position := item.instance.Panel.GridPos
		x := position.X * terminalWidth / grafanaGridColumns
		x = min(max(x, 0), terminalWidth-1)
		w := position.W * terminalWidth / grafanaGridColumns
		w = min(max(w, 1), terminalWidth-x)
		h := terminalPanelHeight(position.H)
		rects = append(rects, gridRect{
			X: x, Y: terminalY, W: w, H: h, InstanceIndex: item.index,
		})
		rowHeight = max(rowHeight, h)
	}

	return rects, terminalY + rowHeight
}

func layoutHorizontalRepeatRow(
	rects []gridRect,
	row []indexedPanelInstance,
	terminalY int,
	terminalWidth int,
) ([]gridRect, int) {
	repeatLength := max(row[0].instance.RepeatLength, 1)
	columns := min(repeatLength, max(terminalWidth/minimumRepeatWidth, 1))
	rowHeight := 0
	for _, item := range row {
		rowHeight = max(rowHeight, terminalPanelHeight(item.instance.Panel.GridPos.H))
	}

	for _, item := range row {
		repeatIndex := item.instance.RepeatIndex
		column := repeatIndex % columns
		repeatRow := repeatIndex / columns
		x := column * terminalWidth / columns
		right := (column + 1) * terminalWidth / columns
		rects = append(rects, gridRect{
			X:             x,
			Y:             terminalY + repeatRow*rowHeight,
			W:             right - x,
			H:             terminalPanelHeight(item.instance.Panel.GridPos.H),
			InstanceIndex: item.index,
		})
	}

	rows := (len(row) + columns - 1) / columns
	return rects, terminalY + rows*rowHeight
}

func sameLayoutGroup(a, b grafana.PanelInstance) bool {
	aRepeated := isHorizontalRepeat(a)
	bRepeated := isHorizontalRepeat(b)
	if !aRepeated || !bRepeated {
		return aRepeated == bRepeated
	}

	return a.Panel.ID == b.Panel.ID && a.Panel.Repeat == b.Panel.Repeat
}

func isHorizontalRepeat(instance grafana.PanelInstance) bool {
	return instance.Panel.Repeat != "" && instance.Panel.RepeatDirection != "v"
}

func terminalPanelHeight(gridHeight int) int {
	return max(gridHeight*3/2, minimumPanelHeight)
}

func totalGridHeight(rects []gridRect) int {
	height := 0
	for _, rect := range rects {
		height = max(height, rect.Y+rect.H)
	}

	return height
}
