// Package smtpmail submits verification mail to the configured SMTP server over verified TLS.
package smtpmail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/mailconfig"
	"drama-agent/internal/domain/fault"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
)

type SMTP struct {
	Config mailconfig.Delivery
	roots  *x509.CertPool
}

func (s SMTP) SendCode(ctx context.Context, email, purpose, code string) error {
	from, e := mail.ParseAddress(s.Config.From)
	if e != nil || from.Address != s.Config.From {
		return fault.New("MAIL_UNAVAILABLE", "邮件发送地址无效")
	}
	to, e := mail.ParseAddress(email)
	if e != nil || to.Address != email {
		return fault.ErrInvalid
	}
	host, _, e := net.SplitHostPort(s.Config.Address)
	if e != nil {
		return fault.New("MAIL_UNAVAILABLE", "邮件连接无效")
	}
	ctx, cancel := context.WithTimeout(ctx, appconfig.MailSubmissionTimeout)
	defer cancel()
	dialer := &net.Dialer{}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: s.roots}
	var conn net.Conn
	if s.Config.TLS == "tls" {
		conn, e = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", s.Config.Address)
	} else {
		conn, e = dialer.DialContext(ctx, "tcp", s.Config.Address)
	}
	if e != nil {
		return e
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if e = conn.SetDeadline(deadline); e != nil {
		return e
	}
	client, e := smtp.NewClient(conn, host)
	if e != nil {
		return e
	}
	defer client.Close()
	if s.Config.TLS == "starttls" {
		if e = client.StartTLS(tlsConfig); e != nil {
			return e
		}
	} else if s.Config.TLS != "tls" {
		return fault.New("MAIL_UNAVAILABLE", "邮件连接必须使用TLS")
	}
	if e = client.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, host)); e != nil {
		return e
	}
	if e = client.Mail(from.Address); e != nil {
		return e
	}
	if e = client.Rcpt(to.Address); e != nil {
		return e
	}
	body, e := client.Data()
	if e != nil {
		return e
	}
	_, e = fmt.Fprintf(body, "From: %s\r\nTo: %s\r\nSubject: Drama Agent verification code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour %s verification code: %s\r\nValid for 10 minutes. Do not share this code.\r\n", from.Address, to.Address, purpose, code)
	if e != nil {
		return e
	}
	if e = body.Close(); e != nil {
		return e
	}
	// DATA 已被对端确认；关闭 SMTP 会话不能撤回这次提交。
	return nil
}

// Transport 只消费运行配置服务提供的不可变投递快照；不读 YAML 回退，不缓存凭证。
type Transport struct{ roots *x509.CertPool }

func (t Transport) DeliverCode(ctx context.Context, config mailconfig.Delivery, to, purpose, code string) error {
	return (SMTP{Config: config, roots: t.roots}).SendCode(ctx, to, purpose, code)
}
