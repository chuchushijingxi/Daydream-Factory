package accounts

import (
	"encoding/base64"
	"strings"

	"drama-agent/internal/domain/fault"
)

const Workspace = "workspace"
const Operations = "operations"

func ValidAudience(value string) bool { return value == Workspace || value == Operations }

// SessionParts只校验凭证信封格式；唯一SessionAuthenticator依据Redis当前会话核对摘要。
func SessionParts(token string) (id, audience, digest string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || len(parts[0]) > 128 || !ValidAudience(parts[1]) || len(parts[2]) != 43 {
		return "", "", "", fault.New("AUTH_REQUIRED", "请先登录")
	}
	secret, decodeErr := base64.RawURLEncoding.DecodeString(parts[2])
	if decodeErr != nil || len(secret) != 32 {
		return "", "", "", fault.New("AUTH_REQUIRED", "请先登录")
	}
	return parts[0], parts[1], Digest(parts[1] + ":" + parts[2]), nil
}
