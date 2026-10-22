package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ParseTrustedProxies 解析逗号分隔的 CIDR 列表；空串表示不信任任何代理，按直连地址限流。
func ParseTrustedProxies(raw string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", item, err)
		}
		out = append(out, network)
	}
	return out, nil
}

// clientIP 默认取直连地址；只有直连方位于受信代理网段（如前端 nginx 容器）时，
// 才采用它写入的 X-Real-IP，避免直连客户端伪造来源绕过按 IP 限流。
func (h *Handler) clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	peer := net.ParseIP(ip)
	if peer == nil || len(h.TrustedProxies) == 0 {
		return ip
	}
	for _, network := range h.TrustedProxies {
		if network.Contains(peer) {
			if forwarded := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); forwarded != nil {
				return forwarded.String()
			}
			return ip
		}
	}
	return ip
}
