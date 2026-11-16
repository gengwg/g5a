package grafanatui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gengwg/g5a/grafana"
)

const (
	chromeHeaderHeight = 2 // header + separator
	chromeFooterHeight = 3 // separator + breadcrumbs + help

	keyEnter = "enter"
	keyEsc   = "esc"
	keyUp    = "up"
	keyDown  = "down"
)

// view is the interface that all TUI views implement.
type view interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (view, tea.Cmd)
	View(width, height int) string
	Title() string
	KeyBindings() []key.Binding
	// IsFiltering reports whether the view is currently in a
	// filtering/text-input state, so the app does not intercept
	// printable keys like 'q'.
	IsFiltering() bool
}

// Options configures the App at creation time.
type Options struct {
	TimeRange      grafana.TimeRange
	Refresh        time.Duration
	RefreshSet     bool
	DashboardUID   string // if set, open this dashboard directly
	InitialPanelID int    // if set, jump to this panel
}

// App is the root Bubble Tea model that manages a view stack.
type App struct {
	client     *grafana.Client
	views      []view
	width      int
	height     int
	timeRange  grafana.TimeRange
	refresh    time.Duration
	refreshSet bool
	err        error
	showHelp   bool
}

// Navigation and lifecycle messages.
type (
	pushViewMsg           struct{ v view }
	popViewMsg            struct{}
	setTimeRangeMsg       struct{ timeRange grafana.TimeRange }
	popAndSetTimeRangeMsg struct{ timeRange grafana.TimeRange }
	popAndFilterMsg       struct{ hidden map[string]bool }
	popAndSetVariablesMsg struct{ values map[string]string }
	setVariablesMsg       struct{ values map[string]string }
	variablesResolvedMsg  struct {
		options    map[string][]string
		defaults   map[string]string
		generation int
	}
	variablesResolveErrorMsg struct {
		err        error
		generation int
	}
	errMsg         struct{ err error }
	jumpToPanelMsg struct{ index int }
)

// NewApp creates a new App and pushes the dashboard list as the
// initial view.
func NewApp(client *grafana.Client, opts Options) *App {
	app := &App{
		client:     client,
		views:      nil,
		width:      80, //nolint:mnd // sensible default
		height:     24, //nolint:mnd // sensible default
		timeRange:  opts.TimeRange,
		refresh:    opts.Refresh,
		refreshSet: opts.RefreshSet,
		err:        nil,
		showHelp:   false,
	}

	if opts.DashboardUID != "" {
		dashboard := newDashboardView(client, opts.DashboardUID)
		app.configureDashboardView(dashboard)
		app.views = []view{newDashboardListView(client), dashboard}
		if opts.InitialPanelID > 0 {
			panel := newPanelView(client, opts.DashboardUID)
			panel.initialPanelID = opts.InitialPanelID
			app.views = append(app.views, panel)
		}
	} else {
		app.views = []view{newDashboardListView(client)}
	}

	return app
}

func (a *App) configureDashboardView(dashboard *dashboardView) {
	if a.refreshSet {
		dashboard.refresh = a.refresh
		dashboard.refreshSet = true
	}
}

// Init returns initial commands for window size and the top view.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.WindowSize()}

	if len(a.views) > 0 {
		top := a.views[len(a.views)-1]
		if cmd := top.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// Update handles messages for the App.
//
//nolint:cyclop // Bubble Tea message routing is centralized here
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		return a, nil
	case dashboardRefreshMsg:
		return a.handleDashboardRefresh(msg)
	case tea.KeyMsg:
		return a.handleKeyMsg(msg)
	case pushViewMsg:
		return a.handlePushView(msg)
	case popViewMsg:
		return a.handlePopView()
	case popAndSetTimeRangeMsg:
		return a.handlePopAndForward(setTimeRangeMsg(msg))
	case popAndFilterMsg, popAndSetVariablesMsg:
		return a.handlePopAndForwardDynamic(msg)
	case setTimeRangeMsg:
		return a.handleSetTimeRange(msg)
	case errMsg:
		return a.handleErr(msg)
	default:
		return a.delegateToActiveView(msg)
	}
}

