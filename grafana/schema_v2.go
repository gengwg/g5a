package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const (
	dashboardV2GridColumns = 24
	dashboardLayoutGrid    = "GridLayout"
)

var (
	errUnsupportedV2Layout  = errors.New("unsupported dashboard v2 layout")
	errV2ElementNotFound    = errors.New("dashboard element not found")
	errUnsupportedV2Element = errors.New("unsupported dashboard element kind")
	errInvalidV2Query       = errors.New("invalid dashboard v2 query")
	errNestedV2RowRepeat    = errors.New("nested dashboard v2 row repeats are not supported")
)

type dashboardV2Resource struct {
	Spec dashboardV2Spec `json:"spec"`
}

type dashboardV2Spec struct {
	Title        string                        `json:"title"`
	TimeSettings dashboardV2TimeSettings       `json:"timeSettings"`
	Elements     map[string]dashboardV2Element `json:"elements"`
	Layout       dashboardV2Layout             `json:"layout"`
	Variables    []dashboardV2Variable         `json:"variables"`
}

type dashboardV2TimeSettings struct {
	From        string          `json:"from"`
	To          string          `json:"to"`
	AutoRefresh RefreshInterval `json:"autoRefresh"`
}

type dashboardV2Element struct {
	Kind         string `json:"kind"`
	ClassicPanel *Panel `json:"-"`
	Spec         struct {
		ID           int    `json:"id"`
		Title        string `json:"title"`
		LibraryPanel struct {
			UID  string `json:"uid"`
			Name string `json:"name"`
		} `json:"libraryPanel"`
		VizConfig struct {
			Group   string `json:"group"`
			Version string `json:"version"`
			Spec    struct {
				FieldConfig json.RawMessage `json:"fieldConfig"`
				Options     json.RawMessage `json:"options"`
			} `json:"spec"`
		} `json:"vizConfig"`
		Data struct {
			Spec struct {
				Queries         []dashboardV2Query `json:"queries"`
				Transformations []struct {
					Group string `json:"group"`
					Spec  struct {
						Disabled bool            `json:"disabled"`
						Options  json.RawMessage `json:"options"`
					} `json:"spec"`
				} `json:"transformations"`
			} `json:"spec"`
		} `json:"data"`
	} `json:"spec"`
}

type dashboardV2Query struct {
	Spec struct {
		RefID  string `json:"refId"`
		Hidden bool   `json:"hidden"`
		Query  struct {
			Group      string `json:"group"`
			Datasource *struct {
				Name string `json:"name"`
			} `json:"datasource"`
			Spec json.RawMessage `json:"spec"`
		} `json:"query"`
	} `json:"spec"`
}

type dashboardV2Layout struct {
	Kind string                `json:"kind"`
	Spec dashboardV2LayoutSpec `json:"spec"`
}

type dashboardV2LayoutSpec struct {
	Items          []dashboardV2LayoutItem `json:"items"`
	Rows           []dashboardV2LayoutRow  `json:"rows"`
	Tabs           []dashboardV2LayoutTab  `json:"tabs"`
	MaxColumnCount int                     `json:"maxColumnCount"`
	RowHeight      int                     `json:"rowHeight"`
	RowHeightMode  string                  `json:"rowHeightMode"`
}

type dashboardV2LayoutItem struct {
	Kind string `json:"kind"`
	Spec struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
		Repeat *struct {
			Value     string `json:"value"`
			Direction string `json:"direction"`
			MaxPerRow int    `json:"maxPerRow"`
		} `json:"repeat"`
		Element struct {
			Name string `json:"name"`
		} `json:"element"`
	} `json:"spec"`
}

type dashboardV2LayoutRow struct {
	Kind string `json:"kind"`
	Spec struct {
		Repeat *struct {
			Value string `json:"value"`
		} `json:"repeat"`
		Layout dashboardV2Layout `json:"layout"`
	} `json:"spec"`
}

type dashboardV2LayoutTab struct {
	Kind string `json:"kind"`
	Spec struct {
		Title  string `json:"title"`
		Repeat *struct {
			Value string `json:"value"`
		} `json:"repeat"`
		Layout dashboardV2Layout `json:"layout"`
	} `json:"spec"`
}

