package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// DashboardSearchResult represents a dashboard in search results.
type DashboardSearchResult struct {
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	URI         string   `json:"uri"`
	Type        string   `json:"type"`
	Tags        []string `json:"tags"`
	FolderUID   string   `json:"folderUid"`
	FolderTitle string   `json:"folderTitle"`
}

// RefreshInterval accepts Grafana's string, boolean, and numeric refresh forms.
type RefreshInterval string

// UnmarshalJSON decodes a dashboard refresh interval.
func (r *RefreshInterval) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*r = RefreshInterval(text)
		return nil
	}

	var disabled bool
	if err := json.Unmarshal(data, &disabled); err == nil {
		if disabled {
			*r = ""
		} else {
			*r = "0s"
		}
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*r = RefreshInterval(number.String())
		return nil
	}

	return fmt.Errorf("%w: %s", errInvalidRefresh, string(data))
}

var errInvalidRefresh = errors.New("invalid dashboard refresh value")

// Dashboard represents a Grafana dashboard.
type Dashboard struct {
	Title      string          `json:"title"`
	Panels     []Panel         `json:"panels"`
	Templating Templating      `json:"templating"`
	Time       DashboardTime   `json:"time"`
	Refresh    RefreshInterval `json:"refresh"`
	Timezone   string          `json:"timezone,omitempty"`
	WeekStart  string          `json:"weekStart,omitempty"`
	NowDelay   string          `json:"nowDelay,omitempty"`
}

// Panel represents a single panel within a dashboard.
type Panel struct {
	ID              int              `json:"id"`
	Type            string           `json:"type"`
	Title           string           `json:"title"`
	Targets         []Target         `json:"targets"`
	Datasource      *DatasourceRef   `json:"datasource,omitempty"`
	GridPos         GridPos          `json:"gridPos"`
	FieldConfig     json.RawMessage  `json:"fieldConfig,omitempty"`
	Options         json.RawMessage  `json:"options,omitempty"`
	Panels          []Panel          `json:"panels,omitempty"`
	Transformations []Transformation `json:"transformations,omitempty"`
	Repeat          string           `json:"repeat,omitempty"`
	RepeatDirection string           `json:"repeatDirection,omitempty"`
	MaxPerRow       int              `json:"maxPerRow,omitempty"`
	Collapsed       bool             `json:"collapsed,omitempty"`
	Timezone        string           `json:"-"`
	row             *panelRow
}

type panelRow struct {
	key    string
	repeat string
	baseY  int
	height int
}

// Transformation represents a single panel data transformation.
type Transformation struct {
	ID      string          `json:"id"`
	Options json.RawMessage `json:"options,omitempty"`
}

// Target represents a query target within a panel. It preserves the
// raw JSON so it can be forwarded verbatim to /api/ds/query.
type Target struct {
	RefID      string         `json:"refId"`
	Datasource *DatasourceRef `json:"datasource,omitempty"`
	raw        json.RawMessage
}

