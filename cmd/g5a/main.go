package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gengwg/g5a/grafana"
	"github.com/gengwg/g5a/grafanatui"
	"github.com/spf13/cobra"
)

var (
	version = "(devel)"

	errURLRequired = errors.New("URL is required: set --url or GRAFANA_URL")
)

func main() {
	var flags flags

	rootCmd := &cobra.Command{ //nolint:exhaustruct // cobra commands use optional fields
		Use:   "g5a",
		Short: "Browse Grafana dashboards in the terminal",
		Long: `Connect to a remote Grafana instance and browse dashboards in a
terminal UI. Authenticate with a service account token (recommended) or
basic auth credentials.

Authentication priority: command-line flags override environment variables.
For the URL set --url or GRAFANA_URL.
For token auth set --token or GRAFANA_SERVICE_ACCOUNT_TOKEN.
For basic auth set --username/--password or GRAFANA_USERNAME/GRAFANA_PASSWORD.
When both are available, token auth takes precedence.`,
		Args:    cobra.NoArgs,
		Version: version,
		RunE: func(command *cobra.Command, _ []string) error {
			flags.refreshSet = command.Flags().Changed("refresh")
			return run(flags)
		},
	}

	rootCmd.Flags().StringVar(
		&flags.url, "url", "",
		"Grafana instance URL (env: GRAFANA_URL)",
	)
	rootCmd.Flags().StringVar(
		&flags.token, "token", "",
		"Grafana service account token (env: GRAFANA_SERVICE_ACCOUNT_TOKEN)",
	)
	rootCmd.Flags().StringVar(
		&flags.username, "username", "",
		"Grafana basic auth username (env: GRAFANA_USERNAME)",
	)
	rootCmd.Flags().StringVar(
		&flags.password, "password", "",
		"Grafana basic auth password (env: GRAFANA_PASSWORD)",
	)
	rootCmd.Flags().DurationVar(
		&flags.refresh, "refresh", 0,
		"Auto-refresh interval (dashboard setting or 30s; 0 disables)",
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// flags holds the CLI flags for the root command.
type flags struct {
	url        string
	token      string
	username   string
	password   string
	refresh    time.Duration
	refreshSet bool
}

// run creates the Grafana client and starts the TUI.
func run(f flags) error {
	cleanup := initStderr()
	defer cleanup()

	f.url = resolveEnv(f.url, "GRAFANA_URL")
	if f.url == "" {
		return errURLRequired
	}

	parts := parseGrafanaURL(f.url)
	f.url = parts.baseURL

	clientOpts := resolveAuth(f)
	if parts.orgID != "" {
		clientOpts = append(clientOpts, grafana.WithOrgID(parts.orgID))
	}

	client, err := grafana.NewClient(f.url, clientOpts...)
	if err != nil {
		return fmt.Errorf("creating grafana client: %w", err)
	}

	// Validate connection before entering TUI.
	if err := client.Ping(context.Background()); err != nil {
		return fmt.Errorf("connecting to grafana: %w", err)
	}

	app := grafanatui.NewApp(client, grafanatui.Options{
		TimeRange:        parts.timeRange,
		TimeRangeSet:     parts.timeRangeSet,
		Refresh:          f.refresh,
		RefreshSet:       f.refreshSet,
		DashboardUID:     parts.dashUID,
		InitialPanelID:   parts.panelID,
		InitialVariables: parts.variables,
	})

	program := tea.NewProgram(app, tea.WithAltScreen())
	if _, err = program.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	return nil
}

// resolveAuth builds client options from flags and env vars.
func resolveAuth(f flags) []grafana.ClientOption {
	resolvedToken := resolveEnv(
		f.token, "GRAFANA_SERVICE_ACCOUNT_TOKEN",
	)
	resolvedUsername := resolveEnv(
		f.username, "GRAFANA_USERNAME",
	)
	resolvedPassword := resolveEnv(
		f.password, "GRAFANA_PASSWORD",
	)

	switch {
	case resolvedToken != "":
		return []grafana.ClientOption{
			grafana.WithToken(resolvedToken),
		}
	case resolvedUsername != "":
		return []grafana.ClientOption{
			grafana.WithBasicAuth(resolvedUsername, resolvedPassword),
		}
	default:
		return nil
	}
}

// resolveEnv returns the flag value if non-empty, otherwise falls back
// to the named environment variable.
func resolveEnv(flagValue, envKey string) string {
	if flagValue != "" {
		return flagValue
	}

	return os.Getenv(envKey)
}

const timeNow = "now"

// dashboardPathPattern matches /d/{uid} or /d-solo/{uid} with an optional slug.
var dashboardPathPattern = regexp.MustCompile(`/(?:d|d-solo)/([^/?]+)`)

type grafanaURLParts struct {
	baseURL      string
	dashUID      string
	panelID      int
	timeRange    grafana.TimeRange
	timeRangeSet bool
	variables    map[string]string
	orgID        string
}

// parseGrafanaURL extracts connection, dashboard, and initial view options.
func parseGrafanaURL(rawURL string) grafanaURLParts {
	parts := grafanaURLParts{
		baseURL: "", dashUID: "", panelID: 0, orgID: "", timeRangeSet: false,
		timeRange: grafana.TimeRange{From: "now-1h", To: timeNow},
		variables: map[string]string{},
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		parts.baseURL = rawURL

		return parts
	}
	matches := dashboardPathPattern.FindStringSubmatch(parsed.Path)
	if matches == nil {
		parts.baseURL = rawURL

		return parts
	}
	parts.dashUID = matches[1]
	if viewPanel := parsed.Query().Get("viewPanel"); viewPanel != "" {
		parts.panelID, _ = strconv.Atoi(viewPanel)
	}
	query := parsed.Query()
	if from := query.Get("from"); from != "" {
		parts.timeRange.From = from
		parts.timeRangeSet = true
	}
	if to := query.Get("to"); to != "" {
		parts.timeRange.To = to
		parts.timeRangeSet = true
	}
	parts.orgID = query.Get("orgId")
	for name, values := range query {
		if strings.HasPrefix(name, "var-") && len(values) > 0 {
			variable := strings.TrimPrefix(name, "var-")
			if variable != "" {
				parts.variables[variable] = strings.Join(values, ",")
			}
		}
	}
	parsed.Path = parsed.Path[:dashboardPathPattern.FindStringIndex(parsed.Path)[0]]
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parts.baseURL = parsed.String()

	return parts
}

// initStderr redirects stderr to a log file so that log output does not
// interfere with the terminal UI. The returned function restores the
// original stderr and should be called via defer.
func initStderr() func() {
	cleanup := func() {}

	oldStderr := os.Stderr

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		slog.Error("cannot find user cache directory", "error", err)
		return cleanup
	}
	dataPath := filepath.Join(cacheDir, "g5a")
	if err := os.MkdirAll(dataPath, 0o700); err != nil {
		slog.Error("error creating log directory", "error", err)
		return cleanup
	}

	logFile, err := os.OpenFile(
		filepath.Join(dataPath, "g5a.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		slog.Error("error opening log file, logs will be written to stderr (can result in glitches)", "error", err)
		return cleanup
	}

	os.Stderr = logFile
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{ //nolint:exhaustruct // only Level needed
		Level: slog.LevelInfo,
	})))

	cleanup = func() {
		os.Stderr = oldStderr
		slog.SetDefault(slog.New(slog.NewTextHandler(oldStderr, nil)))

		if err := logFile.Close(); err != nil {
			slog.Error("error closing log file", "error", err)
		}
	}

	return cleanup
}