type dashboardV2Variable struct {
	Kind string          `json:"kind"`
	Spec json.RawMessage `json:"spec"`
}

func (c *Client) getDashboardV2(ctx context.Context, uid string) (*Dashboard, error) {
	versions := []string{"v2", "v2beta1", "v1beta1", "v1"}
	var lastErr error
	for _, version := range versions {
		path := "/apis/dashboard.grafana.app/" + version +
			"/namespaces/default/dashboards/" + url.PathEscape(uid)
		var raw json.RawMessage
		if err := c.doJSON(ctx, path, &raw); err != nil {
			lastErr = err
			if !dashboardFallbackStatus(err) {
				return nil, fmt.Errorf("getting dashboard %s: %w", version, err)
			}
			continue
		}
		dashboard, err := c.dashboardFromResource(ctx, raw)
		if err == nil {
			return dashboard, nil
		}
		return nil, err
	}

	return nil, fmt.Errorf("getting dashboard through dashboard.grafana.app: %w", lastErr)
}

func dashboardFallbackStatus(err error) bool {
	var statusErr *apiError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.StatusCode == http.StatusBadRequest ||
		statusErr.StatusCode == http.StatusNotFound ||
		statusErr.StatusCode == http.StatusMethodNotAllowed
}

func (c *Client) dashboardFromResource(ctx context.Context, raw json.RawMessage) (*Dashboard, error) {
	var resource dashboardV2Resource
	if err := json.Unmarshal(raw, &resource); err != nil {
		return nil, fmt.Errorf("decoding dashboard resource: %w", err)
	}
	if len(resource.Spec.Elements) > 0 || resource.Spec.Layout.Kind != "" {
		if err := c.resolveLibraryPanels(ctx, &resource); err != nil {
			return nil, err
		}
		return resource.Spec.classicDashboard()
	}

	var classic dashboardResponse
	if err := json.Unmarshal(raw, &classic); err != nil {
		return nil, fmt.Errorf("decoding classic dashboard resource: %w", err)
	}
	if classic.Dashboard.Title != "" || classic.Dashboard.Panels != nil {
		return &classic.Dashboard, nil
	}
	var direct struct {
		Spec Dashboard `json:"spec"`
	}
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, fmt.Errorf("decoding direct dashboard resource: %w", err)
	}

	return &direct.Spec, nil
}

func (c *Client) resolveLibraryPanels(ctx context.Context, resource *dashboardV2Resource) error {
	for name, element := range resource.Spec.Elements {
		if element.Kind != "LibraryPanel" {
			continue
		}
		uid := element.Spec.LibraryPanel.UID
		panel, err := c.getLibraryPanel(ctx, uid)
		if err != nil {
			return fmt.Errorf("resolving library panel %q: %w", uid, err)
		}
		element.ClassicPanel = panel
		resource.Spec.Elements[name] = element
	}

	return nil
}

func (c *Client) getLibraryPanel(ctx context.Context, uid string) (*Panel, error) {
	var response struct {
		Result struct {
			Model Panel `json:"model"`
		} `json:"result"`
	}
	if err := c.doJSON(ctx, "/api/library-elements/"+url.PathEscape(uid), &response); err != nil {
		return nil, fmt.Errorf("getting library panel %q: %w", uid, err)
	}

	return &response.Result.Model, nil
}

func (spec dashboardV2Spec) classicDashboard() (*Dashboard, error) {
	panels, err := spec.panels()
	if err != nil {
		return nil, err
	}
	variables, err := spec.classicVariables()
	if err != nil {
		return nil, err
	}

	return &Dashboard{
		Title:      spec.Title,
		Panels:     panels,
		Templating: Templating{List: variables},
		Time:       DashboardTime{From: spec.TimeSettings.From, To: spec.TimeSettings.To},
		Refresh:    spec.TimeSettings.AutoRefresh,
	}, nil
}

type v2IDAllocator struct{ next int }

func newV2IDAllocator(elements map[string]dashboardV2Element) *v2IDAllocator {
	maxID := len(elements)
	for _, element := range elements {
		maxID = max(maxID, element.Spec.ID)
	}

	return &v2IDAllocator{next: maxID + 1}
}

