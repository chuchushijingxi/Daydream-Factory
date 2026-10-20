package accounts

import (
	"testing"
	"time"
)

func TestRegistrationReservedIdentityAndAuthorizationLifetime(t *testing.T) {
	if AllowPublicRegistration(InitialOperatorEmail) == nil || AllowPublicRegistration("user@example.test") != nil {
		t.Fatal("public registration changed reserved identity behavior")
	}
	now := time.Now()
	code := AuthorizationCode{ExpiresAt: now.Add(time.Hour)}
	if err := code.RequireAvailable(now); err != nil {
		t.Fatal(err)
	}
	user := "existing-user"
	for _, unavailable := range []AuthorizationCode{
		{ExpiresAt: now},
		{ExpiresAt: now.Add(-time.Second)},
		{ExpiresAt: code.ExpiresAt, Disabled: true},
		{ExpiresAt: code.ExpiresAt, UsedBy: &user},
	} {
		if unavailable.RequireAvailable(now) == nil {
			t.Fatal("invalid authorization accepted", unavailable)
		}
	}
}
