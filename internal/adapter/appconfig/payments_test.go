package appconfig_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"drama-agent/internal/adapter/appconfig"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPaymentEnvironmentDefaultsAndProtection(t *testing.T) {
	for _, tt := range []struct {
		name, extra string
		ok          bool
	}{{"default", "", true}, {"production-no-channels", "environment: production\npayment: {}\n", true}, {"development-demo", "environment: development\npayment:\n  demo:\n    enabled: true\n", true}, {"staging-demo", "environment: staging\npayment:\n  demo:\n    enabled: true\n", true}, {"production-demo", "payment:\n  demo:\n    enabled: true\n", false}, {"production-sandbox-disabled", "payment:\n  alipay:\n    gateway: sandbox\n    enabled: false\n", false}, {"staging-sandbox-disabled", "environment: staging\npayment:\n  alipay:\n    gateway: sandbox\n", true}, {"unknown", "environment: PRIVATE_PAYMENT_SENTINEL\n", false}, {"blank", "environment: ''\n", false}, {"unknown-payment-field", "payment:\n  privateKey: PRIVATE_PAYMENT_SENTINEL\n", false}} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := appconfig.Load(write(t, minimal+tt.extra))
			if (err == nil) != tt.ok {
				t.Fatal(err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE_PAYMENT_SENTINEL") {
				t.Fatal("value leaked", err)
			}
			if err == nil && tt.name == "default" && (cfg.Environment != "production" || cfg.Payment.Demo.Enabled || cfg.Payment.Wechatpay.Enabled || cfg.Payment.Alipay.Enabled || cfg.Payment.Alipay.Gateway != "production") {
				t.Fatal("unsafe defaults")
			}
		})
	}
}
func paymentKeys(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	writePEM := func(name, kind string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: raw}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writePEM("private.pem", "PRIVATE KEY", private)
	writePEM("public.pem", "PUBLIC KEY", public)
	writePEM("pkcs1-private.pem", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
	writePEM("pkcs1-public.pem", "RSA PUBLIC KEY", x509.MarshalPKCS1PublicKey(&key.PublicKey))
	cert := &x509.Certificate{SerialNumber: big.NewInt(1234), Subject: pkix.Name{CommonName: "Generated payment test key"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	certificate, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM("certificate.pem", "CERTIFICATE", certificate)
	wrong, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPublic, err := x509.MarshalPKIXPublicKey(&wrong.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	writePEM("wrong-public.pem", "PUBLIC KEY", wrongPublic)
	if err = os.WriteFile(filepath.Join(dir, "broken.pem"), []byte("PRIVATE_PAYMENT_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	doc := minimal + `environment: staging
payment:
  notifyBaseUrl: https://payments.example.invalid
  wechatpay:
    enabled: true
    mchId: generated-test-merchant
    appId: generated-test-app
    mchSerialNo: generated-test-serial
    mchPrivateKeyFile: private.pem
    apiV3Key: abcdefghijklmnopqrstuvwxyz012345
    verifyKeys:
      - id: PUB_KEY_ID_GENERATED
        file: public.pem
      - id: GENERATED_CERTIFICATE_SERIAL
        file: certificate.pem
  alipay:
    enabled: true
    gateway: sandbox
    appId: generated-alipay-app
    sellerId: generated-alipay-seller
    appPrivateKeyFile: pkcs1-private.pem
    alipayPublicKeyFile: pkcs1-public.pem
`
	return dir, doc
}
func loadPaymentDoc(t *testing.T, dir, doc string) (appconfig.Config, error) {
	t.Helper()
	name := filepath.Join(dir, "app.local.yaml")
	if err := os.WriteFile(name, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	return appconfig.Load(name)
}
func TestPaymentConfigResolvesPathsBeforeParsingGeneratedKeys(t *testing.T) {
	dir, doc := paymentKeys(t)
	cfg, err := loadPaymentDoc(t, dir, doc)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Payment.Wechatpay.MchPrivateKeyFile != filepath.Join(dir, "private.pem") || cfg.Payment.Wechatpay.VerifyKeys[0].File != filepath.Join(dir, "public.pem") || cfg.Payment.Alipay.AppPrivateKeyFile != filepath.Join(dir, "pkcs1-private.pem") || cfg.Payment.Alipay.AlipayPublicKeyFile != filepath.Join(dir, "pkcs1-public.pem") {
		t.Fatal("paths were not relative to configuration")
	}
	if cfg.Payment.Wechatpay.PrivateKey == nil || len(cfg.Payment.Wechatpay.PublicKeys) != 2 {
		t.Fatal("validated key material not available for assembly")
	}
	if cfg.Payment.Wechatpay.PrivateKey.N.BitLen() != 2048 || cfg.Payment.Wechatpay.PublicKeys["PUB_KEY_ID_GENERATED"].N.Cmp(cfg.Payment.Wechatpay.PrivateKey.N) != 0 {
		t.Fatal("wrong parsed key")
	}
	production := strings.Replace(strings.Replace(doc, "environment: staging", "environment: production", 1), "gateway: sandbox", "gateway: production", 1)
	if _, err := loadPaymentDoc(t, dir, production); err != nil {
		t.Fatal("production real-only config rejected", err)
	}
	combined := strings.Replace(doc, "payment:\n", "payment:\n  demo:\n    enabled: true\n", 1)
	if _, err := loadPaymentDoc(t, dir, combined); err != nil {
		t.Fatal("nonproduction demo and real config rejected", err)
	}

}
func TestPaymentConfigEnabledRequirementsAndSafeErrors(t *testing.T) {
	dir, doc := paymentKeys(t)
	for _, tt := range []struct{ name, old, next, field string }{
		{"notify", "https://payments.example.invalid", "http://PRIVATE_PAYMENT_SENTINEL.invalid", "payment.notifyBaseUrl"},
		{"notify-userinfo", "https://payments.example.invalid", "https://PRIVATE_PAYMENT_SENTINEL:password@example.invalid", "payment.notifyBaseUrl"},
		{"merchant", "mchId: generated-test-merchant", "mchId: ''", "payment.wechatpay.mchId"},
		{"wechat-app", "appId: generated-test-app", "appId: ''", "payment.wechatpay.appId"},
		{"serial", "mchSerialNo: generated-test-serial", "mchSerialNo: ''", "payment.wechatpay.mchSerialNo"},
		{"api-key", "apiV3Key: abcdefghijklmnopqrstuvwxyz012345", "apiV3Key: PRIVATE_PAYMENT_SENTINEL", "payment.wechatpay.apiV3Key"},
		{"private-file", "mchPrivateKeyFile: private.pem", "mchPrivateKeyFile: PRIVATE_PAYMENT_SENTINEL.pem", "payment.wechatpay.mchPrivateKeyFile"},
		{"private-malformed", "mchPrivateKeyFile: private.pem", "mchPrivateKeyFile: broken.pem", "payment.wechatpay.mchPrivateKeyFile"},
		{"public-malformed", "file: public.pem", "file: broken.pem", "payment.wechatpay.verifyKeys.file"},
		{"public-wrong-type", "file: public.pem", "file: wrong-public.pem", "payment.wechatpay.verifyKeys.file"},
		{"duplicate-serial", "id: GENERATED_CERTIFICATE_SERIAL", "id: PUB_KEY_ID_GENERATED", "payment.wechatpay.verifyKeys.id"},
		{"no-key-id", "id: PUB_KEY_ID_GENERATED", "id: ''", "payment.wechatpay.verifyKeys.id"},
		{"alipay-app", "appId: generated-alipay-app", "appId: ''", "payment.alipay.appId"},
		{"seller", "sellerId: generated-alipay-seller", "sellerId: ''", "payment.alipay.sellerId"},
		{"ali-private", "appPrivateKeyFile: pkcs1-private.pem", "appPrivateKeyFile: broken.pem", "payment.alipay.appPrivateKeyFile"},
		{"ali-public", "alipayPublicKeyFile: pkcs1-public.pem", "alipayPublicKeyFile: broken.pem", "payment.alipay.alipayPublicKeyFile"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadPaymentDoc(t, dir, strings.Replace(doc, tt.old, tt.next, 1))
			if err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatal(err)
			}
			for _, secret := range []string{"PRIVATE_PAYMENT_SENTINEL", dir, "password", "generated-test-merchant", "abcdefghijklmnopqrstuvwxyz012345"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("configuration error leaked value", err)
				}
			}
		})
	}
	// 密钥文件过大时先失败，不尝试无界读取或输出内容。
	if err := os.WriteFile(filepath.Join(dir, "large.pem"), []byte(strings.Repeat("x", int(appconfig.PaymentKeyMaxBytes)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPaymentDoc(t, dir, strings.Replace(doc, "mchPrivateKeyFile: private.pem", "mchPrivateKeyFile: large.pem", 1)); err == nil {
		t.Fatal("oversized key accepted")
	}
}