func (a *v2IDAllocator) nextID() int {
	id := a.next
	a.next++

	return id
}

func (spec dashboardV2Spec) panels() ([]Panel, error) {
	return spec.Layout.panelsWithIDs(spec.Elements, newV2IDAllocator(spec.Elements))
}

func (layout dashboardV2Layout) panelsWithIDs(
	elements map[string]dashboardV2Element,
	ids *v2IDAllocator,
) ([]Panel, error) {
	switch layout.Kind {
	case dashboardLayoutGrid:
		return layout.gridPanels(elements, 0, ids)
	case "RowsLayout":
		return layout.rowsPanels(elements, ids)
	case "AutoGridLayout":
		return layout.autoGridPanels(elements, ids)
	case "TabsLayout":
		return layout.tabsPanels(elements, ids)
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedV2Layout, layout.Kind)
	}
}

func (layout dashboardV2Layout) gridPanels(
	elements map[string]dashboardV2Element,
	yOffset int,
	ids *v2IDAllocator,
) ([]Panel, error) {
	panels := make([]Panel, 0, len(layout.Spec.Items))
	for _, item := range layout.Spec.Items {
		elementName := item.Spec.Element.Name
		element, ok := elements[elementName]
		if !ok {
			return nil, fmt.Errorf("%w: %s", errV2ElementNotFound, elementName)
		}
		if element.ClassicPanel != nil {
			panels = append(panels, classicLibraryPanel(element, item, yOffset, ids))
			continue
		}
		if element.Kind != "Panel" {
			return nil, fmt.Errorf("%w: %s", errUnsupportedV2Element, element.Kind)
		}
		panel := Panel{
			ID:    element.Spec.ID,
			Type:  element.Spec.VizConfig.Group,
			Title: element.Spec.Title,
			GridPos: GridPos{
				H: item.Spec.Height,
				W: item.Spec.Width,
				X: item.Spec.X,
				Y: item.Spec.Y + yOffset,
			},
			FieldConfig: element.Spec.VizConfig.Spec.FieldConfig,
			Options:     element.Spec.VizConfig.Spec.Options,
			Targets:     nil, Datasource: nil, Panels: nil, Transformations: nil,
			Repeat: "", RepeatDirection: "", MaxPerRow: 0, Collapsed: false, row: nil,
		}
		if panel.ID == 0 {
			panel.ID = ids.nextID()
		}
		if element.Spec.ID != 0 {
			panel.ID = element.Spec.ID
		}
		if item.Spec.Repeat != nil {
			panel.Repeat = item.Spec.Repeat.Value
			panel.RepeatDirection = item.Spec.Repeat.Direction
			panel.MaxPerRow = item.Spec.Repeat.MaxPerRow
		}
		if err := setV2PanelData(&panel, element); err != nil {
			return nil, err
		}
		panels = append(panels, panel)
	}

	return panels, nil
}

func classicLibraryPanel(
	element dashboardV2Element,
	item dashboardV2LayoutItem,
	yOffset int,
	ids *v2IDAllocator,
) Panel {
	panel := *element.ClassicPanel
	panel.ID = element.Spec.ID
	if panel.ID == 0 {
		panel.ID = ids.nextID()
	}
	panel.Repeat = ""
	panel.RepeatDirection = ""
	panel.MaxPerRow = 0
	if element.Spec.Title != "" {
		panel.Title = element.Spec.Title
	}
	panel.GridPos = GridPos{
		H: item.Spec.Height,
		W: item.Spec.Width,
		X: item.Spec.X,
		Y: item.Spec.Y + yOffset,
	}
	if item.Spec.Repeat != nil {
		panel.Repeat = item.Spec.Repeat.Value
		panel.RepeatDirection = item.Spec.Repeat.Direction
		panel.MaxPerRow = item.Spec.Repeat.MaxPerRow
	}

	return panel
}

