package grafanatui

import (
	"context"
	"errors"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

const dashboardQueryConcurrency = 4

type dashboardPanelDataMsg struct {
	key        string
	generation int
	result     *grafana.QueryResult
}

type dashboardPanelErrorMsg struct {
	key        string
	generation int
	err        error
}

type dashboardRefreshMsg struct {
	generation int
}

type dashboardLoadErrorMsg struct {
	err error
}

// dashboardView renders all concrete panels and owns their refresh lifecycle.
type dashboardView struct {
	client    *grafana.Client
	uid       string
	dashboard *grafana.Dashboard
	instances []grafana.PanelInstance

	results map[string]*grafana.QueryResult
	errors  map[string]error
	loading map[string]bool

	variableValues  map[string]string
	variableOptions map[string][]string
	timeRange       grafana.TimeRange
	refresh         time.Duration
	refreshSet      bool

	selected int
	scroll   int
	width    int
	height   int
	err      error

	generation         int
	variableGeneration int
	cancel             context.CancelFunc
	semaphore          chan struct{}
}

func newDashboardView(client *grafana.Client, uid string) *dashboardView {
	return &dashboardView{ //nolint:exhaustruct // optional runtime state starts zero-valued
		client:    client,
		uid:       uid,
		instances: []grafana.PanelInstance{},
		results:   make(map[string]*grafana.QueryResult),
		errors:    make(map[string]error),
		loading:   make(map[string]bool),
		timeRange: grafana.TimeRange{From: defaultTimeFrom, To: timeNow},
		semaphore: make(chan struct{}, dashboardQueryConcurrency),
	}
}

func (d *dashboardView) Init() tea.Cmd {
	return d.fetchDashboard()
}

//nolint:cyclop // dashboard message routing is centralized here
func (d *dashboardView) Update(msg tea.Msg) (view, tea.Cmd) {
	switch msg := msg.(type) {
	case dashboardLoadedMsg:
		return d.handleDashboardLoaded(msg)
	case variablesResolvedMsg:
		return d.handleVariablesResolved(msg)
	case variablesResolveErrorMsg:
		return d.handleVariablesResolveError(msg)
	case dashboardPanelDataMsg:
		d.handlePanelData(msg)
		return d, nil
	case dashboardPanelErrorMsg:
		d.handlePanelError(msg)
		return d, nil
	case dashboardLoadErrorMsg:
		d.err = msg.err
		return d, nil
	case dashboardRefreshMsg:
		if msg.generation != d.generation {
			return d, nil
		}
		return d, d.refreshAndSchedule()
	case setVariablesMsg:
		return d, d.changeVariables(msg.values)
	case setTimeRangeMsg:
		d.timeRange = msg.timeRange
		d.invalidatePanelQueries()
		return d, d.resolveVariables()
	case tea.KeyMsg:
		return d.handleKeyMsg(msg)
	default:
		return d, nil
	}
}

func (d *dashboardView) handleDashboardLoaded(
	msg dashboardLoadedMsg,
) (view, tea.Cmd) {
	d.dashboard = msg.dashboard
	d.err = nil
	if d.dashboard.Time.From != "" && d.dashboard.Time.To != "" {
		d.timeRange = grafana.TimeRange{
			From: d.dashboard.Time.From,
			To:   d.dashboard.Time.To,
		}
	}
	if !d.refreshSet {
		d.refresh = dashboardRefreshInterval(d.dashboard.Refresh)
	}

	return d, d.resolveVariables()
}

func (d *dashboardView) handleVariablesResolved(
	msg variablesResolvedMsg,
) (view, tea.Cmd) {
	if msg.generation != d.variableGeneration {
		return d, nil
	}
	d.variableOptions = msg.options
	d.variableValues = msg.defaults
	d.err = nil
	d.expandPanels()

	return d, d.refreshAndSchedule()
}

func (d *dashboardView) handleVariablesResolveError(
	msg variablesResolveErrorMsg,
) (view, tea.Cmd) {
	if msg.generation != d.variableGeneration {
		return d, nil
	}
	d.err = msg.err
	clear(d.loading)

	return d, nil
}

func (d *dashboardView) handlePanelData(msg dashboardPanelDataMsg) {
	if msg.generation != d.generation {
		return
	}
	d.results[msg.key] = msg.result
	delete(d.errors, msg.key)
	delete(d.loading, msg.key)
}

func (d *dashboardView) handlePanelError(msg dashboardPanelErrorMsg) {
	if msg.generation != d.generation {
		return
	}
	if !isContextCancellation(msg.err) {
		d.errors[msg.key] = msg.err
	}
	delete(d.loading, msg.key)
}

//nolint:cyclop // keyboard command routing is centralized here
func (d *dashboardView) handleKeyMsg(msg tea.KeyMsg) (view, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		if d.cancel != nil {
			d.cancel()
		}
		return d, func() tea.Msg { return popViewMsg{} }
	case "r":
		return d, d.refreshAndSchedule()
	case "t":
		return d, func() tea.Msg {
			return pushViewMsg{v: newTimeRangePickerView(d.timeRange)}
		}
	case "v":
		return d, d.openVariablePicker()
	case "h", directionLeft:
		d.moveSelection(-1)
	case "l", directionRight:
		d.moveSelection(1)
	case "j", keyDown:
		d.scrollBy(1)
	case "k", keyUp:
		d.scrollBy(-1)
	case "pgdown":
		d.scrollBy(max(d.height-1, 1))
	case "pgup":
		d.scrollBy(-max(d.height-1, 1))
	case "home":
		d.scroll = 0
	case "end":
		d.scroll = d.maxScroll()
	case keyEnter:
		return d.openSelectedPanel()
	}

	return d, nil
}

