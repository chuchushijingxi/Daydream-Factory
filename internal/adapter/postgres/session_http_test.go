package postgres_test

import (
	a "drama-agent/internal/application/accounts"
	"net/http"
)

func addSessionCookie(r *http.Request, token string) {
	_, audience, _, err := a.SessionParts(token)
	if err != nil {
		panic("invalid isolated test session")
	}
	name := "drama_workspace_session"
	if audience == a.Operations {
		name = "drama_operations_session"
	}
	r.Header.Set("X-Drama-Audience", audience)
	r.AddCookie(&http.Cookie{Name: name, Value: token})
}