func (layout dashboardV2Layout) rowsPanels(
	elements map[string]dashboardV2Element,
	ids *v2IDAllocator,
) ([]Panel, error) {
	panels := make([]Panel, 0)
	yOffset := 0
	for _, row := range layout.Spec.Rows {
		if row.Kind != "RowsLayoutRow" {
			return nil, fmt.Errorf("%w: %s", errUnsupportedV2Layout, row.Kind)
		}
		rowPanels, err := row.Spec.Layout.panelsAtOffset(elements, yOffset, ids)
		if err != nil {
			return nil, err
		}
		height := layoutHeight(row.Spec.Layout)
		if len(rowPanels) > 0 {
			rowGroup := &panelRow{
				key: fmt.Sprintf("v2-row-%d", rowPanels[0].ID), repeat: "",
				baseY: yOffset, height: height,
			}
			if row.Spec.Repeat != nil {
				rowGroup.repeat = row.Spec.Repeat.Value
			}
			if err := assignRowGroup(rowPanels, rowGroup); err != nil {
				return nil, err
			}
		}
		panels = append(panels, rowPanels...)
		yOffset += height
	}

	return panels, nil
}

func assignRowGroup(panels []Panel, group *panelRow) error {
	for index := range panels {
		inner := panels[index].row
		switch {
		case inner == nil:
			panels[index].row = group
		case group.repeat == "":
			// Preserve a nested row's identity when this row only supplies an offset.
		case inner.repeat != "":
			return fmt.Errorf("%w: %s", errNestedV2RowRepeat, inner.repeat)
		default:
			// A repeating outer row repeats the entire nested, non-repeating layout.
			panels[index].row = group
		}
	}

	return nil
}

func (layout dashboardV2Layout) panelsAtOffset(
	elements map[string]dashboardV2Element,
	yOffset int,
	ids *v2IDAllocator,
) ([]Panel, error) {
	panels, err := layout.panelsWithIDs(elements, ids)
	if err != nil {
		return nil, err
	}
	offsetRows := make(map[*panelRow]bool)
	for index := range panels {
		panels[index].GridPos.Y += yOffset
		row := panels[index].row
		if row != nil && !offsetRows[row] {
			row.baseY += yOffset
			offsetRows[row] = true
		}
	}

	return panels, nil
}

func gridRowsHeight(layout dashboardV2Layout) int {
	height := 1
	for _, item := range layout.Spec.Items {
		height = max(height, item.Spec.Y+item.Spec.Height)
	}

	return height + 1
}

func (layout dashboardV2Layout) autoGridPanels(
	elements map[string]dashboardV2Element,
	ids *v2IDAllocator,
) ([]Panel, error) {
	columns := layout.autoGridColumns()
	rowHeight := layout.autoGridRowHeight()
	normalized := layout
	normalized.Kind = dashboardLayoutGrid
	for index := range normalized.Spec.Items {
		item := &normalized.Spec.Items[index].Spec
		item.X = (index % columns) * (dashboardV2GridColumns / columns)
		item.Y = (index / columns) * rowHeight
		item.Width = dashboardV2GridColumns / columns
		item.Height = rowHeight
	}

	return normalized.gridPanels(elements, 0, ids)
}

func (layout dashboardV2Layout) autoGridColumns() int {
	if layout.Spec.MaxColumnCount <= 0 {
		return 3
	}

	return min(layout.Spec.MaxColumnCount, dashboardV2GridColumns)
}

func (layout dashboardV2Layout) autoGridRowHeight() int {
	switch layout.Spec.RowHeightMode {
	case "short":
		return 4
	case "tall":
		return 12
	case variableTypeCustom:
		if layout.Spec.RowHeight > 0 {
			return layout.Spec.RowHeight
		}
		return 8
	default:
		return 8
	}
}

