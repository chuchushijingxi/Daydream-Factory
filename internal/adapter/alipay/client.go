// Package alipay implements the official public-key RSA2 face-to-face payment protocol.
package alipay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

const productionGateway = "https://openapi.alipay.com/gateway.do"
const sandboxGateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"

var beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

// Config accepts deployment identifiers and key paths; gateway is production or sandbox.
// Limits are injected by cmd from appconfig; zero selects those same central defaults.
type Config struct {
	Gateway, AppID, SellerID, AppPrivateKeyFile, AlipayPublicKeyFile, NotifyURL         string
	HTTPClient                                                                          *http.Client
	Now                                                                                 func() time.Time
	HTTPTimeout                                                                         time.Duration
	ResponseMaxBytes, BillCompressedMaxBytes, BillUncompressedMaxBytes, BillCSVMaxBytes int64
	BillMaxEntries, BillMaxRows, BillMaxFieldBytes                                      int
}
type Channel struct {
	config  Config
	client  http.Client
	gateway string
	private *rsa.PrivateKey
	public  *rsa.PublicKey
}

var _ ports.PaymentChannel = (*Channel)(nil)

func New(c Config) (*Channel, error) {
	for name, value := range map[string]string{"appId": c.AppID, "sellerId": c.SellerID, "appPrivateKeyFile": c.AppPrivateKeyFile, "alipayPublicKeyFile": c.AlipayPublicKeyFile} {
		if value == "" || strings.TrimSpace(value) != value {
			return nil, invalidConfig(name)
		}
	}
	gateway := productionGateway
	switch c.Gateway {
	case "", "production":
	case "sandbox":
		gateway = sandboxGateway
	default:
		return nil, invalidConfig("gateway")
	}
	u, err := url.Parse(c.NotifyURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, invalidConfig("notifyUrl")
	}
	if c.HTTPTimeout < 0 || c.ResponseMaxBytes < 0 || c.BillCompressedMaxBytes < 0 || c.BillUncompressedMaxBytes < 0 || c.BillCSVMaxBytes < 0 || c.BillMaxEntries < 0 || c.BillMaxRows < 0 || c.BillMaxFieldBytes < 0 {
		return nil, invalidConfig("limits")
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = appconfig.PaymentHTTPTimeout
	}
	if c.ResponseMaxBytes == 0 {
		c.ResponseMaxBytes = appconfig.PaymentMaxResponseBytes
	}
	if c.BillCompressedMaxBytes == 0 {
		c.BillCompressedMaxBytes = appconfig.PaymentMaxBillCompressedBytes
	}
	if c.BillUncompressedMaxBytes == 0 {
		c.BillUncompressedMaxBytes = appconfig.PaymentMaxBillDecompressedBytes
	}
	if c.BillCSVMaxBytes == 0 {
		c.BillCSVMaxBytes = appconfig.PaymentMaxBillCSVBytes
	}
	if c.BillMaxEntries == 0 {
		c.BillMaxEntries = appconfig.PaymentMaxBillEntries
	}
	if c.BillMaxRows == 0 {
		c.BillMaxRows = appconfig.PaymentMaxBillRows
	}
	if c.BillMaxFieldBytes == 0 {
		c.BillMaxFieldBytes = appconfig.PaymentMaxBillFieldBytes
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	private, err := readPrivate(c.AppPrivateKeyFile)
	if err != nil {
		return nil, invalidConfig("appPrivateKeyFile")
	}
	public, err := readPublic(c.AlipayPublicKeyFile)
	if err != nil {
		return nil, invalidConfig("alipayPublicKeyFile")
	}
	client := http.Client{}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	client.Timeout = c.HTTPTimeout
	// Token URLs and signed gateway requests must never be forwarded to redirects.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	return &Channel{config: c, client: client, gateway: gateway, private: private, public: public}, nil
}
func (*Channel) ID() string            { return "alipay" }
func invalidConfig(field string) error { return fault.New("PAYMENT_CONFIG_INVALID", "alipay."+field) }
func protocol(field string) error      { return fault.New("PAYMENT_PROTOCOL_INVALID", "alipay."+field) }
func signatureError() error            { return fault.New("PAYMENT_SIGNATURE_INVALID", "alipay.sign") }
func identityError(field string) error {
	return fault.New("PAYMENT_IDENTITY_MISMATCH", "alipay."+field)
}
func channelError() error { return fault.New("PAYMENT_CHANNEL_FAILED", "alipay.transport") }

func readPrivate(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, protocol("privateKey")
	}
	var key *rsa.PrivateKey
	if block.Type == "RSA PRIVATE KEY" {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		var k any
		k, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		key, _ = k.(*rsa.PrivateKey)
	} else {
		return nil, protocol("privateKey")
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 || key.Validate() != nil {
		return nil, protocol("privateKey")
	}
	return key, nil
}
func readPublic(path string) (*rsa.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, protocol("publicKey")
	}
	var key *rsa.PublicKey
	if block.Type == "PUBLIC KEY" {
		var k any
		k, err = x509.ParsePKIXPublicKey(block.Bytes)
		key, _ = k.(*rsa.PublicKey)
	} else if block.Type == "RSA PUBLIC KEY" {
		key, err = x509.ParsePKCS1PublicKey(block.Bytes)
	} else {
		return nil, protocol("publicKey")
	}
	if err != nil || key == nil || key.N.BitLen() < 2048 || key.E < 3 {
		return nil, protocol("publicKey")
	}
	return key, nil
}
func canonical(values url.Values, notify bool) string {
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if k != "sign" && !(notify && k == "sign_type") && len(v) == 1 && v[0] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+values.Get(k))
	}
	return strings.Join(parts, "&")
}
func sign(key *rsa.PrivateKey, raw []byte) (string, error) {
	digest := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	return base64.StdEncoding.EncodeToString(sig), err
}
func verify(key *rsa.PublicKey, raw []byte, encoded string) error {
	sig, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return signatureError()
	}
	digest := sha256.Sum256(raw)
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) != nil {
		return signatureError()
	}
	return nil
}
func limitedRead(reader io.Reader, maximum int64) ([]byte, error) {
	// Prevent max+1 overflow even when a caller injects a nonsensical limit.
	if maximum <= 0 || maximum == int64(^uint64(0)>>1) {
		return nil, protocol("size")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, protocol("size")
	}
	return raw, nil
}

