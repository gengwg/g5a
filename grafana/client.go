package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultScrapeIntervalMs  = 15000
	datasourceTypePrometheus = "prometheus"
	maxResponseBytes         = 32 << 20
	datasourceFailureBackoff = 30 * time.Second
	maxRedirectCount         = 10
	schemeHTTP               = "http"
	schemeHTTPS              = "https"
)

var (
	errTooManyRedirects    = errors.New("stopped after 10 redirects")
	errCrossOriginRedirect = errors.New("refusing redirect to a different origin")
	errHTTPSDowngrade      = errors.New("refusing redirect from https to plaintext")
	errResponseTooLarge    = errors.New("grafana response exceeds size limit")
	errInvalidAPIPath      = errors.New("API path must be relative to the Grafana host")
)

// Client is an HTTP client for the Grafana API.
type Client struct {
	baseURL    *url.URL
	token      string
	username   string
	password   string
	orgID      string
	httpClient *http.Client

	// dsCache maps datasource name → UID, lazily populated.
	dsCache map[string]string
	// dsTypeCache maps datasource type → first UID of that type.
	dsTypeCache     map[string]string
	scrapeCache     map[string]int
	dsResolved      bool
	dsFailureCount  int
	dsRetryAfter    time.Time
	dsFetchInFlight bool
	dsFetchDone     chan struct{}
	dsMutex         sync.Mutex

	namespaceCache    string
	namespaceResolved bool
	namespaceMutex    sync.Mutex
}

// ClientOption configures a [Client].
type ClientOption func(*Client)

// WithToken sets a bearer token for authentication.
func WithToken(token string) ClientOption {
	return func(c *Client) { c.token = token }
}

// WithBasicAuth sets basic auth credentials for authentication.
func WithBasicAuth(username, password string) ClientOption {
	return func(c *Client) {
		c.username = username
		c.password = password
	}
}

// WithOrgID selects the Grafana organization for API requests.
func WithOrgID(orgID string) ClientOption {
	return func(c *Client) { c.orgID = orgID }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		cloned := *httpClient
		customRedirect := cloned.CheckRedirect
		cloned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if err := checkSameHostRedirect(request, via); err != nil {
				return err
			}
			if customRedirect != nil {
				return customRedirect(request, via)
			}

			return nil
		}
		c.httpClient = &cloned
	}
}

// NewClient creates a new Grafana API client.
func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing grafana URL: %w", err)
	}

	client := &Client{ //nolint:exhaustruct // mutex and optional caches use zero values
		baseURL:  parsedURL,
		token:    "",
		username: "",
		password: "",
		orgID:    "",
		httpClient: &http.Client{ //nolint:exhaustruct // default transport settings
			Timeout:       60 * time.Second,
			CheckRedirect: checkSameHostRedirect,
		},
		dsCache:         nil,
		dsTypeCache:     nil,
		scrapeCache:     nil,
		dsResolved:      false,
		dsFailureCount:  0,
		dsRetryAfter:    time.Time{},
		dsFetchInFlight: false,
		dsFetchDone:     nil,
	}
	if parsedURL.User != nil {
		client.username = parsedURL.User.Username()
		client.password, _ = parsedURL.User.Password()
		parsedURL.User = nil
	}
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

func checkSameHostRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirectCount {
		return errTooManyRedirects
	}
	original := via[0].URL
	if request.URL.Scheme != schemeHTTP && request.URL.Scheme != schemeHTTPS {
		return fmt.Errorf("%w: %s", errCrossOriginRedirect, request.URL.Scheme)
	}
	if !strings.EqualFold(request.URL.Hostname(), original.Hostname()) ||
		effectivePort(request.URL) != effectivePort(original) {
		return fmt.Errorf("%w: %s", errCrossOriginRedirect, request.URL.Host)
	}
	if original.Scheme == schemeHTTPS && request.URL.Scheme != original.Scheme {
		return errHTTPSDowngrade
	}

	return nil
}

func effectivePort(parsedURL *url.URL) string {
	if port := parsedURL.Port(); port != "" {
		return port
	}
	switch parsedURL.Scheme {
	case schemeHTTP:
		return "80"
	case schemeHTTPS:
		return "443"
	default:
		return ""
	}
}

