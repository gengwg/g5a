//nolint:cyclop,exhaustruct,testpackage // direct cache state and concurrency assertions need synthetic responses
package grafana

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveDatasourceUIDCachesEmptyList(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if got := client.ResolveDatasourceUID(t.Context(), "missing", ""); got != "missing" {
			t.Errorf("resolved UID = %q, want missing", got)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("datasource requests = %d, want 1", got)
	}
}

func TestResolveDatasourceUIDCoalescesConcurrentFetch(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		close(started)
		<-release
		_, _ = w.Write([]byte(`[{"uid":"new-uid","name":"shared","type":"prometheus"}]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan string, 1)
	go func() { firstDone <- client.ResolveDatasourceUID(t.Context(), "shared", "") }()
	waitForSignal(t, started, "first datasource lookup")
	secondDone := make(chan string, 1)
	go func() { secondDone <- client.ResolveDatasourceUID(t.Context(), "shared", "") }()
	select {
	case got := <-secondDone:
		t.Fatalf("second lookup completed during first request with %q", got)
	case <-time.After(50 * time.Millisecond):
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("concurrent datasource requests = %d, want 1", got)
	}

	unblock()
	for range 2 {
		select {
		case got := <-firstDone:
			if got != "new-uid" {
				t.Errorf("first lookup = %q, want new-uid", got)
			}
		case got := <-secondDone:
			if got != "new-uid" {
				t.Errorf("second lookup = %q, want new-uid", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("datasource lookup did not finish")
		}
	}
}

func TestResolveDatasourceUIDSharedFailureAllowsOneRetry(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan string, 1)
	go func() { firstDone <- client.ResolveDatasourceUID(t.Context(), "shared", "") }()
	waitForSignal(t, started, "first datasource lookup")
	secondDone := make(chan string, 1)
	go func() { secondDone <- client.ResolveDatasourceUID(t.Context(), "shared", "") }()
	select {
	case got := <-secondDone:
		t.Fatalf("second lookup completed during first request with %q", got)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for range 2 {
		select {
		case got := <-firstDone:
			if got != "shared" {
				t.Errorf("first lookup = %q, want shared", got)
			}
		case got := <-secondDone:
			if got != "shared" {
				t.Errorf("second lookup = %q, want shared", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("datasource lookup did not finish")
		}
	}
	if got := client.ResolveDatasourceUID(t.Context(), "shared", ""); got != "shared" {
		t.Errorf("lookup during backoff = %q, want shared", got)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("datasource requests = %d, want one fetch and one recovery attempt", got)
	}
}

func TestResolveDatasourceUIDCancellationDoesNotBackoff(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`[{"uid":"new-uid","name":"shared","type":"prometheus"}]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client.populateDatasourceCache(ctx)
	if client.dsFailureCount != 0 || !client.dsRetryAfter.IsZero() {
		t.Fatal("caller cancellation was recorded as a backend failure")
	}
	if got := client.ResolveDatasourceUID(t.Context(), "shared", ""); got != "new-uid" {
		t.Errorf("lookup after cancellation = %q, want new-uid", got)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("datasource requests = %d, want 1", got)
	}
}

func TestResolveDatasourceUIDBacksOffRepeatedFailures(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`[{"uid":"new-uid","name":"shared","type":"prometheus"}]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if got := client.ResolveDatasourceUID(t.Context(), "shared", ""); got != "shared" {
			t.Errorf("failed lookup = %q, want shared", got)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests during backoff = %d, want 2", got)
	}

	client.dsRetryAfter = time.Now().Add(-time.Second)
	if got := client.ResolveDatasourceUID(t.Context(), "shared", ""); got != "new-uid" {
		t.Errorf("lookup after backoff = %q, want new-uid", got)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("requests after backoff = %d, want 3", got)
	}
}

func TestDatasourceScrapeIntervalCachesMissingUID(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if got := client.datasourceScrapeInterval(t.Context(), "missing"); got != defaultScrapeIntervalMs {
			t.Errorf("scrape interval = %d, want %d", got, defaultScrapeIntervalMs)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("datasource requests = %d, want 1", got)
	}
}

func TestDatasourceScrapeIntervalRetriesFailures(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`[{"uid":"target","jsonData":{"timeInterval":"30s"}}]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	if got := client.datasourceScrapeInterval(t.Context(), "target"); got != defaultScrapeIntervalMs {
		t.Errorf("first scrape interval = %d, want default", got)
	}
	for range 2 {
		if got := client.datasourceScrapeInterval(t.Context(), "target"); got != 30000 {
			t.Errorf("recovered scrape interval = %d, want 30000", got)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("datasource requests = %d, want 2", got)
	}
}

func TestDatasourceCacheLookupDoesNotWaitForHTTP(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	client.dsCache = map[string]string{"cached": "cached-uid"}
	client.dsTypeCache = map[string]string{"prometheus": "cached-uid"}
	client.dsResolved = true

	scrapeDone := make(chan int, 1)
	go func() { scrapeDone <- client.datasourceScrapeInterval(t.Context(), "missing") }()
	waitForSignal(t, started, "datasource request")
	lookupDone := make(chan string, 1)
	go func() { lookupDone <- client.ResolveDatasourceUID(t.Context(), "cached", "") }()
	select {
	case uid := <-lookupDone:
		if uid != "cached-uid" {
			t.Errorf("resolved UID = %q, want cached-uid", uid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cached datasource lookup blocked behind HTTP")
	}

	unblock()
	select {
	case <-scrapeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("datasource scrape lookup did not finish")
	}
}

func TestDashboardNamespaceDoesNotWaitForHTTP(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var requests atomic.Int32
	transport := cacheRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		namespace := "stacks-second"
		if requests.Add(1) == 1 {
			close(started)
			<-release
			namespace = "stacks-first"
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"namespace":"` + namespace + `"}`)),
			Request:    request,
		}, nil
	})
	client, err := NewClient(
		"https://example.grafana.net",
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatal(err)
	}

	type namespaceResult struct {
		namespace string
		err       error
	}
	firstDone := make(chan namespaceResult, 1)
	go func() {
		namespace, err := client.dashboardNamespace(t.Context())
		firstDone <- namespaceResult{namespace: namespace, err: err}
	}()
	waitForSignal(t, started, "namespace request")
	secondDone := make(chan namespaceResult, 1)
	go func() {
		namespace, err := client.dashboardNamespace(t.Context())
		secondDone <- namespaceResult{namespace: namespace, err: err}
	}()
	select {
	case result := <-secondDone:
		if result.err != nil || result.namespace != "stacks-second" {
			t.Errorf("second namespace = %q, %v", result.namespace, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("namespace lookup blocked behind HTTP")
	}

	unblock()
	select {
	case result := <-firstDone:
		if result.err != nil || result.namespace != "stacks-second" {
			t.Errorf("first namespace = %q, %v", result.namespace, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first namespace lookup did not finish")
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not start", name)
	}
}

type cacheRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f cacheRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
