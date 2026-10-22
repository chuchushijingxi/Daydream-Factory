package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestRequestSpaceSelector(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		headers     []string
		want        string
		invalid     bool
	}{
		{name: "default personal", want: "user_a"},
		{name: "header", headers: []string{"team_a"}, want: "team_a"},
		{name: "native URL", query: "?space=team_a", want: "team_a"},
		{name: "matching", query: "?space=team_a", headers: []string{"team_a"}, want: "team_a"},
		{name: "mismatch", query: "?space=team_a", headers: []string{"team_b"}, invalid: true},
		{name: "duplicate header", headers: []string{"team_a", "team_a"}, invalid: true},
		{name: "duplicate query", query: "?space=team_a&space=team_b", invalid: true},
		{name: "empty", query: "?space=", invalid: true},
		{name: "control", query: "?space=a%00", invalid: true},
		{name: "space", query: "?space=a+b", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://localhost/api/projects"+tc.query, nil)
			if tc.headers != nil {
				r.Header["X-Drama-Space"] = tc.headers
			}
			got, err := requestSpace(r, "user_a")
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("got %q, %v; want %q invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}