// BaseURL returns the Grafana instance base URL as a string.
func (c *Client) BaseURL() string {
	return c.baseURL.String()
}

// Ping verifies that the Grafana instance is reachable and the
// credentials are valid. It returns an error if the connection fails.
func (c *Client) Ping(ctx context.Context) error {
	var results []json.RawMessage

	err := c.doJSON(
		ctx,
		"/api/search?limit=1",
		&results,
	)
	if err != nil {
		return fmt.Errorf("pinging grafana: %w", err)
	}

	return nil
}

// doJSON executes an HTTP GET request and decodes the JSON response
// into dst.
func (c *Client) doJSON(
	ctx context.Context,
	path string,
	dst any,
) error {
	fullURL, err := resolveURL(c.baseURL, path)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, fullURL, nil,
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setAuthHeader(request)

	slog.Debug("grafana request", "method", http.MethodGet, "path", request.URL.Path)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return sanitizedTransportError(err)
	}

	responseBody, readErr := readResponseBody(response.Body)
	_ = response.Body.Close()

	slog.Debug("grafana response",
		"path", request.URL.Path,
		"status", response.StatusCode,
		"bytes", len(responseBody),
	)

	if response.StatusCode != http.StatusOK {
		return &apiError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
		}
	}
	if readErr != nil {
		return readErr
	}

	if err := json.Unmarshal(responseBody, dst); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// doPostJSON executes an HTTP POST request with a JSON body and
// decodes the JSON response into dst.
func (c *Client) doPostJSON(
	ctx context.Context,
	path string,
	body any,
	dst any,
) error {
	fullURL, err := resolveURL(c.baseURL, path)
	if err != nil {
		return err
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, fullURL, bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(request)

	slog.Debug("grafana request", "method", http.MethodPost, "path", request.URL.Path)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return sanitizedTransportError(err)
	}

	responseBody, readErr := readResponseBody(response.Body)
	_ = response.Body.Close()

	slog.Debug("grafana response",
		"path", request.URL.Path,
		"status", response.StatusCode,
		"bytes", len(responseBody),
	)

	if response.StatusCode != http.StatusOK {
		return &apiError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
		}
	}
	if readErr != nil {
		return readErr
	}

	if err := json.Unmarshal(responseBody, dst); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

func readResponseBody(body io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf("%w: %d bytes", errResponseTooLarge, maxResponseBytes)
	}

	return payload, nil
}

// resolveURL joins the base URL with a path that may contain
// query parameters.
func resolveURL(base *url.URL, path string) (string, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return "", errInvalidAPIPath
	}
	if ref.IsAbs() || ref.Host != "" || ref.User != nil {
		return "", errInvalidAPIPath
	}

	ref.Path = strings.TrimRight(base.Path, "/") + ref.Path

	return base.ResolveReference(ref).String(), nil
}

// setAuthHeader sets the appropriate auth header on the request.
func (c *Client) setAuthHeader(request *http.Request) {
	switch {
	case c.token != "":
		request.Header.Set("Authorization", "Bearer "+c.token)
	case c.username != "":
		request.SetBasicAuth(c.username, c.password)
	}
	if c.orgID != "" {
		request.Header.Set("X-Grafana-Org-Id", c.orgID)
	}
}

// datasourceInfo represents a datasource from the Grafana API.
type datasourceInfo struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (c *Client) datasourceScrapeInterval(ctx context.Context, uid string) int {
	c.dsMutex.Lock()
	interval, ok := c.scrapeCache[uid]
	c.dsMutex.Unlock()
	if ok {
		return interval
	}

	var datasources []struct {
		UID      string `json:"uid"`
		JSONData struct {
			TimeInterval string `json:"timeInterval"`
		} `json:"jsonData"`
	}
	if err := c.doJSON(ctx, "/api/datasources", &datasources); err != nil {
		return defaultScrapeIntervalMs
	}

	interval = defaultScrapeIntervalMs
	for _, datasource := range datasources {
		if datasource.UID != uid {
			continue
		}
		interval = parseDurationMs(datasource.JSONData.TimeInterval)
		if interval <= 0 {
			interval = defaultScrapeIntervalMs
		}
		break
	}

	c.dsMutex.Lock()
	defer c.dsMutex.Unlock()
	if cached, exists := c.scrapeCache[uid]; exists {
		return cached
	}
	if c.scrapeCache == nil {
		c.scrapeCache = make(map[string]int)
	}
	c.scrapeCache[uid] = interval

	return interval
}

