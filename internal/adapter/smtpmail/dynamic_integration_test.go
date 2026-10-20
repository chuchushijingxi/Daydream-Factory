package smtpmail

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/testsupport/pgfixture"
	"drama-agent/internal/testsupport/redisruntime"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/accounts"
	"drama-agent/internal/application/mailconfig"
	projectapp "drama-agent/internal/application/project"
	"drama-agent/internal/domain/identity"
	"github.com/jackc/pgx/v5"
)

type localMail struct{ username, password, body string }

func tlsMailSink(t *testing.T, mode string) (string, *x509.CertPool, <-chan localMail, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil && mode == "tls" {
		listener = tls.NewListener(listener, tlsConfig)
	}
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan localMail, 10)
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connection.Close()
				connection.SetDeadline(time.Now().Add(5 * time.Second))
				client := textproto.NewConn(connection)
				client.PrintfLine("220 fixture ESMTP")
				var message localMail
				encrypted := mode == "tls"
				for {
					line, err := client.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						client.PrintfLine("250-fixture")
						if mode == "starttls" && !encrypted {
							client.PrintfLine("250-STARTTLS")
						}
						client.PrintfLine("250 AUTH PLAIN")
					case line == "STARTTLS":
						if mode != "starttls" || encrypted {
							client.PrintfLine("454 unavailable")
							return
						}
						client.PrintfLine("220 upgrade")
						upgraded := tls.Server(connection, tlsConfig)
						if err := upgraded.Handshake(); err != nil {
							return
						}
						client = textproto.NewConn(upgraded)
						encrypted = true
					case strings.HasPrefix(line, "AUTH PLAIN "):
						if !encrypted {
							client.PrintfLine("538 TLS required")
							return
						}
						raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
						values := bytes.Split(raw, []byte{0})
						if len(values) != 3 {
							client.PrintfLine("535 rejected")
							return
						}
						message.username = string(values[1])
						message.password = string(values[2])
						client.PrintfLine("235 authenticated")
					case strings.HasPrefix(line, "MAIL FROM"):
						client.PrintfLine("250 accepted")
					case strings.Contains(line, "reject@example.test"):
						client.PrintfLine("550 PRIVATE_SMTP_FAILURE")
					case strings.HasPrefix(line, "RCPT TO"):
						client.PrintfLine("250 accepted")
					case line == "DATA":
						client.PrintfLine("354 message")
						body, err := client.ReadDotBytes()
						if err != nil {
							return
						}
						message.body = string(body)
						received <- message
						client.PrintfLine("250 queued")
					default:
						client.PrintfLine("221 bye")
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done; wg.Wait() })
	return listener.Addr().String(), roots, received, cert
}
func TestDynamicSMTPRealPGHTTPAndTLS(t *testing.T) {
	path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
	if path == "" {
		t.Skip("dedicated PG required")
	}
	cfg, err := appconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(cfg.Database.URL)
	if !strings.HasPrefix(parsed.Path, "/drama_accounts_test_") {
		t.Fatal("dedicated accounts resources required")
	}
	ctx := context.Background()
	store, err := pgfixture.Open(t, ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	reset := func() {
		_, err := conn.Exec(ctx, `UPDATE drama_catalog_v1.mail_configuration SET revision=0,enabled=false,address='',username='',from_address='',tls='starttls',password_cipher=NULL WHERE id`)
		if err != nil {
			t.Error(err)
		}
	}
	reset()
	defer reset()
	cipher, err := postgres.OpenModelCipher(filepath.Join(t.TempDir(), "configuration.key"), true)
	if err != nil {
		t.Fatal(err)
	}
	address1, roots1, messages1, _ := tlsMailSink(t, "tls")
	address2, _, messages2, cert2 := tlsMailSink(t, "tls")
	roots1.AddCert(cert2)
	mailService := &mailconfig.Service{Store: store, Cipher: cipher, Sender: Transport{roots: roots1}}
	legacy := mailconfig.Settings{Enabled: true, Address: address1, Username: "legacy-fixture", From: "sender@example.test", TLS: "tls"}
	for i := 0; i < 2; i++ {
		value, err := mailService.ImportLegacy(ctx, legacy, "legacy-fixture-password")
		if err != nil || value.Revision != 1 {
			t.Fatal("real PG legacy import/recovery", err)
		}
	}
	if _, err = mailService.ImportLegacy(ctx, legacy, "conflicting-fixture-password"); err == nil {
		t.Fatal("legacy import overwrote PG credential")
	}
	reset()
	accountService := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}, Mailer: mailService}
	operator, _, err := accountService.InitializeOperator(ctx, "fixture-operator-password-123")
	if err != nil {
		t.Fatal(err)
	}
	login, err := accountService.Login(ctx, operator.Email, "fixture-operator-password-123", "", "operations")
	if err != nil {
		t.Fatal(err)
	}
	h, handler := httpapi.New(&projectapp.Service{}, nil, "../../../demo", 0)
	h.Accounts = accountService
	h.Authenticator = accountService
	h.MailConfig = mailService
	defer h.Shutdown(ctx)
	server := httptest.NewServer(handler)
	defer server.Close()
	request := func(method, path, token string, input any) (int, []byte) {
		t.Helper()
		raw, _ := json.Marshal(input)
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			_, audience, _, e := accounts.SessionParts(token)
			if e != nil {
				t.Fatal(e)
			}
			name := "drama_workspace_session"
			if audience == "operations" {
				name = "drama_operations_session"
			}
			req.Header.Set("X-Drama-Audience", audience)
			req.AddCookie(&http.Cookie{Name: name, Value: token})
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value json.RawMessage
		if err = json.NewDecoder(resp.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, value
	}
	input := mailconfig.Input{Settings: mailconfig.Settings{Enabled: true, Address: address1, Username: "first-fixture", From: "sender@example.test", TLS: "tls"}, Password: "smtp-fixture-password"}
	if status, _ := request("GET", "/api/operations/smtp", "", nil); status != 401 {
		t.Fatal("anonymous SMTP config")
	}
	// A real ordinary session is built from the same PG session fact.
	ordinaryID := identity.New("smtp-user")
	_, err = conn.Exec(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,'fixture ordinary','user',$3)`, ordinaryID, ordinaryID+"@example.test", []byte("fixture-unused-hash"))
	if err != nil {
		t.Fatal(err)
	}
	redisruntime.SeedDigest(t, ordinaryID, accounts.Workspace, accounts.Digest("workspace:"+strings.Repeat("x", 43)), ports.RoleUser, time.Now().Add(time.Hour))
	ordinaryToken := ordinaryID + ".workspace." + strings.Repeat("x", 43)
	for _, method := range []string{"GET", "POST"} {
		if status, _ := request(method, "/api/operations/smtp", ordinaryToken, input); status != 403 {
			t.Fatal("ordinary SMTP access")
		}
	}
	save := func(input mailconfig.Input, expected int) mailconfig.View {
		t.Helper()
		status, raw := request("POST", "/api/operations/smtp", login.Token, input)
		if status != expected {
			t.Fatal("SMTP save", status, string(raw))
		}
		for _, private := range []string{"smtp-fixture-password", "password_cipher", "PasswordCipher", "PRIVATE"} {
			if bytes.Contains(raw, []byte(private)) {
				t.Fatal("SMTP response leaked credential")
			}
		}
		var view mailconfig.View
		if expected == 200 && json.Unmarshal(raw, &view) != nil {
			t.Fatal("SMTP view")
		}
		return view
	}
	if status, _ := request("POST", "/api/operations/smtp", login.Token, map[string]any{"revision": 0, "enabled": false}); status != 400 {
		t.Fatal("partial SMTP payload reset fields")
	}
	first := save(input, 200)
	if first.Revision != 1 || !first.PasswordConfigured {
		t.Fatal("initial save")
	}
	snapshot, err := store.MailConfigSnapshot(ctx)
	if err != nil || bytes.Contains(snapshot.PasswordCipher, []byte(input.Password)) {
		t.Fatal("plaintext persisted")
	}
	missingKey := filepath.Join(t.TempDir(), "must-not-create.key")
	if _, err := store.ConfigurationCipher(ctx, missingKey); err == nil {
		t.Fatal("missing key replaced despite SMTP ciphertext")
	}
	if _, err := os.Stat(missingKey); !os.IsNotExist(err) {
		t.Fatal("replacement encryption key was created")
	}
	plain, err := cipher.Open(snapshot.PasswordCipher)
	if err != nil || plain != input.Password {
		t.Fatal("encrypted password readback")
	}
	prefix := identity.New("smtp-fixture")
	send := func(to string, expected int) {
		status, raw := request("POST", "/api/auth/email-code", "", map[string]string{"email": to, "purpose": "login"})
		if status != expected {
			t.Fatal("code submission", status, string(raw))
		}
	}
	send(prefix+"1@example.test", 200)
	select {
	case message := <-messages1:
		if message.username != "first-fixture" || message.password != "smtp-fixture-password" || !strings.Contains(message.body, "verification code:") {
			t.Fatal("actual SMTP did not receive configured credentials and code")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("local SMTP submission missing")
	}
	input.Revision = 1
	input.Password = ""
	input.Address = address2
	input.Username = "second-fixture"
	save(input, 200)
	send(prefix+"2@example.test", 200)
	select {
	case message := <-messages2:
		if message.password != "smtp-fixture-password" || message.username != "second-fixture" {
			t.Fatal("blank password did not retain or address change required restart")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("latest SMTP address not used")
	}
	input.Revision = 2
	input.Password = "replacement-fixture-password"
	save(input, 200)
	send(prefix+"3@example.test", 200)
	select {
	case message := <-messages2:
		if message.password != input.Password {
			t.Fatal("runtime used stale secret")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("replacement SMTP submission missing")
	}
	input.Revision = 1
	save(input, 409)
	input.Revision = 3
	input.Password = ""
	input.ClearPassword = true
	save(input, 400)
	input.Enabled = false
	view := save(input, 200)
	if view.PasswordConfigured {
		t.Fatal("explicit clear ignored")
	}
	send(prefix+"4@example.test", 503)
	// A configured, rejected delivery never activates the verification challenge.
	input.Revision = 4
	input.ClearPassword = false
	input.Enabled = true
	input.Password = "replacement-fixture-password"
	save(input, 200)
	rejectEmail := prefix + "-reject@example.test"
	send(rejectEmail, 503)
	var active bool
	if err = conn.QueryRow(ctx, `SELECT active FROM drama_catalog_v1.email_verifications WHERE email=$1 ORDER BY created_at DESC LIMIT 1`, rejectEmail).Scan(&active); err != nil || active {
		t.Fatal("failed delivery activated challenge", err)
	}
	status, raw := request("GET", "/api/operations/smtp", login.Token, nil)
	if status != 200 || bytes.Contains(raw, []byte("replacement-fixture-password")) || bytes.Contains(raw, []byte("passwordCipher")) {
		t.Fatal("configuration GET leaked secret")
	}
	t.Log("Real PG + HTTP + verified local TLS SMTP: operator access, encrypted write-only credentials, CAS conflict, blank retention, explicit clear, runtime connection/password changes and rejected delivery verified; no external mail")
}

func TestSMTPStartTLSAndCertificateVerification(t *testing.T) {
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			address, roots, messages, _ := tlsMailSink(t, mode)
			config := mailconfig.Delivery{Settings: mailconfig.Settings{Address: address, Username: "fixture", From: "sender@example.test", TLS: mode}, Password: "fixture-password"}
			if err := (SMTP{Config: config}).SendCode(context.Background(), "recipient@example.test", "login", "fictional-code"); err == nil {
				t.Fatal("untrusted TLS certificate accepted")
			}
			if err := (SMTP{Config: config, roots: roots}).SendCode(context.Background(), "recipient@example.test", "login", "fictional-code"); err != nil {
				t.Fatal("verified SMTP submission", err)
			}
			select {
			case message := <-messages:
				if message.password != config.Password || !strings.Contains(message.body, "fictional-code") {
					t.Fatal("verified transport body/auth mismatch")
				}
			case <-time.After(6 * time.Second):
				t.Fatal("verified transport did not submit")
			}
		})
	}
}