func (d *dashboardView) fetchDashboard() tea.Cmd {
	client := d.client
	uid := d.uid

	return func() tea.Msg {
		dashboard, err := client.GetDashboard(contextBackground(), uid)
		if err != nil {
			return dashboardLoadErrorMsg{err: err}
		}

		return dashboardLoadedMsg{dashboard: dashboard}
	}
}

func (d *dashboardView) resolveVariables() tea.Cmd {
	if d.dashboard == nil {
		return nil
	}
	d.variableGeneration++
	generation := d.variableGeneration
	client := d.client
	variables := d.dashboard.Templating.List
	timeRange := d.timeRange
	selections := cloneVariableValues(d.variableValues)

	return func() tea.Msg {
		options, defaults, err := client.ResolveVariableOptionsWithSelections(
			contextBackground(),
			variables,
			timeRange,
			selections,
		)
		if err != nil {
			return variablesResolveErrorMsg{err: err, generation: generation}
		}

		return variablesResolvedMsg{
			options: options, defaults: defaults, generation: generation,
		}
	}
}

func (d *dashboardView) changeVariables(values map[string]string) tea.Cmd {
	d.variableValues = values
	d.invalidatePanelQueries()

	return d.resolveVariables()
}

func (d *dashboardView) invalidatePanelQueries() {
	if d.cancel != nil {
		d.cancel()
	}
	d.generation++
}

func (d *dashboardView) expandPanels() {
	if d.dashboard == nil {
		return
	}
	d.instances = grafana.ExpandPanels(
		d.dashboard.Panels,
		d.dashboard.Templating.List,
		d.variableOptions,
		d.variableValues,
	)
	d.results = make(map[string]*grafana.QueryResult)
	d.errors = make(map[string]error)
	d.loading = make(map[string]bool)
	d.selected = min(d.selected, max(len(d.instances)-1, 0))
	d.scroll = 0
}

func (d *dashboardView) refreshAndSchedule() tea.Cmd {
	refresh := d.startRefresh()
	if d.refresh <= 0 {
		return refresh
	}
	generation := d.generation
	tick := tea.Tick(d.refresh, func(time.Time) tea.Msg {
		return dashboardRefreshMsg{generation: generation}
	})

	return tea.Batch(refresh, tick)
}

func (d *dashboardView) startRefresh() tea.Cmd {
	d.invalidatePanelQueries()
	if len(d.instances) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	d.cancel = cancel
	generation := d.generation
	widths := d.panelWidths()
	commands := make([]tea.Cmd, 0, len(d.instances))

	for _, instance := range d.instances {
		d.loading[instance.Key] = true
		commands = append(commands, d.fetchPanel(
			ctx,
			generation,
			instance,
			widths[instance.Key],
		))
	}

	return tea.Batch(commands...)
}

