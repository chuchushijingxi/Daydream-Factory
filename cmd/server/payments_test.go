package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"drama-agent/internal/adapter/appconfig"
	paymentapp "drama-agent/internal/application/payment"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/accounts"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type paymentResourcesFixture struct {
	ports.PaymentStore
	ports.DemoPaymentStore
}

func (*paymentResourcesFixture) CurrentUser(context.Context, string) (accounts.User, error) {
	return accounts.User{}, nil
}

type paymentRateFixture struct{ ports.RateLimiter }
type paymentRoundTripper func(*http.Request) (*http.Response, error)

func (f paymentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func paymentConfigurationFixture(t *testing.T) appconfig.Config {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	privateFile, publicFile := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	for _, f := range []struct {
		name, kind string
		body       []byte
	}{{privateFile, "PRIVATE KEY", private}, {publicFile, "PUBLIC KEY", public}} {
		if err = os.WriteFile(f.name, pem.EncodeToMemory(&pem.Block{Type: f.kind, Bytes: f.body}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := appconfig.Defaults()
	cfg.Environment = "staging"
	cfg.Payment.NotifyBaseURL = "https://payments.example.invalid/base/"
	cfg.Payment.Demo.Enabled = true
	cfg.Payment.Wechatpay = appconfig.WechatPayment{Enabled: true, MchID: "GENERATED_MERCHANT", AppID: "generated_app", MchSerialNo: "ABC123", APIV3Key: "abcdefghijklmnopqrstuvwxyz012345", MchPrivateKeyFile: privateFile, PrivateKey: key, PublicKeys: map[string]*rsa.PublicKey{"RETIRED": &key.PublicKey, "CURRENT": &key.PublicKey}, VerifyKeys: []appconfig.PaymentVerifyKey{{ID: "CURRENT", File: publicFile}, {ID: "RETIRED", File: publicFile}}}
	cfg.Payment.Alipay = appconfig.AlipayPayment{Enabled: true, Gateway: "sandbox", AppID: "generated_alipay_app", SellerID: "generated_alipay_seller", AppPrivateKeyFile: privateFile, AlipayPublicKeyFile: publicFile}
	return cfg
}
func TestPaymentServerAssemblyMapsAllChannelsWithoutNetwork(t *testing.T) {
	cfg := paymentConfigurationFixture(t)
	var outbound atomic.Int32
	original := http.DefaultTransport
	http.DefaultTransport = paymentRoundTripper(func(*http.Request) (*http.Response, error) {
		outbound.Add(1)
		return nil, errors.New("constructor must not perform network")
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	store, runtime := &paymentResourcesFixture{}, &paymentRateFixture{}
	service, err := configuredPayments(cfg, store, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(service.Channels) != 3 || service.Environment != "staging" || service.Store != store || service.Users != store || service.RateLimiter != runtime {
		t.Fatal("payment dependencies not bound")
	}
	if _, ok := service.Targets.(paymentapp.PersonalTargets); !ok {
		t.Fatal("personal recharge targets not installed")
	}
	for _, id := range []string{"demo", "wechatpay", "alipay"} {
		if service.Channels[id] == nil || service.Channels[id].ID() != id {
			t.Fatal("missing configured channel", id)
		}
	}
	if outbound.Load() != 0 {
		t.Fatal("construction sent an external request")
	}
	wechat := configuredWechatPayment(cfg.Payment)
	if wechat.VerifySerial != "CURRENT" || len(wechat.VerifyKeys) != 2 || wechat.PrivateKey != cfg.Payment.Wechatpay.PrivateKey || string(wechat.APIV3Key) != cfg.Payment.Wechatpay.APIV3Key || wechat.NotifyURL != "https://payments.example.invalid/base/api/payments/notify/wechatpay" {
		t.Fatal("wechat merchant/key/notify mapping changed")
	}
	if wechat.RequestTimeout != appconfig.PaymentHTTPTimeout || wechat.SignatureWindow != appconfig.PaymentSignatureWindow || wechat.NotifyMaxBytes != appconfig.PaymentNotifyMaxBytes || wechat.MaxResponseBytes != appconfig.PaymentMaxResponseBytes || wechat.MaxBillCSVBytes != appconfig.PaymentMaxBillCSVBytes || wechat.MaxBillRows != appconfig.PaymentMaxBillRows {
		t.Fatal("wechat runtime budgets not injected")
	}
	alipay := configuredAlipayPayment(cfg.Payment)
	if alipay.Gateway != "sandbox" || alipay.SellerID != cfg.Payment.Alipay.SellerID || alipay.AppPrivateKeyFile != cfg.Payment.Alipay.AppPrivateKeyFile || alipay.AlipayPublicKeyFile != cfg.Payment.Alipay.AlipayPublicKeyFile || alipay.NotifyURL != "https://payments.example.invalid/base/api/payments/notify/alipay" {
		t.Fatal("alipay merchant/key/notify mapping changed")
	}
	if alipay.HTTPTimeout != appconfig.PaymentHTTPTimeout || alipay.BillCompressedMaxBytes != appconfig.PaymentMaxBillCompressedBytes || alipay.BillUncompressedMaxBytes != appconfig.PaymentMaxBillDecompressedBytes || alipay.BillMaxEntries != appconfig.PaymentMaxBillEntries || alipay.BillMaxFieldBytes != appconfig.PaymentMaxBillFieldBytes {
		t.Fatal("alipay runtime budgets not injected")
	}
}
func TestPaymentServerAssemblyAllowsNoChannelsAndRedactsInitializationErrors(t *testing.T) {
	service, err := configuredPayments(appconfig.Defaults(), &paymentResourcesFixture{}, &paymentRateFixture{})
	if err != nil || len(service.Channels) != 0 {
		t.Fatal("default deployment requires a merchant", err)
	}
	cfg := appconfig.Defaults()
	cfg.Payment.Wechatpay.Enabled = true
	if _, err = configuredPayments(cfg, &paymentResourcesFixture{}, &paymentRateFixture{}); err == nil {
		t.Fatal("empty verification list panicked or was accepted")
	}
	cfg.Payment.Wechatpay.Enabled = false
	cfg.Payment.Alipay.Enabled = true
	cfg.Payment.Alipay.AppID = "PRIVATE_MERCHANT_SENTINEL"
	cfg.Payment.Alipay.SellerID = "PRIVATE_SELLER_SENTINEL"
	cfg.Payment.Alipay.AppPrivateKeyFile = "PRIVATE_PATH_SENTINEL.pem"
	_, err = configuredPayments(cfg, &paymentResourcesFixture{}, &paymentRateFixture{})
	if err == nil {
		t.Fatal("invalid channel initialization accepted")
	}
	for _, private := range []string{"PRIVATE_MERCHANT_SENTINEL", "PRIVATE_SELLER_SENTINEL", "PRIVATE_PATH_SENTINEL"} {
		if strings.Contains(err.Error(), private) {
			t.Fatal("initialization error exposed private data")
		}
	}
}

type paymentMaintenanceFixture struct {
	entered chan string
	exited  atomic.Int32
}

func (f *paymentMaintenanceFixture) step(ctx context.Context, name string) error {
	f.entered <- name
	<-ctx.Done()
	f.exited.Add(1)
	return ctx.Err()
}
func (f *paymentMaintenanceFixture) ScanOrders(ctx context.Context) error {
	return f.step(ctx, "orders")
}
func (f *paymentMaintenanceFixture) ScanRefunds(ctx context.Context) error {
	return f.step(ctx, "refunds")
}
func (f *paymentMaintenanceFixture) ReconcileBills(ctx context.Context) error {
	return f.step(ctx, "bills")
}
func TestPaymentLoopsSkipReadOnlyAndNoChannels(t *testing.T) {
	for _, tt := range []struct{ readOnly, hasChannels bool }{{true, true}, {true, false}, {false, false}} {
		fixture := &paymentMaintenanceFixture{entered: make(chan string, 3)}
		done := startPaymentLoops(context.Background(), tt.readOnly, tt.hasChannels, fixture)
		select {
		case <-done:
		default:
			t.Fatal("disabled payment loops spawned background work")
		}
		select {
		case name := <-fixture.entered:
			t.Fatal("payment write loop ran", name)
		default:
		}
	}
}
func TestPaymentLoopsCancelAndJoinAllThreeJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture := &paymentMaintenanceFixture{entered: make(chan string, 3)}
	done := startPaymentLoops(ctx, false, true, fixture)
	seen := map[string]bool{}
	for range 3 {
		select {
		case name := <-fixture.entered:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("payment loop did not start")
		}
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("payment shutdown did not wait for cancellation")
	}
	if fixture.exited.Load() != 3 {
		t.Fatal("resources could be closed while payment job still ran", fixture.exited.Load())
	}
}
func TestPaymentJobHasFiniteDeadlineRetriesAndDoesNotLeakErrors(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan int32, 4)
	var count, active atomic.Int32
	var timedOut atomic.Bool
	job := paymentJob{name: "orders", interval: time.Millisecond, timeout: 20 * time.Millisecond, run: func(call context.Context) error {
		if active.Add(1) != 1 {
			t.Error("same payment loop overlapped itself")
		}
		defer active.Add(-1)
		deadline, ok := call.Deadline()
		if !ok || time.Until(deadline) > 25*time.Millisecond {
			t.Error("payment call lacks bounded deadline")
		}
		n := count.Add(1)
		calls <- n
		if n == 1 {
			return errors.New("PRIVATE_TOKEN_URL_AND_BODY_SENTINEL")
		}
		<-call.Done()
		if n == 2 && errors.Is(call.Err(), context.DeadlineExceeded) {
			timedOut.Store(true)
		}
		return call.Err()
	}}
	done := make(chan struct{})
	go func() { defer close(done); runPaymentJob(ctx, job) }()
	for range 3 {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("failed loop did not retry")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active payment step ignored parent cancellation")
	}
	if !timedOut.Load() {
		t.Fatal("loop did not resume after its own call deadline")
	}
	if strings.Contains(logs.String(), "PRIVATE_TOKEN_URL_AND_BODY_SENTINEL") || !strings.Contains(logs.String(), "payment_background_failed") {
		t.Fatal("unsafe or missing payment failure log")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	ran := false
	runPaymentJob(cancelled, paymentJob{name: "orders", interval: time.Millisecond, timeout: time.Millisecond, run: func(context.Context) error { ran = true; return nil }})
	if ran {
		t.Fatal("cancelled payment loop performed late work")
	}
}
