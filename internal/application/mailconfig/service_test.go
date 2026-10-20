package mailconfig

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

type memoryConfig struct {
	mu      sync.Mutex
	value   Snapshot
	unknown bool
	readErr error
}

func (s *memoryConfig) MailConfigSnapshot(context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.readErr
}

type rejectingSender struct{ cause error }

func (s rejectingSender) DeliverCode(context.Context, Delivery, string, string, string) error {
	return s.cause
}

func TestSubmissionDiagnosticsPreserveCausesWithoutLoggingSecrets(t *testing.T) {
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	settings := Settings{Enabled: true, TLS: "tls", Address: "localhost:465", Username: "PRIVATE_USERNAME", From: "sender@example.test"}
	store := &memoryConfig{value: Snapshot{Settings: settings, PasswordCipher: []byte("fictional-seal:PRIVATE_PASSWORD")}}
	service := Service{Store: store, Cipher: fixtureCipher{}}
	databaseFailure := errors.New("PRIVATE_DB_DIAGNOSTIC")
	store.readErr = databaseFailure
	err := service.SendCode(context.Background(), "recipient@example.test", "login", "PRIVATE_CODE")
	if !errors.Is(err, databaseFailure) || !hasCode(err, "MAIL_UNAVAILABLE") || !strings.Contains(output.String(), "stage=configuration-read") {
		t.Fatal("configuration cause was lost", err, output.String())
	}
	store.readErr = nil
	peer := &textproto.Error{Code: 535, Msg: "PRIVATE_PASSWORD PRIVATE_CODE"}
	service.Sender = rejectingSender{cause: peer}
	err = service.SendCode(context.Background(), "recipient@example.test", "login", "PRIVATE_CODE")
	if !errors.Is(err, peer) || !hasCode(err, "MAIL_DELIVERY_FAILED") || !strings.Contains(output.String(), "stage=delivery") || !strings.Contains(output.String(), "smtp_code=535") {
		t.Fatal("delivery diagnosis was lost", err, output.String())
	}
	store.value.PasswordCipher = []byte("bad-cipher")
	err = service.SendCode(context.Background(), "recipient@example.test", "login", "PRIVATE_CODE")
	if !hasCode(err, "MAIL_UNAVAILABLE") || !strings.Contains(output.String(), "stage=decrypt") {
		t.Fatal(err, output.String())
	}
	if strings.Contains(output.String()+err.Error(), "PRIVATE_") || !strings.Contains(err.Error(), "诊断编号") {
		t.Fatal("private diagnostics escaped or correlation was missing")
	}
}
func (s *memoryConfig) CommitMailConfig(_ context.Context, revision uint64, next Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value.Revision != revision {
		return fault.New("MAIL_CONFIGURATION_CONFLICT", "conflict")
	}
	s.value = next
	if s.unknown {
		return fault.New("MAIL_CONFIG_SAVE_UNKNOWN", "unknown")
	}
	return nil
}

type fixtureCipher struct{}

func (fixtureCipher) Seal(value string) ([]byte, error) {
	return []byte("fictional-seal:" + value), nil
}
func (fixtureCipher) Open(value []byte) (string, error) {
	if !strings.HasPrefix(string(value), "fictional-seal:") {
		return "", errors.New("bad fixture cipher")
	}
	return strings.TrimPrefix(string(value), "fictional-seal:"), nil
}

type deliveryFixture struct {
	values []Delivery
	fail   bool
}