func (a *App) handleDashboardRefresh(msg dashboardRefreshMsg) (tea.Model, tea.Cmd) {
	commands := make([]tea.Cmd, 0)
	for index, current := range a.views {
		dashboard, ok := current.(*dashboardView)
		if !ok {
			continue
		}
		updated, command := dashboard.Update(msg)
		a.views[index] = updated
		if command != nil {
			commands = append(commands, command)
		}
	}

	return a, tea.Batch(commands...)
}

func (a *App) handleErr(msg errMsg) (tea.Model, tea.Cmd) {
	a.err = msg.err

	return a, nil
}

// handleSetTimeRange updates the time range and forwards to the
// active view.
func (a *App) handleSetTimeRange(msg setTimeRangeMsg) (tea.Model, tea.Cmd) {
	a.timeRange = msg.timeRange

	return a.delegateToActiveView(msg)
}

// handlePopAndForwardDynamic pops the top overlay and forwards a
// message that may need type conversion.
func (a *App) handlePopAndForwardDynamic(msg tea.Msg) (tea.Model, tea.Cmd) {
	if len(a.views) > 1 {
		a.views = a.views[:len(a.views)-1]
	}

	if m, ok := msg.(popAndSetVariablesMsg); ok {
		return a.delegateToActiveView(setVariablesMsg(m))
	}

	return a.delegateToActiveView(msg)
}

// handlePushView pushes a new view onto the stack.
func (a *App) handlePushView(msg pushViewMsg) (tea.Model, tea.Cmd) {
	if pv, ok := msg.v.(*panelView); ok && !pv.preserveTimeRange {
		pv.timeRange = a.timeRange
	}
	if dashboard, ok := msg.v.(*dashboardView); ok {
		a.configureDashboardView(dashboard)
	}

	a.views = append(a.views, msg.v)

	return a, msg.v.Init()
}

// handlePopAndForward pops the top overlay view and forwards the
// given message to the now-active view underneath.
func (a *App) handlePopAndForward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if len(a.views) > 1 {
		a.views = a.views[:len(a.views)-1]
	}

	return a.delegateToActiveView(msg)
}

// handleKeyMsg processes global key bindings.
func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "?":
		a.showHelp = !a.showHelp

		return a, nil
	case "q":
		// Let the active view handle 'q' if it's filtering.
		if a.activeViewHandlesKey(msg) {
			return a.delegateToActiveView(msg)
		}

		return a, tea.Quit
	default:
		return a.delegateToActiveView(msg)
	}
}

// activeViewHandlesKey returns true if the active view is currently
// filtering, meaning printable keys like 'q' should be forwarded to
// the view instead of being handled as global shortcuts.
func (a *App) activeViewHandlesKey(_ tea.KeyMsg) bool {
	if len(a.views) == 0 {
		return false
	}

	return a.views[len(a.views)-1].IsFiltering()
}

// handlePopView pops the top view off the stack. If the stack would
// become empty, the app quits. The newly active view is re-initialized
// if it hasn't been loaded yet (e.g., dashboard list skipped on
// deep-link startup).
func (a *App) handlePopView() (tea.Model, tea.Cmd) {
	if len(a.views) <= 1 {
		return a, tea.Quit
	}

	a.views = a.views[:len(a.views)-1]

	top := a.views[len(a.views)-1]
	switch current := top.(type) {
	case *dashboardListView:
		if !current.loaded {
			return a, current.Init()
		}
	case *dashboardView:
		if current.dashboard == nil {
			return a, current.Init()
		}
	}

	return a, nil
}

// delegateToActiveView forwards a message to the top view on the
// stack.
func (a *App) delegateToActiveView(
	msg tea.Msg,
) (tea.Model, tea.Cmd) {
	if len(a.views) == 0 {
		return a, nil
	}

	top := a.views[len(a.views)-1]
	updated, cmd := top.Update(msg)
	a.views[len(a.views)-1] = updated

	return a, cmd
}

