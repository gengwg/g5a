//nolint:exhaustruct,testpackage // synthetic requests and direct access to the default policy
package grafana

import (
	"net/http"
	"net/url"
	"testing"
)

func TestResolveURLRejectsNonRelativeReferences(t *testing.T) {
	t.Parallel()

	base := &url.URL{Scheme: "https", Host: "grafana.example.com", Path: "/grafana"}
	for _, path := range []string{
		"https://evil.example.com/steal",
		"//evil.example.com/steal",
		"//user:secret@evil.example.com/steal",
		"://invalid",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if _, err := resolveURL(base, path); err == nil {
				t.Errorf("resolveURL(%q) succeeded, want error", path)
			}
		})
	}
}

func TestCheckSameHostRedirectRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://grafana.example.com")
	if err != nil {
		t.Fatal(err)
	}
	original := &http.Request{URL: &url.URL{Scheme: "http", Host: "grafana.example.com"}}
	target := &http.Request{URL: &url.URL{Scheme: "ftp", Host: "grafana.example.com"}}
	if err := client.httpClient.CheckRedirect(target, []*http.Request{original}); err == nil {
		t.Fatal("unsupported redirect scheme succeeded")
	}
}

func TestCheckSameHostRedirect(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://grafana.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if client.httpClient.CheckRedirect == nil {
		t.Fatal("default HTTP client has no redirect policy")
	}

	original := &http.Request{URL: &url.URL{Scheme: "https", Host: "grafana.example.com"}}
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "same host", target: "https://grafana.example.com/api/search/", wantErr: false},
		{name: "default HTTPS port", target: "https://grafana.example.com:443/api/search/", wantErr: false},
		{name: "different port", target: "https://grafana.example.com:8443/steal", wantErr: true},
		{name: "subdomain", target: "https://evil.grafana.example.com/steal", wantErr: true},
		{name: "different host", target: "https://evil.example.com/steal", wantErr: true},
		{name: "HTTPS downgrade", target: "http://grafana.example.com/api/search", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target, err := url.Parse(test.target)
			if err != nil {
				t.Fatal(err)
			}
			err = client.httpClient.CheckRedirect(&http.Request{URL: target}, []*http.Request{original})
			if (err != nil) != test.wantErr {
				t.Errorf("redirect error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
