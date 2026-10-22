package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsForwardedAddressOnlyFromConfiguredProxies(t *testing.T) {
	proxies, err := ParseTrustedProxies(" 172.16.0.0/12, 10.0.0.0/8 ,")
	if err != nil || len(proxies) != 2 {
		t.Fatal(proxies, err)
	}
	if _, err = ParseTrustedProxies("172.16.0.0"); err == nil {
		t.Fatal("bare address accepted as CIDR")
	}
	h := &Handler{TrustedProxies: proxies}
	request := func(remote, forwarded string) string {
		r := httptest.NewRequest("GET", "/api/health", nil)
		r.RemoteAddr = remote
		if forwarded != "" {
			r.Header.Set("X-Real-IP", forwarded)
		}
		return h.clientIP(r)
	}
	// 前端代理容器转发的真实来源被采用，不同用户不再共享代理的限流额度。
	if got := request("172.18.0.5:41234", "203.0.113.7"); got != "203.0.113.7" {
		t.Fatal("trusted proxy", got)
	}
	// 直连客户端不能用伪造头冒充其他来源。
	if got := request("198.51.100.9:5000", "203.0.113.7"); got != "198.51.100.9" {
		t.Fatal("untrusted peer spoofed", got)
	}
	// 受信代理没给出合法来源时退回代理自身地址，而不是空键。
	if got := request("10.0.0.3:80", "not-an-ip"); got != "10.0.0.3" {
		t.Fatal("invalid forwarded value", got)
	}
	if got := (&Handler{}).clientIP(httptest.NewRequest("GET", "/", nil)); got != "192.0.2.1" {
		t.Fatal("no proxies configured must keep the direct peer", got)
	}
}