// object rejects duplicate JSON keys while retaining each value's exact signed bytes.
func object(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, protocol("json")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, protocol("json")
		}
		name, ok := key.(string)
		if !ok {
			return nil, protocol("json")
		}
		if _, exists := fields[name]; exists {
			return nil, protocol("duplicate")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, protocol("json")
		}
		fields[name] = value
	}
	if tok, err = d.Token(); err != nil || tok != json.Delim('}') {
		return nil, protocol("json")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, protocol("json")
	}
	return fields, nil
}

type response struct {
	// 缺省字段与显式空字段不同，负面应答也不能绕过已附带身份的核对。
	hasOrderNo, hasRefundNo, hasTradeNo bool
	hasTotalAmount, hasRefundAmount     bool
	Code                                string `json:"code"`
	SubCode                             string `json:"sub_code"`
	AppID                               string `json:"app_id"`
	SellerID                            string `json:"seller_id"`
	OrderNo                             string `json:"out_trade_no"`
	TradeNo                             string `json:"trade_no"`
	QRCode                              string `json:"qr_code"`
	TotalAmount                         string `json:"total_amount"`
	TradeStatus                         string `json:"trade_status"`
	PaidAt                              string `json:"send_pay_date"`
	RefundNo                            string `json:"out_request_no"`
	RefundAmount                        string `json:"refund_amount"`
	RefundFee                           string `json:"refund_fee"`
	RefundStatus                        string `json:"refund_status"`
	FundChange                          string `json:"fund_change"`
	RefundPaidAt                        string `json:"gmt_refund_pay"`
	BillURL                             string `json:"bill_download_url"`
}

func (c *Channel) call(ctx context.Context, method string, biz any, notify bool) (response, error) {
	var out response
	body, err := json.Marshal(biz)
	if err != nil {
		return out, protocol("request")
	}
	values := url.Values{"app_id": {c.config.AppID}, "method": {method}, "format": {"json"}, "charset": {"utf-8"}, "sign_type": {"RSA2"}, "timestamp": {c.config.Now().In(beijing).Format("2006-01-02 15:04:05")}, "version": {"1.0"}, "biz_content": {string(body)}}
	if notify {
		values.Set("notify_url", c.config.NotifyURL)
	}
	sig, err := sign(c.private, []byte(canonical(values, false)))
	if err != nil {
		return out, signatureError()
	}
	values.Set("sign", sig)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gateway, strings.NewReader(values.Encode()))
	if err != nil {
		return out, protocol("request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	resp, err := c.client.Do(req)
	if err != nil {
		return out, channelError()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, channelError()
	}
	raw, err := limitedRead(resp.Body, c.config.ResponseMaxBytes)
	if err != nil {
		return out, err
	}
	envelope, err := object(raw)
	if err != nil {
		return out, err
	}
	signed, ok := envelope[strings.ReplaceAll(method, ".", "_")+"_response"]
	if !ok {
		return out, protocol("response")
	}
	var encoded string
	if json.Unmarshal(envelope["sign"], &encoded) != nil || encoded == "" {
		return out, signatureError()
	}
	if err = verify(c.public, signed, encoded); err != nil {
		return out, err
	}
	signedFields, err := object(signed)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(signed, &out) != nil || out.Code == "" {
		return response{}, protocol("response")
	}
	_, out.hasOrderNo = signedFields["out_trade_no"]
	_, out.hasRefundNo = signedFields["out_request_no"]
	_, out.hasTradeNo = signedFields["trade_no"]
	_, out.hasTotalAmount = signedFields["total_amount"]
	_, out.hasRefundAmount = signedFields["refund_amount"]
	if out.AppID != "" && out.AppID != c.config.AppID {
		return response{}, identityError("app_id")
	}
	if out.SellerID != "" && out.SellerID != c.config.SellerID {
		return response{}, identityError("seller_id")
	}
	return out, nil
}
func businessError() error { return fault.New("PAYMENT_CHANNEL_FAILED", "alipay.business") }
