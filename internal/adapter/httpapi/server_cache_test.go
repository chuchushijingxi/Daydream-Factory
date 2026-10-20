package httpapi

import (
	"net/http"
	"testing"
)

func TestCacheControl(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{"hashed css", http.MethodGet, "/demo/app/assets/app-abc123.css", immutableAssets},
		{"hashed font", http.MethodGet, "/demo/app/assets/MiSansVF-abc123.ttf", immutableAssets},
		{"hashed js via head", http.MethodHead, "/demo/app/assets/vendor-abc123.js", immutableAssets},
		{"entry html", http.MethodGet, "/demo/app/", "no-store"},
		{"index html", http.MethodGet, "/demo/app/index.html", "no-store"},
		{"unhashed demo asset", http.MethodGet, "/demo/assets/styles/catalog.json", "no-store"},
		{"api", http.MethodGet, "/api/projects", "no-store"},
		{"post to hashed asset", http.MethodPost, "/demo/app/assets/app-abc123.css", "no-store"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cacheControl(tc.method, tc.path); got != tc.want {
				t.Fatalf("cacheControl(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
			}
		})
	}
}
