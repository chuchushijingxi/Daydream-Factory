package mailconfig

import (
	"context"
	"errors"
	"log"
	"net"
	"net/textproto"

	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
)

func submissionFailure(stage string, cause error, code, message string) error {
	id := identity.New("mail")
	var smtpError *textproto.Error
	var networkError net.Error
	var databaseError interface{ SQLState() string }
	var smtpCode int
	var sqlState string
	if errors.As(cause, &smtpError) {
		smtpCode = smtpError.Code
	}
	if errors.As(cause, &databaseError) {
		sqlState = databaseError.SQLState()
	}
	timeout := errors.Is(cause, context.DeadlineExceeded) || errors.As(cause, &networkError) && networkError.Timeout()
	// 对端错误正文可能回显密码/验证码；只记录不含正文的结构化诊断。
	log.Printf("mail submission id=%s stage=%s cause_type=%T smtp_code=%d sqlstate=%s timeout=%t cancelled=%t", id, stage, cause, smtpCode, sqlState, timeout, errors.Is(cause, context.Canceled))
	return fault.Wrap(code, message+"（诊断编号："+id+"）", cause)
}
