package accounts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
)

type verificationStoreFixture struct {
	Store
	digest    string
	activated bool
	put       bool
}

type operatorStoreFixture struct {
	Store
	initialized bool
}

func (s *operatorStoreFixture) InitializeOperator(_ context.Context, id string, hash []byte) (User, bool, error) {
	s.initialized = true
	return User{ID: id, Email: InitialOperatorEmail, Role: ports.RoleOperator}, true, nil
}

type passwordHasherFixture struct{}

func (passwordHasherFixture) Hash(raw []byte) ([]byte, error) { return []byte("fixture-hash"), nil }

type loginStoreFixture struct {
	Store
	credentials *LoginCredentials
}

func (s *loginStoreFixture) Login(_ context.Context, credentials LoginCredentials) (User, error) {
	s.credentials = &credentials
	return User{ID: "fixture-user", Email: credentials.Email}, nil
}

func TestBootstrapAliasUsesPasswordFlowAndKeepsEmailLogin(t *testing.T) {
	for _, test := range []struct {
		identity string
		password string
		code     string
		email    string
	}{
		{"Daydream", "Daydream123456", "", InitialOperatorEmail},
		{"  dayDREAM  ", "changed-password-123", "", InitialOperatorEmail},
		{"User@Example.test", "existing-password", "", "user@example.test"},
		{"User@Example.test", "", "123456", "user@example.test"},
		{"Daydream", "", "123456", ""},
		{"other-display-name", "existing-password", "", ""},
	} {
		t.Run(test.identity+test.code, func(t *testing.T) {
			store := &loginStoreFixture{}
			service := &Service{Store: store}
			result, err := service.Login(context.Background(), test.identity, test.password, test.code, "workspace")
			if test.email == "" {
				if err == nil || store.credentials != nil {
					t.Fatal("non-email code login or display-name login reached the store")
				}
				return
			}
			if err != nil || result.User.Email != test.email || store.credentials.Email != test.email || store.credentials.Code != test.code || string(store.credentials.Password) != test.password {
				t.Fatalf("login identity or credential changed: result=%+v error=%v", result.User, err)
			}
		})
	}
}

func TestControlledInitializationAcceptsSuppliedSecretWithoutRelaxingPublicPasswords(t *testing.T) {
	store := &operatorStoreFixture{}
	service := &Service{Store: store, Hasher: passwordHasherFixture{}}
	ctx := context.Background()
	short := strings.Repeat("x", 6)
	user, created, err := service.InitializeOperator(ctx, short)
	if err != nil || !created || !store.initialized || user.Role != ports.RoleOperator {
		t.Fatalf("controlled initialization failed: created=%t err=%v", created, err)
	}
	if _, err = service.Register(ctx, "new@example.test", "测试用户", short, "unused", "unused"); err == nil {
		t.Fatal("public registration accepted a short password")
	}
	if err = service.ResetPassword(ctx, "new@example.test", "unused", short); err == nil {
		t.Fatal("public reset accepted a short password")
	}
	for _, invalid := range []string{"", strings.Repeat("x", 5), strings.Repeat("x", 73)} {
		store.initialized = false
		if _, _, err = service.InitializeOperator(ctx, invalid); err == nil || store.initialized {
			t.Fatal("invalid initialization secret reached the store")
		}
	}
}

func (s *verificationStoreFixture) PutVerification(_ context.Context, id, email, purpose, digest string, until time.Time) error {
	s.digest = digest
	s.put = true
	return nil
}
func (s *verificationStoreFixture) ActivateVerification(_ context.Context, id string, active bool) error {
	s.activated = active
	return nil
}

type mailerFixture struct {
	fail bool
	code string
}

func (m *mailerFixture) SendCode(_ context.Context, email, purpose, code string) error {
	m.code = code
	if m.fail {
		return errors.New("fixture mail failure")
	}
	return nil
}
func TestMailFailureDoesNotActivateCodeAndMissingResourceIsExplicit(t *testing.T) {
	store := &verificationStoreFixture{}
	service := &Service{Store: store}
	if e := service.SendCode(context.Background(), "a@example.test", "register"); e == nil || store.put {
		t.Fatal("missing mailer was successful")
	}
	mailer := &mailerFixture{fail: true}
	service.Mailer = mailer
	if e := service.SendCode(context.Background(), "a@example.test", "register"); e == nil || store.activated {
		t.Fatal("failed mailer activated code")
	}
	mailer.fail = false
	if e := service.SendCode(context.Background(), "a@example.test", "register"); e != nil || !store.activated {
		t.Fatal(e)
	}
	if len(mailer.code) != 6 || store.digest == mailer.code || strings.Contains(store.digest, mailer.code) {
		t.Fatal("plain code stored")
	}
}