// View renders the full application chrome and the active view.
func (a *App) View() string {
	if a.showHelp {
		return a.renderHelp()
	}

	header := a.renderHeader()
	footer := a.renderFooter()

	contentHeight := max(
		a.height-chromeHeaderHeight-chromeFooterHeight, 0,
	)

	content := ""
	if len(a.views) > 0 {
		content = a.views[len(a.views)-1].View(a.width, contentHeight)
	}

	// Pad content to fill the full area. Each line gets an ANSI
	// reset before padding to prevent background color bleed from
	// colored content (e.g. heatmap cells).
	content = padContent(content, a.width, contentHeight)

	return header + "\n" + content + "\n" + footer
}

// renderHeader renders the top chrome bar.
func (a *App) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{
			Light: colorLightText,
			Dark:  colorDarkText,
		})

	urlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: "#666666",
			Dark:  colorLightMuted,
		})

	header := titleStyle.Render(" g5a") +
		urlStyle.Render(" │ "+a.client.BaseURL())

	separator := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: "#CCCCCC",
			Dark:  colorDarkBorder,
		}).
		Render(strings.Repeat("─", a.width))

	return header + "\n" + separator
}

// renderFooter renders the breadcrumbs and help hints.
func (a *App) renderFooter() string {
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: "#CCCCCC",
			Dark:  colorDarkBorder,
		}).
		Render(strings.Repeat("─", a.width))

	breadcrumbs := a.renderBreadcrumbs()
	hints := a.renderHelpHints()

	return separator + "\n" + breadcrumbs + "\n" + hints
}

// renderBreadcrumbs builds the breadcrumb trail from the view stack.
func (a *App) renderBreadcrumbs() string {
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: colorLightMuted,
			Dark:  colorDarkMuted,
		})

	boldStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{
			Light: colorLightText,
			Dark:  colorDarkText,
		})

	var parts []string

	for i, v := range a.views {
		if i == len(a.views)-1 {
			parts = append(parts, boldStyle.Render(v.Title()))
		} else {
			parts = append(parts, dimStyle.Render(v.Title()))
		}
	}

	return " " + strings.Join(parts, dimStyle.Render(" > "))
}

// renderHelpHints shows key binding hints for the active view.
func (a *App) renderHelpHints() string {
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: colorLightMuted,
			Dark:  colorDarkMuted,
		})

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: colorDarkBorder,
			Dark:  "#BBBBBB",
		})

	var hints []string

	if len(a.views) > 0 {
		for _, binding := range a.views[len(a.views)-1].KeyBindings() {
			hint := keyStyle.Render(binding.Help().Key) +
				hintStyle.Render(":"+binding.Help().Desc)
			hints = append(hints, hint)
		}
	}

	hints = append(hints, keyStyle.Render("?")+hintStyle.Render(":help"))
	hints = append(hints, keyStyle.Render("q")+hintStyle.Render(":quit"))

	return " " + strings.Join(hints, "  ")
}

// renderHelp renders a full-screen help overlay.
func (a *App) renderHelp() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{
			Light: colorLightText,
			Dark:  colorDarkText,
		})

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: colorDarkBorder,
			Dark:  "#BBBBBB",
		}).
		Width(15) //nolint:mnd // fixed column width for alignment

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{
			Light: "#666666",
			Dark:  colorLightMuted,
		})

	var lines []string

	lines = append(lines, titleStyle.Render("Key Bindings"))
	lines = append(lines, "")

	if len(a.views) > 0 {
		for _, binding := range a.views[len(a.views)-1].KeyBindings() {
			line := keyStyle.Render(binding.Help().Key) +
				descStyle.Render(binding.Help().Desc)
			lines = append(lines, line)
		}
	}

	lines = append(lines, "")
	lines = append(lines, titleStyle.Render("Global"))
	lines = append(lines, keyStyle.Render("?")+descStyle.Render("toggle help"))
	lines = append(lines, keyStyle.Render("q")+descStyle.Render("quit"))
	lines = append(lines, keyStyle.Render("ctrl+c")+descStyle.Render("force quit"))
	lines = append(lines, "")
	lines = append(lines, descStyle.Render("Press ? to close"))

	return strings.Join(lines, "\n")
}