func parseDurationMs(value string) int {
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}

	return int(duration / time.Millisecond)
}

// ResolveDatasourceUID looks up a datasource by name or type and
// returns its actual UID. The typeHint is used as a fallback when
// the nameOrUID is a variable reference like ${DS_PROMETHEUS}. If
// the name is already a valid UID or can't be resolved, it returns
// the original value unchanged.
func (c *Client) ResolveDatasourceUID(
	ctx context.Context, nameOrUID, typeHint string,
) string {
	for {
		c.dsMutex.Lock()
		if uid, ok := c.dsCache[nameOrUID]; ok {
			c.dsMutex.Unlock()

			return uid
		}
		if c.dsResolved || time.Now().Before(c.dsRetryAfter) {
			uid := c.resolveDatasourceUIDLocked(nameOrUID, typeHint)
			c.dsMutex.Unlock()

			return uid
		}
		if !c.dsFetchInFlight {
			c.dsFetchInFlight = true
			c.dsFetchDone = make(chan struct{})
			c.dsMutex.Unlock()

			done := c.dsFetchDone
			func() {
				defer func() {
					c.dsMutex.Lock()
					c.dsFetchInFlight = false
					close(done)
					c.dsMutex.Unlock()
				}()
				c.populateDatasourceCache(ctx)
			}()

			c.dsMutex.Lock()
			uid := c.resolveDatasourceUIDLocked(nameOrUID, typeHint)
			c.dsMutex.Unlock()

			return uid
		}
		done := c.dsFetchDone
		c.dsMutex.Unlock()

		select {
		case <-done:
		case <-ctx.Done():
			return nameOrUID
		}
	}
}

func (c *Client) resolveDatasourceUIDLocked(nameOrUID, typeHint string) string {
	if uid, ok := c.dsCache[nameOrUID]; ok {
		return uid
	}

	// For variable UIDs like ${DS_PROMETHEUS}, fall back to type.
	if typeHint != "" {
		if uid, ok := c.dsTypeCache[typeHint]; ok {
			slog.Debug("resolved datasource variable by type",
				"variable", nameOrUID, "type", typeHint,
				"uid", uid)
			return uid
		}
	}

	return nameOrUID
}

func (c *Client) populateDatasourceCache(ctx context.Context) {
	var datasources []datasourceInfo
	if err := c.doJSON(ctx, "/api/datasources", &datasources); err != nil {
		if ctx.Err() != nil {
			return
		}
		c.dsMutex.Lock()
		c.dsFailureCount++
		// Allow one immediate recovery attempt before throttling failures.
		if c.dsFailureCount > 1 {
			c.dsRetryAfter = time.Now().Add(datasourceFailureBackoff)
		}
		c.dsMutex.Unlock()
		slog.Info("failed to fetch datasources for name resolution",
			"error", err)
		return
	}

	c.dsMutex.Lock()
	defer c.dsMutex.Unlock()
	if c.dsResolved {
		return
	}
	c.dsFailureCount = 0
	c.dsRetryAfter = time.Time{}
	if c.dsCache == nil {
		c.dsCache = make(map[string]string)
		c.dsTypeCache = make(map[string]string)
	}
	c.dsResolved = true
	for _, ds := range datasources {
		c.dsCache[ds.Name] = ds.UID
		c.dsCache[ds.UID] = ds.UID

		// Store first datasource per type for variable resolution.
		if _, exists := c.dsTypeCache[ds.Type]; !exists {
			c.dsTypeCache[ds.Type] = ds.UID
		}
	}
}

// apiError represents an unexpected HTTP status from the Grafana API.
type apiError struct {
	StatusCode int
	Status     string
}

func (e *apiError) Error() string {
	return "grafana API error: " + e.Status
}

func sanitizedTransportError(err error) error {
	for {
		var urlError *url.Error
		if !errors.As(err, &urlError) {
			break
		}
		err = urlError.Err
	}

	return fmt.Errorf("executing request: %w", err)
}