func (s *deliveryFixture) DeliverCode(_ context.Context, value Delivery, to, purpose, code string) error {
	s.values = append(s.values, value)
	if s.fail {
		return errors.New("PRIVATE_SMTP_ERROR_PASSWORD_AND_BODY")
	}
	return nil
}
func hasCode(err error, code string) bool {
	var f *fault.Error
	return errors.As(err, &f) && f.Code == code
}
func TestMailConfigCASSecretsAndLatestRuntime(t *testing.T) {
	ctx := context.Background()
	actor := ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}
	store := &memoryConfig{value: Snapshot{Settings: Settings{TLS: "starttls"}}}
	sender := &deliveryFixture{}
	s := Service{Store: store, Cipher: fixtureCipher{}, Sender: sender}
	input := Input{Settings: Settings{Enabled: true, Address: " localhost:587 ", Username: " sender ", From: "sender@example.test", TLS: "starttls"}, Password: "fixture-password"}
	first, err := s.Save(ctx, actor, input)
	if err != nil || first.Revision != 1 || !first.PasswordConfigured || first.Address != "localhost:587" {
		t.Fatal(first, err)
	}
	input.Revision = 1
	input.Password = ""
	input.Address = "localhost:465"
	input.TLS = "tls"
	if _, err = s.Save(ctx, actor, input); err != nil {
		t.Fatal(err)
	}
	if err = s.SendCode(ctx, "recipient@example.test", "login", "fictional-code"); err != nil {
		t.Fatal(err)
	}
	if sender.values[0].Address != "localhost:465" || sender.values[0].Password != "fixture-password" {
		t.Fatal("runtime did not use latest connection or retained password")
	}
	input.Revision = 2
	input.Password = "replacement"
	if _, err = s.Save(ctx, actor, input); err != nil {
		t.Fatal(err)
	}
	if err = s.SendCode(ctx, "recipient@example.test", "login", "fictional-code"); err != nil || sender.values[1].Password != "replacement" {
		t.Fatal("runtime retained old password", err)
	}
	if _, err = s.Save(ctx, actor, input); !hasCode(err, "MAIL_CONFIGURATION_CONFLICT") {
		t.Fatal("stale save", err)
	}
	input.Revision = 3
	input.Password = ""
	input.ClearPassword = true
	if _, err = s.Save(ctx, actor, input); !hasCode(err, "MAIL_CONFIGURATION_INVALID") {
		t.Fatal("enabled clear accepted", err)
	}
	input.Enabled = false
	if v, err := s.Save(ctx, actor, input); err != nil || v.PasswordConfigured {
		t.Fatal("clear", v, err)
	}
	if err = s.SendCode(ctx, "recipient@example.test", "login", "fictional-code"); !hasCode(err, "MAIL_UNAVAILABLE") || len(sender.values) != 2 {
		t.Fatal("disabled send", err)
	}
	// Unknown commit is never retried: the original save is visible to an explicit GET.
	store.unknown = true
	input.Revision = 4
	input.ClearPassword = false
	input.Password = "restored"
	input.Enabled = true
	if _, err = s.Save(ctx, actor, input); !hasCode(err, "MAIL_CONFIG_SAVE_UNKNOWN") {
		t.Fatal(err)
	}
	if v, err := s.Get(ctx, actor); err != nil || v.Revision != 5 || !v.PasswordConfigured {
		t.Fatal("lost commit readback", v, err)
	}
	sender.fail = true
	if err = s.SendCode(ctx, "recipient@example.test", "login", "fictional-code"); !hasCode(err, "MAIL_DELIVERY_FAILED") || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("peer diagnostics leaked", err)
	}
}
func TestMailConfigRoleValidationConcurrencyAndLegacyRecovery(t *testing.T) {
	ctx := context.Background()
	denied := Service{}
	for _, actor := range []ports.Actor{{}, {UserID: "ordinary", Role: ports.RoleUser}} {
		if _, err := denied.Get(ctx, actor); err == nil {
			t.Fatal("untrusted read")
		}
		if _, err := denied.Save(ctx, actor, Input{}); err == nil {
			t.Fatal("untrusted save")
		}
	}
	for _, value := range []Settings{{TLS: "none"}, {TLS: "tls", Address: "host:0"}, {TLS: "tls", Address: "host:65536"}, {TLS: "tls", From: "name <sender@example.test>"}, {TLS: "tls", Username: "bad\nAUTH"}} {
		if _, err := Validate(value, false); err == nil {
			t.Fatal("invalid settings accepted")
		}
	}
	store := &memoryConfig{value: Snapshot{Settings: Settings{TLS: "tls"}}}
	s := Service{Store: store, Cipher: fixtureCipher{}}
	settings := Settings{Enabled: true, TLS: "tls", Address: "localhost:465", Username: "fixture", From: "sender@example.test"}
	for i := 0; i < 2; i++ {
		v, err := s.ImportLegacy(ctx, settings, "legacy-fixture")
		if err != nil || v.Revision != 1 {
			t.Fatal("legacy recovery", v, err)
		}
	}
	if _, err := s.ImportLegacy(ctx, settings, "different-fixture"); !hasCode(err, "MAIL_CONFIGURATION_CONFLICT") {
		t.Fatal("legacy overwrite", err)
	}
	actor := ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Save(ctx, actor, Input{Revision: 1, Settings: settings})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !hasCode(err, "MAIL_CONFIGURATION_CONFLICT") {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("CAS winners", success)
	}
}
