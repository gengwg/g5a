package grafana_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

	client, err := grafana.NewClient(server.URL,
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
