package grafana_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gengwg/g5a/grafana"
)

func TestNewClient_InvalidURL(t *testing.T) {
	t.Parallel()

	_, err := grafana.NewClient("://invalid")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestClient_BearerTokenAuth(t *testing.T) {
	t.Parallel()

	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		},
	))
	defer server.Close()

	rawURL := strings.Replace(server.URL, "://", "://ignored:ignored@", 1)
	client, err := grafana.NewClient(rawURL,
		grafana.WithToken("my-secret-token"),
		grafana.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = client.SearchDashboards(t.Context(), "")

	expected := "Bearer my-secret-token"
	if gotAuth != expected {
		t.Errorf("auth header = %q, want %q", gotAuth, expected)
	}
}

func TestClient_OrgIDHeader(t *testing.T) {
	t.Parallel()

	var gotOrgID string
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotOrgID = r.Header.Get("X-Grafana-Org-Id")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL,
		grafana.WithOrgID("2"),
		grafana.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = client.SearchDashboards(t.Context(), "")
	if gotOrgID != "2" {
		t.Errorf("org header = %q, want 2", gotOrgID)
	}
}

func TestClient_BasicAuth(t *testing.T) {
	t.Parallel()

	var gotUsername, gotPassword string
	var gotOK bool

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotUsername, gotPassword, gotOK = r.BasicAuth()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL,
		grafana.WithBasicAuth("admin", "password123"),
		grafana.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = client.SearchDashboards(t.Context(), "")

	if !gotOK {
		t.Fatal("expected basic auth to be set")
	}

	if gotUsername != "admin" {
		t.Errorf("username = %q, want %q", gotUsername, "admin")
	}

	if gotPassword != "password123" {
		t.Errorf("password = %q, want %q", gotPassword, "password123")
	}
}

func TestClient_URLConstruction(t *testing.T) {
	t.Parallel()

	var gotPath string

	dashboardJSON := `{"dashboard":{"title":"test","panels":[],` +
		`"templating":{"list":[]},"time":{"from":"now-1h","to":"now"}}}`

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(dashboardJSON))
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL+"/grafana",
		grafana.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _ = client.GetDashboard(t.Context(), "abc123")

	expected := "/grafana/api/dashboards/uid/abc123"
	if gotPath != expected {
		t.Errorf("path = %q, want %q", gotPath, expected)
	}
}

//nolint:paralleltest // replaces the process-wide logger to assert redaction
func TestClient_APIErrorDoesNotExposeResponseBody(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"private datasource URL"}`))
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if err == nil {
		t.Fatal("expected an API error")
	}
	if strings.Contains(err.Error(), "private datasource URL") {
		t.Fatalf("error exposed response body: %v", err)
	}
	if strings.Contains(logs.String(), "private datasource URL") {
		t.Fatalf("logs exposed response body: %s", logs.String())
	}
}

func TestClient_APIErrorTakesPrecedenceOverBodyReadError(t *testing.T) {
	t.Parallel()

	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{ //nolint:exhaustruct // synthetic test response
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Body:       io.NopCloser(&errorReader{err: errors.New("connection reset")}),
			Request:    request,
		}, nil
	})
	client, err := grafana.NewClient(
		"https://grafana.example.com",
		grafana.WithHTTPClient(&http.Client{Transport: transport}), //nolint:exhaustruct // test client
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("error = %v, want HTTP status", err)
	}
	if strings.Contains(err.Error(), "reading response") {
		t.Fatalf("body read error hid HTTP status: %v", err)
	}
}

func TestClient_TransportErrorDoesNotExposeUserinfo(t *testing.T) {
	t.Parallel()

	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{
			Op:  "Get",
			URL: request.URL.String(),
			Err: errors.New("dial failed"),
		}
	})
	client, err := grafana.NewClient(
		"http://user:secret@example.com",
		grafana.WithHTTPClient(&http.Client{Transport: transport}), //nolint:exhaustruct // test client
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "user") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error exposed URL userinfo: %v", err)
	}
}

func TestClient_URLUserinfoIsUsedForAuthButNotDisplayed(t *testing.T) {
	t.Parallel()

	var gotUsername, gotPassword string
	var gotOK bool
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			gotUsername, gotPassword, gotOK = r.BasicAuth()
			_, _ = w.Write([]byte(`[]`))
		},
	))
	defer server.Close()

	rawURL := strings.Replace(server.URL, "://", "://svc:s3cret@", 1)
	client, err := grafana.NewClient(rawURL, grafana.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if got := client.BaseURL(); got != server.URL {
		t.Errorf("base URL = %q, want %q", got, server.URL)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !gotOK || gotUsername != "svc" || gotPassword != "s3cret" {
		t.Errorf("basic auth = %q/%q, %v", gotUsername, gotPassword, gotOK)
	}
}

func TestClient_ResponseBodyReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset")
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{ //nolint:exhaustruct // synthetic test response
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(&errorReader{err: readErr}),
			Request:    request,
		}, nil
	})
	client, err := grafana.NewClient(
		"https://grafana.example.com",
		grafana.WithHTTPClient(&http.Client{Transport: transport}), //nolint:exhaustruct // test client
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if !errors.Is(err, readErr) {
		t.Fatalf("error = %v, want wrapped read error", err)
	}
}

func TestClient_ResponseBodyTooLarge(t *testing.T) {
	t.Parallel()

	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{ //nolint:exhaustruct // synthetic test response
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(io.LimitReader(zeroReader{}, 32<<20+1)),
			Request:    request,
		}, nil
	})
	client, err := grafana.NewClient(
		"https://grafana.example.com",
		grafana.WithHTTPClient(&http.Client{Transport: transport}), //nolint:exhaustruct // test client
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want response size error", err)
	}
}

func TestClient_RejectsCrossOriginRedirect(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		sourceHost   string
		customClient bool
	}{
		{name: "different hostname", sourceHost: "localhost", customClient: false},
		{name: "same hostname different port", sourceHost: "127.0.0.1", customClient: false},
		{name: "custom client different hostname", sourceHost: "localhost", customClient: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var destinationRequests atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					destinationRequests.Add(1)
					_, _ = w.Write([]byte(`[]`))
				},
			))
			defer destination.Close()
			source := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, destination.URL+"/steal", http.StatusFound)
				},
			))
			defer source.Close()

			sourceURL := strings.Replace(source.URL, "127.0.0.1", test.sourceHost, 1)
			opts := []grafana.ClientOption{grafana.WithToken("secret-token")}
			if test.customClient {
				opts = append(opts, grafana.WithHTTPClient(source.Client()))
			}
			client, err := grafana.NewClient(sourceURL, opts...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.SearchDashboards(t.Context(), "")
			if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
				t.Fatalf("error = %v, want redirect rejection", err)
			}
			if got := destinationRequests.Load(); got != 0 {
				t.Errorf("redirect target received %d requests, want 0", got)
			}
		})
	}
}

func TestClient_AllowsSameHostRedirect(t *testing.T) {
	t.Parallel()

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/search":
				http.Redirect(w, r, "/api/search/", http.StatusFound)
			case "/api/search/":
				gotAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(`[]`))
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer server.Close()

	client, err := grafana.NewClient(server.URL, grafana.WithToken("secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchDashboards(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("auth header = %q", gotAuth)
	}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(buffer []byte) (int, error) {
	copy(buffer, "[]")

	return 2, r.err
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)

	return len(buffer), nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