// UnmarshalJSON parses known fields and stores the raw bytes.
func (t *Target) UnmarshalJSON(data []byte) error {
	// Store the raw bytes for later forwarding.
	t.raw = make(json.RawMessage, len(data))
	copy(t.raw, data)

	// Parse known fields into a temporary struct to avoid recursion.
	var fields struct {
		RefID      string         `json:"refId"`
		Datasource *DatasourceRef `json:"datasource,omitempty"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("unmarshaling target fields: %w", err)
	}

	t.RefID = fields.RefID
	t.Datasource = fields.Datasource

	return nil
}

// MarshalJSON returns the original raw bytes.
func (t Target) MarshalJSON() ([]byte, error) {
	if t.raw != nil {
		return t.raw, nil
	}

	// Fallback: marshal the known fields.
	type plain struct {
		RefID      string         `json:"refId"`
		Datasource *DatasourceRef `json:"datasource,omitempty"`
	}

	data, err := json.Marshal(plain{
		RefID: t.RefID, Datasource: t.Datasource,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling target: %w", err)
	}

	return data, nil
}

// Raw returns the raw JSON bytes for this target.
func (t *Target) Raw() json.RawMessage {
	return t.raw
}

// DatasourceRef identifies a datasource by UID and type. Older
// dashboards may encode this as a plain string instead of an object.
type DatasourceRef struct {
	UID  string `json:"uid"`
	Type string `json:"type"`
}

// UnmarshalJSON handles both string and object forms of a datasource
// reference. A plain string like "Static" is stored as the UID.
func (d *DatasourceRef) UnmarshalJSON(data []byte) error {
	// Try as a plain string first.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		d.UID = str
		d.Type = ""

		return nil
	}

	// Fall back to object form.
	type plain DatasourceRef

	var obj plain
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("unmarshaling datasource ref: %w", err)
	}

	*d = DatasourceRef(obj)

	return nil
}

// DashboardTime holds the dashboard's default time range.
type DashboardTime struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// GridPos describes a panel's position and size on the grid.
type GridPos struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

// Templating holds a dashboard's template variable definitions.
type Templating struct {
	List []TemplateVariable `json:"list"`
}

// TemplateVariable represents a single template variable.
type TemplateVariable struct {
	Name       string                   `json:"name"`
	Type       string                   `json:"type"`
	Current    TemplateVariableCurrent  `json:"current"`
	IncludeAll bool                     `json:"includeAll,omitempty"`
	AllValue   string                   `json:"allValue,omitempty"`
	Multi      bool                     `json:"multi,omitempty"`
	Definition string                   `json:"definition,omitempty"`
	Query      json.RawMessage          `json:"query,omitempty"`
	Datasource *DatasourceRef           `json:"datasource,omitempty"`
	Options    []TemplateVariableOption `json:"options,omitempty"`
	Regex      string                   `json:"regex,omitempty"`
	Sort       int                      `json:"sort,omitempty"`
	Refresh    int                      `json:"refresh,omitempty"`
	Hide       int                      `json:"hide,omitempty"`
}

// TemplateVariableOption represents a selectable option for a
// template variable.
type TemplateVariableOption struct {
	Text     string `json:"text"`
	Value    string `json:"value"`
	Selected bool   `json:"selected"`
}

// TemplateVariableCurrent holds the current value of a template
// variable. Both Text and Value can be either a string or []string.
type TemplateVariableCurrent struct {
	Text  json.RawMessage `json:"text"`
	Value json.RawMessage `json:"value"`
}

// dashboardResponse wraps the Grafana API response for
// GET /api/dashboards/uid/:uid.
type dashboardResponse struct {
	Dashboard Dashboard `json:"dashboard"`
}

// SearchDashboards returns all dashboards matching the query.
func (c *Client) SearchDashboards(
	ctx context.Context,
	query string,
) ([]DashboardSearchResult, error) {
	params := url.Values{
		"type":  {"dash-db"},
		"limit": {"1000"},
	}
	if query != "" {
		params.Set("query", query)
	}

	results := make([]DashboardSearchResult, 0)
	for page := 1; ; page++ {
		params.Set("page", strconv.Itoa(page))
		var pageResults []DashboardSearchResult
		if err := c.doJSON(ctx, "/api/search?"+params.Encode(), &pageResults); err != nil {
			return nil, fmt.Errorf("searching dashboards: %w", err)
		}
		results = append(results, pageResults...)
		if len(pageResults) < 1000 {
			return results, nil
		}
	}
}

// GetDashboard returns the full dashboard by UID.
func (c *Client) GetDashboard(
	ctx context.Context,
	uid string,
) (*Dashboard, error) {
	path := "/api/dashboards/uid/" + url.PathEscape(uid)

	var raw json.RawMessage
	if err := c.doJSON(ctx, path, &raw); err != nil {
		if !dashboardFallbackStatus(err) {
			return nil, fmt.Errorf("getting dashboard: %w", err)
		}
		return c.getDashboardV2(ctx, uid)
	}
	dashboard, err := c.dashboardFromResource(ctx, raw)
	if err != nil {
		return nil, err
	}

	return dashboard, nil
}

// skipPanelTypes lists panel types that are not useful in terminal
// mode and should be excluded from navigation.
var skipPanelTypes = map[string]bool{ //nolint:gochecknoglobals // read-only set
	"innius-video-panel": true,
}

// FlattenPanels returns all renderable panels, expanding row panels
// that contain nested sub-panels. Non-data panels (text, dashlist,
// etc.) are skipped.
func FlattenPanels(panels []Panel) []Panel {
	rowCounter := 0
	result := flattenPanels(panels, nil, &rowCounter)
	for _, panel := range result {
		if panel.row == nil {
			continue
		}
		panel.row.height = max(
			panel.row.height,
			panel.GridPos.Y+panel.GridPos.H-panel.row.baseY,
		)
	}

	return result
}

func flattenPanels(panels []Panel, inherited *panelRow, counter *int) []Panel {
	var result []Panel
	activeRow := inherited

	for _, panel := range panels {
		if panel.Type == "row" {
			*counter++
			row := &panelRow{
				key:    fmt.Sprintf("row-%d-%d", *counter, panel.ID),
				repeat: panel.Repeat,
				baseY:  panel.GridPos.Y,
				height: max(panel.GridPos.H, 1),
			}
			result = append(result, flattenPanels(panel.Panels, row, counter)...)
			if panel.Collapsed {
				activeRow = inherited
			} else {
				activeRow = row
			}
			continue
		}

		if skipPanelTypes[panel.Type] {
			continue
		}

		if activeRow != nil {
			panel.row = activeRow
		}
		result = append(result, panel)
	}

	return result
}
