package appconfig

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Payment struct {
	NotifyBaseURL string        `yaml:"notifyBaseUrl"`
	Demo          DemoPayment   `yaml:"demo"`
	Wechatpay     WechatPayment `yaml:"wechatpay"`
	Alipay        AlipayPayment `yaml:"alipay"`
}
type DemoPayment struct {
	Enabled bool `yaml:"enabled"`
}
type PaymentVerifyKey struct {
	ID   string `yaml:"id"`
	File string `yaml:"file"`
}
type WechatPayment struct {
	Enabled           bool                      `yaml:"enabled"`
	MchID             string                    `yaml:"mchId"`
	AppID             string                    `yaml:"appId"`
	MchSerialNo       string                    `yaml:"mchSerialNo"`
	MchPrivateKeyFile string                    `yaml:"mchPrivateKeyFile"`
	APIV3Key          string                    `yaml:"apiV3Key"`
	VerifyKeys        []PaymentVerifyKey        `yaml:"verifyKeys"`
	PrivateKey        *rsa.PrivateKey           `yaml:"-" json:"-"`
	PublicKeys        map[string]*rsa.PublicKey `yaml:"-" json:"-"`
}
type AlipayPayment struct {
	Enabled             bool   `yaml:"enabled"`
	Gateway             string `yaml:"gateway"`
	AppID               string `yaml:"appId"`
	SellerID            string `yaml:"sellerId"`
	AppPrivateKeyFile   string `yaml:"appPrivateKeyFile"`
	AlipayPublicKeyFile string `yaml:"alipayPublicKeyFile"`
}

func (c *Config) resolvePaymentPaths(base string) {
	for _, path := range []*string{&c.Payment.Wechatpay.MchPrivateKeyFile, &c.Payment.Alipay.AppPrivateKeyFile, &c.Payment.Alipay.AlipayPublicKeyFile} {
		if *path != "" && !filepath.IsAbs(*path) {
			*path = filepath.Join(base, *path)
		}
	}
	for i := range c.Payment.Wechatpay.VerifyKeys {
		path := &c.Payment.Wechatpay.VerifyKeys[i].File
		if *path != "" && !filepath.IsAbs(*path) {
			*path = filepath.Join(base, *path)
		}
	}
}
func paymentField(field string) error { return fmt.Errorf("invalid configuration %s", field) }
func paymentIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
func (c *Config) validatePayment() error {
	if c.Environment != "production" && c.Environment != "staging" && c.Environment != "development" {
		return paymentField("environment")
	}
	if c.Environment == "production" && c.Payment.Demo.Enabled {
		return paymentField("payment.demo.enabled")
	}
	a := &c.Payment.Alipay
	if a.Gateway != "production" && a.Gateway != "sandbox" {
		return paymentField("payment.alipay.gateway")
	}
	if c.Environment == "production" && a.Gateway == "sandbox" {
		return paymentField("payment.alipay.gateway")
	}
	w := &c.Payment.Wechatpay
	if w.Enabled || a.Enabled {
		u, err := url.Parse(c.Payment.NotifyBaseURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return paymentField("payment.notifyBaseUrl")
		}
	}
	if w.Enabled {
		for _, f := range []struct{ name, value string }{{"mchId", w.MchID}, {"appId", w.AppID}, {"mchSerialNo", w.MchSerialNo}} {
			if !paymentIdentifier(f.value) {
				return paymentField("payment.wechatpay." + f.name)
			}
		}
		if len(w.APIV3Key) != 32 {
			return paymentField("payment.wechatpay.apiV3Key")
		}
		key, err := paymentPrivateKey(w.MchPrivateKeyFile)
		if err != nil {
			return paymentField("payment.wechatpay.mchPrivateKeyFile")
		}
		w.PrivateKey = key
		if len(w.VerifyKeys) < 1 || len(w.VerifyKeys) > 8 {
			return paymentField("payment.wechatpay.verifyKeys")
		}
		w.PublicKeys = make(map[string]*rsa.PublicKey, len(w.VerifyKeys))
		for _, v := range w.VerifyKeys {
			if !paymentIdentifier(v.ID) {
				return paymentField("payment.wechatpay.verifyKeys.id")
			}
			if _, exists := w.PublicKeys[v.ID]; exists {
				return paymentField("payment.wechatpay.verifyKeys.id")
			}
			key, err := paymentPublicKey(v.File)
			if err != nil {
				return paymentField("payment.wechatpay.verifyKeys.file")
			}
			w.PublicKeys[v.ID] = key
		}
	}
	if a.Enabled {
		if !paymentIdentifier(a.AppID) {
			return paymentField("payment.alipay.appId")
		}
		if !paymentIdentifier(a.SellerID) {
			return paymentField("payment.alipay.sellerId")
		}
		if _, err := paymentPrivateKey(a.AppPrivateKeyFile); err != nil {
			return paymentField("payment.alipay.appPrivateKeyFile")
		}
		if _, err := paymentPublicKey(a.AlipayPublicKeyFile); err != nil {
			return paymentField("payment.alipay.alipayPublicKeyFile")
		}
	}
	return nil
}
func paymentPEM(path string) (*pem.Block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("key file unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > PaymentKeyMaxBytes {
		return nil, fmt.Errorf("invalid key file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, PaymentKeyMaxBytes+1))
	if err != nil || int64(len(raw)) > PaymentKeyMaxBytes {
		return nil, fmt.Errorf("invalid key file")
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(block.Headers) != 0 || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, fmt.Errorf("invalid key encoding")
	}
	return block, nil
}
func paymentPrivateKey(path string) (*rsa.PrivateKey, error) {
	block, err := paymentPEM(path)
	if err != nil {
		return nil, err
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var raw any
		raw, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			key, _ = raw.(*rsa.PrivateKey)
		}
	default:
		return nil, fmt.Errorf("invalid RSA private key")
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, fmt.Errorf("invalid RSA private key")
	}
	return key, nil
}
func paymentPublicKey(path string) (*rsa.PublicKey, error) {
	block, err := paymentPEM(path)
	if err != nil {
		return nil, err
	}
	var raw any
	switch block.Type {
	case "PUBLIC KEY":
		raw, err = x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		raw, err = x509.ParsePKCS1PublicKey(block.Bytes)
	case "CERTIFICATE":
		var cert *x509.Certificate
		cert, err = x509.ParseCertificate(block.Bytes)
		if err == nil {
			raw = cert.PublicKey
		}
	default:
		return nil, fmt.Errorf("invalid RSA public key")
	}
	key, ok := raw.(*rsa.PublicKey)
	if err != nil || !ok || key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
		return nil, fmt.Errorf("invalid RSA public key")
	}
	return key, nil
}
