// Package wechatpay implements the ordinary-merchant WeChat Pay APIv3 protocol.
package wechatpay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

const endpoint = "https://api.mch.weixin.qq.com"

type Config struct {
	VerifySerial                                                      string
	MchID, AppID, MchSerialNo, NotifyURL                              string
	PrivateKey                                                        *rsa.PrivateKey
	APIV3Key                                                          []byte
	VerifyKeys                                                        map[string]*rsa.PublicKey
	HTTPClient                                                        *http.Client
	Now                                                               func() time.Time
	RequestTimeout, SignatureWindow                                   time.Duration
	MaxResponseBytes, NotifyMaxBytes                                  int64
	MaxBillCompressedBytes, MaxBillDecompressedBytes, MaxBillCSVBytes int64
	MaxBillRows, MaxBillFieldBytes                                    int
}
type Client struct {
	cfg  Config
	http *http.Client
}

var _ ports.PaymentChannel = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	invalid := func() (*Client, error) { return nil, fault.New("PAYMENT_CONFIG_INVALID", "微信支付配置无效") }
	if !identifier(cfg.MchID, 128) || !identifier(cfg.AppID, 128) || !identifier(cfg.MchSerialNo, 128) || cfg.PrivateKey == nil || cfg.PrivateKey.N == nil || cfg.PrivateKey.N.BitLen() < 2048 || len(cfg.APIV3Key) != 32 || len(cfg.VerifyKeys) == 0 {
		return invalid()
	}
	if cfg.PrivateKey.Validate() != nil {
		return invalid()
	}
	u, err := url.Parse(cfg.NotifyURL)
	if len(cfg.NotifyURL) > 256 || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return invalid()
	}
	if cfg.RequestTimeout <= 0 || cfg.SignatureWindow <= 0 || cfg.MaxResponseBytes <= 0 || cfg.NotifyMaxBytes <= 0 || cfg.MaxBillCompressedBytes <= 0 || cfg.MaxBillDecompressedBytes <= 0 || cfg.MaxBillCSVBytes <= 0 || cfg.MaxBillRows <= 0 || cfg.MaxBillFieldBytes <= 0 {
		return invalid()
	}
	if !identifier(cfg.VerifySerial, 128) || cfg.VerifyKeys[cfg.VerifySerial] == nil {
		return invalid()
	}
	keys := make(map[string]*rsa.PublicKey, len(cfg.VerifyKeys))
	for serial, key := range cfg.VerifyKeys {
		if !identifier(serial, 128) || key == nil || key.N == nil || key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
			return invalid()
		}
		keys[serial] = key
	}
	cfg.VerifyKeys = keys
	cfg.APIV3Key = append([]byte(nil), cfg.APIV3Key...)
	hc := http.Client{}
	if cfg.HTTPClient != nil {
		hc = *cfg.HTTPClient
	}
	// 所有跳转都停在第一跳，签名及商户身份绝不随重定向外发。
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if hc.Timeout == 0 || hc.Timeout > cfg.RequestTimeout {
		hc.Timeout = cfg.RequestTimeout
	}
	return &Client{cfg: cfg, http: &hc}, nil
}
func (c *Client) ID() string { return "wechatpay" }
func (c *Client) now() time.Time {
	if c.cfg.Now != nil {
		return c.cfg.Now().UTC()
	}
	return time.Now().UTC()
}
func identifier(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, v := range value {
		if !(v >= 'A' && v <= 'Z' || v >= 'a' && v <= 'z' || v >= '0' && v <= '9' || v == '_' || v == '-') {
			return false
		}
	}
	return true
}
func protocol() error {
	return fault.New("PAYMENT_CHANNEL_FAILED", "微信支付返回的事实无效")
}
func identityError() error {
	return fault.New("PAYMENT_IDENTITY_MISMATCH", "微信支付身份核对失败")
}
func signatureError() error {
	return fault.New("PAYMENT_SIGNATURE_INVALID", "微信支付签名校验失败")
}
func amountError() error { return fault.New("PAYMENT_INVALID_AMOUNT", "微信支付金额无效") }
func transportError() error {
	return fault.New("PAYMENT_CHANNEL_FAILED", "微信支付请求失败，请稍后重试")
}

type responseError struct{ code string }

func (*responseError) Error() string { return "微信支付业务拒绝" }
func hasResponseCode(err error, code string) bool {
	var response *responseError
	return errors.As(err, &response) && response.code == code
}

func (c *Client) request(ctx context.Context, method, address string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, transportError()
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return nil, transportError()
	}
	authorization, err := c.authorization(method, req.URL.RequestURI(), hex.EncodeToString(nonce), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Wechatpay-Serial", c.cfg.VerifySerial)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError()
	}
	return resp, nil
}
func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, protocol()
	}
	return raw, nil
}
func (c *Client) api(ctx context.Context, method, path string, input any, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return protocol()
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	resp, err := c.request(callCtx, method, endpoint+path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := readLimited(resp.Body, c.cfg.MaxResponseBytes)
	if err != nil {
		return err
	}
	if err = c.verify(resp.Header, raw); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Code string `json:"code"`
		}
		if decodeJSON(raw, &failure) != nil {
			return protocol()
		}
		// 只在内部按精确错误码决定分支；错误描述及整个报文不能进入日志或对外错误。
		return fault.Wrap("PAYMENT_CHANNEL_FAILED", "微信支付请求未成功", &responseError{code: failure.Code})
	}
	if resp.StatusCode == http.StatusNoContent {
		if len(raw) != 0 || output != nil {
			return protocol()
		}
		return nil
	}
	if output == nil {
		return protocol()
	}
	if err = decodeJSON(raw, output); err != nil {
		return protocol()
	}
	return nil
}
func decodeJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return protocol()
				}
				keys[name] = true
				if err = walk(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return protocol()
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return protocol()
			}
		default:
			return protocol()
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return protocol()
	}
	return json.Unmarshal(raw, out)
}
func cleanPath(value string) string { return url.PathEscape(value) }
func safeDownloadURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "api.mch.weixin.qq.com") || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, protocol()
	}
	return u, nil
}