func (layout dashboardV2Layout) tabsPanels(
	elements map[string]dashboardV2Element,
	ids *v2IDAllocator,
) ([]Panel, error) {
	panels := make([]Panel, 0)
	yOffset := 0
	for index, tab := range layout.Spec.Tabs {
		if tab.Kind != "TabsLayoutTab" {
			return nil, fmt.Errorf("%w: %s", errUnsupportedV2Layout, tab.Kind)
		}
		title := tab.Spec.Title
		if title == "" {
			title = fmt.Sprintf("Tab %d", index+1)
		}
		heading := Panel{
			ID: ids.nextID(), Type: "text", Title: title,
			Targets: nil, Datasource: nil,
			GridPos:     GridPos{H: 2, W: dashboardV2GridColumns, X: 0, Y: yOffset},
			FieldConfig: nil,
			Options:     mustMarshal(map[string]string{"content": "## " + title}),
			Panels:      nil, Transformations: nil, Repeat: "", RepeatDirection: "", MaxPerRow: 0, Collapsed: false, row: nil,
		}
		tabPanels, err := tab.Spec.Layout.panelsAtOffset(elements, yOffset+2, ids)
		if err != nil {
			return nil, err
		}
		height := 2 + layoutHeight(tab.Spec.Layout)
		tabGroup := &panelRow{
			key: fmt.Sprintf("v2-tab-%d", heading.ID), repeat: "",
			baseY: yOffset, height: height,
		}
		if tab.Spec.Repeat != nil {
			tabGroup.repeat = tab.Spec.Repeat.Value
		}
		groupPanels := append([]Panel{heading}, tabPanels...)
		if err := assignRowGroup(groupPanels, tabGroup); err != nil {
			return nil, err
		}
		panels = append(panels, groupPanels...)
		yOffset += height
	}

	return panels, nil
}

func layoutHeight(layout dashboardV2Layout) int {
	switch layout.Kind {
	case dashboardLayoutGrid:
		return gridRowsHeight(layout)
	case "AutoGridLayout":
		columns := layout.autoGridColumns()
		rows := (len(layout.Spec.Items) + columns - 1) / columns
		return rows*layout.autoGridRowHeight() + 1
	case "RowsLayout":
		height := 1
		for _, row := range layout.Spec.Rows {
			height += layoutHeight(row.Spec.Layout)
		}
		return height
	case "TabsLayout":
		height := 1
		for _, tab := range layout.Spec.Tabs {
			height += 2 + layoutHeight(tab.Spec.Layout)
		}
		return height
	default:
		return 1
	}
}

func dashboardRefID(index int) string {
	identifier := ""
	for {
		identifier = string(rune('A'+index%26)) + identifier
		if index < 26 {
			break
		}
		index = index/26 - 1
	}

	return identifier
}

func classicV2Target(query dashboardV2Query, index int) (Target, error) {
	datasource := &DatasourceRef{UID: "", Type: query.Spec.Query.Group}
	if query.Spec.Query.Datasource != nil {
		datasource.UID = query.Spec.Query.Datasource.Name
	}
	var targetMap map[string]any
	if err := json.Unmarshal(query.Spec.Query.Spec, &targetMap); err != nil {
		return Target{}, fmt.Errorf("%w: %s: %w", errInvalidV2Query, query.Spec.RefID, err)
	}
	if targetMap == nil {
		return Target{}, fmt.Errorf("%w: %s is null", errInvalidV2Query, query.Spec.RefID)
	}
	refID := query.Spec.RefID
	if refID == "" {
		refID = dashboardRefID(index)
	}
	targetMap["refId"] = refID
	if query.Spec.Hidden {
		targetMap["hide"] = true
	}
	targetMap["datasource"] = datasource
	raw, err := json.Marshal(targetMap)
	if err != nil {
		return Target{}, fmt.Errorf("encoding v2 query %q: %w", query.Spec.RefID, err)
	}

	return Target{RefID: refID, Datasource: datasource, raw: raw}, nil
}

func setV2PanelData(panel *Panel, element dashboardV2Element) error {
	for index, query := range element.Spec.Data.Spec.Queries {
		target, err := classicV2Target(query, index)
		if err != nil {
			return err
		}
		if panel.Datasource == nil {
			panel.Datasource = target.Datasource
		}
		panel.Targets = append(panel.Targets, target)
	}
	for _, transformation := range element.Spec.Data.Spec.Transformations {
		if transformation.Spec.Disabled {
			continue
		}
		panel.Transformations = append(panel.Transformations, Transformation{
			ID: transformation.Group, Options: transformation.Spec.Options,
		})
	}

	return nil
}