func (d *dashboardView) fetchPanel(
	ctx context.Context,
	generation int,
	instance grafana.PanelInstance,
	width int,
) tea.Cmd {
	timeRange := d.timeRange
	variables := d.dashboard.Templating.List
	allPanels := grafana.FlattenPanels(d.dashboard.Panels)

	return func() tea.Msg {
		select {
		case d.semaphore <- struct{}{}:
			defer func() { <-d.semaphore }()
		case <-ctx.Done():
			return dashboardPanelErrorMsg{
				key: instance.Key, generation: generation, err: ctx.Err(),
			}
		}

		maxDataPoints := max(width, 20)
		result, err := d.client.QueryPanel(
			ctx,
			instance.Panel,
			timeRange,
			maxDataPoints,
			variables,
			instance.Variables,
			allPanels,
		)
		if err != nil {
			return dashboardPanelErrorMsg{
				key: instance.Key, generation: generation, err: err,
			}
		}

		return dashboardPanelDataMsg{
			key: instance.Key, generation: generation, result: result,
		}
	}
}

func (d *dashboardView) panelWidths() map[string]int {
	width := d.width
	if width <= 0 {
		width = 80
	}
	rects := layoutPanelInstances(d.instances, width)
	widths := make(map[string]int, len(rects))
	for _, rect := range rects {
		widths[d.instances[rect.InstanceIndex].Key] = max(rect.W-2, 1)
	}

	return widths
}

func (d *dashboardView) openVariablePicker() tea.Cmd {
	if d.dashboard == nil {
		return nil
	}
	picker := newVariablePickerView(
		d.dashboard.Templating.List,
		d.variableOptions,
		d.variableValues,
	)

	return func() tea.Msg { return pushViewMsg{v: picker} }
}

func (d *dashboardView) openSelectedPanel() (view, tea.Cmd) {
	if d.selected < 0 || d.selected >= len(d.instances) {
		return d, nil
	}
	instance := d.instances[d.selected]
	panel := newPanelView(d.client, d.uid)
	panel.initialPanelID = instance.Panel.ID
	panel.initialVariables = cloneVariableValues(instance.Variables)
	panel.preserveTimeRange = true
	panel.timeRange = d.timeRange

	return d, func() tea.Msg { return pushViewMsg{v: panel} }
}

func (d *dashboardView) moveSelection(delta int) {
	if len(d.instances) == 0 {
		return
	}
	d.selected = (d.selected + delta + len(d.instances)) % len(d.instances)
	d.ensureSelectionVisible()
}

func (d *dashboardView) ensureSelectionVisible() {
	if d.selected < 0 || d.selected >= len(d.instances) {
		return
	}
	rects := layoutPanelInstances(d.instances, max(d.width, 1))
	for _, rect := range rects {
		if rect.InstanceIndex != d.selected {
			continue
		}
		if rect.Y < d.scroll {
			d.scroll = rect.Y
		}
		if rect.Y+rect.H > d.scroll+d.height {
			d.scroll = rect.Y + rect.H - d.height
		}
		d.scroll = max(d.scroll, 0)
		return
	}
}

func (d *dashboardView) scrollBy(delta int) {
	d.scroll = min(max(d.scroll+delta, 0), d.maxScroll())
}

func (d *dashboardView) maxScroll() int {
	height := totalGridHeight(layoutPanelInstances(d.instances, max(d.width, 1)))

	return max(height-d.height, 0)
}

func (d *dashboardView) View(width, height int) string {
	d.width = width
	d.height = height
	if d.err != nil {
		return centerText("Error: "+d.err.Error(), width, height)
	}
	if d.dashboard == nil {
		return centerText("Loading dashboard...", width, height)
	}
	if d.variableValues == nil && len(d.dashboard.Templating.List) > 0 {
		return centerText("Resolving variables...", width, height)
	}
	if len(d.instances) == 0 {
		return centerText("No panels found", width, height)
	}

	canvas := d.renderCanvas(width)
	d.scroll = min(d.scroll, d.maxScroll())
	start := min(d.scroll, len(canvas))
	end := min(start+height, len(canvas))
	visible := canvas[start:end]
	if len(visible) == 0 {
		return ""
	}

	return strings.Join(visible, "\n")
}

