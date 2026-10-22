package wechatpay

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (c *Client) authorization(method, uri, nonce string, body []byte) (string, error) {
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	message := method + "\n" + uri + "\n" + timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.cfg.PrivateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", signatureError()
	}
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`, c.cfg.MchID, nonce, timestamp, c.cfg.MchSerialNo, base64.StdEncoding.EncodeToString(signature)), nil
}
func (c *Client) verify(header http.Header, body []byte) error {
	// 单头唯一且Serial精确选择已配置密钥，不能尝试任意一把能通过的密钥。
	read := func(name string) (string, bool) {
		values := header.Values(name)
		returnValue := ""
		if len(values) == 1 {
			returnValue = values[0]
		}
		return returnValue, len(values) == 1 && returnValue != ""
	}
	serial, ok := read("Wechatpay-Serial")
	if !ok {
		return signatureError()
	}
	key := c.cfg.VerifyKeys[serial]
	if key == nil {
		return signatureError()
	}
	timestamp, ok := read("Wechatpay-Timestamp")
	if !ok {
		return signatureError()
	}
	for _, v := range timestamp {
		if v < '0' || v > '9' {
			return signatureError()
		}
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return signatureError()
	}
	now := c.now().Unix()
	window := int64(c.cfg.SignatureWindow.Seconds())
	if seconds < now-window || seconds > now+window {
		return signatureError()
	}
	nonce, ok := read("Wechatpay-Nonce")
	if !ok || len(nonce) > 256 || strings.ContainsAny(nonce, "\r\n\x00") {
		return signatureError()
	}
	signature, ok := read("Wechatpay-Signature")
	if !ok {
		return signatureError()
	}
	if kind := header.Get("Wechatpay-Signature-Type"); kind != "" && kind != "WECHATPAY2-SHA256-RSA2048" {
		return signatureError()
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return signatureError()
	}
	digest := sha256.Sum256([]byte(timestamp + "\n" + nonce + "\n" + string(body) + "\n"))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], decoded) != nil {
		return signatureError()
	}
	return nil
}