func (d *dashboardView) renderCanvas(width int) []string {
	rects := layoutPanelInstances(d.instances, width)
	byRow := make(map[int][]gridRect)
	rows := make([]int, 0)
	for _, rect := range rects {
		if _, exists := byRow[rect.Y]; !exists {
			rows = append(rows, rect.Y)
		}
		byRow[rect.Y] = append(byRow[rect.Y], rect)
	}
	sort.Ints(rows)

	canvas := make([]string, 0, totalGridHeight(rects))
	for _, row := range rows {
		rowRects := byRow[row]
		sort.Slice(rowRects, func(i, j int) bool {
			return rowRects[i].X < rowRects[j].X
		})
		blocks := make([]string, 0, len(rowRects))
		currentX := 0
		rowHeight := 0
		for _, rect := range rowRects {
			if rect.X > currentX {
				blocks = append(blocks, strings.Repeat(" ", rect.X-currentX))
			}
			instance := d.instances[rect.InstanceIndex]
			blocks = append(blocks, d.renderPanelBlock(instance, rect))
			currentX = rect.X + rect.W
			rowHeight = max(rowHeight, rect.H)
		}
		band := lipgloss.JoinHorizontal(0, blocks...)
		lines := strings.Split(band, "\n")
		for len(lines) < rowHeight {
			lines = append(lines, "")
		}
		canvas = append(canvas, lines...)
	}

	return canvas
}

func (d *dashboardView) renderPanelBlock(
	instance grafana.PanelInstance,
	rect gridRect,
) string {
	title := instance.Panel.Title
	if d.loading[instance.Key] {
		title = "* " + title
	} else if d.errors[instance.Key] != nil {
		title = "! " + title
	} else if result := d.results[instance.Key]; result != nil && len(result.Warnings) > 0 {
		title = "! " + title
	}
	title = truncateTableText(title, max(rect.W-1, 1))
	titleStyle := lipgloss.NewStyle().Bold(true).Width(rect.W)
	if rect.InstanceIndex == d.selected {
		titleStyle = titleStyle.Foreground(lipgloss.Color("#00FFFF"))
	}

	bodyHeight := max(rect.H-1, 1)
	body := d.renderPanelBody(instance, rect.W, bodyHeight)
	content := titleStyle.Render(title) + "\n" + body
	content = lipgloss.NewStyle().MaxWidth(rect.W).Render(content)

	return padContent(content, rect.W, rect.H)
}

func (d *dashboardView) renderPanelBody(
	instance grafana.PanelInstance,
	width int,
	height int,
) string {
	result := d.results[instance.Key]
	if result == nil {
		if err := d.errors[instance.Key]; err != nil {
			return centerText("Error: "+err.Error(), width, height)
		}
		if d.loading[instance.Key] {
			return centerText("Loading...", width, height)
		}
		return centerText("No data", width, height)
	}

	renderer := RendererForPanel(instance.Panel.Type)
	if len(result.Warnings) == 0 {
		return renderer.Render(result, instance.Panel, width, height, nil)
	}

	warning := "! " + strings.Join(result.Warnings, "; ")
	warning = truncateTableText(warning, width)
	body := renderer.Render(result, instance.Panel, width, max(height-1, 1), nil)

	return warning + "\n" + body
}

func (d *dashboardView) Title() string {
	if d.dashboard == nil {
		return "Loading..."
	}

	return d.dashboard.Title
}

func (d *dashboardView) IsFiltering() bool { return false }

func (d *dashboardView) KeyBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys(keyEnter), key.WithHelp("enter", "zoom")),
		key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "select")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time")),
		key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "vars")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func dashboardRefreshInterval(value string) time.Duration {
	if value != "" {
		if interval, err := time.ParseDuration(value); err == nil && interval > 0 {
			return interval
		}
	}

	return 30 * time.Second
}

func cloneVariableValues(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	maps.Copy(clone, values)

	return clone
}

func isContextCancellation(err error) bool {
	return errors.Is(err, context.Canceled)
}
