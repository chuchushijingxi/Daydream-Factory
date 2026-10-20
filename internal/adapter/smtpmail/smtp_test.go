package smtpmail

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"drama-agent/internal/application/mailconfig"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestSMTPActualTLSSubmissionAndRejectedRecipient(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "submitted", true: "recipient-rejected"}[reject], func(t *testing.T) {
			listener, e := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			bodies := make(chan string, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				client := textproto.NewConn(conn)
				client.PrintfLine("220 fixture ESMTP")
				for {
					line, e := client.ReadLine()
					if e != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						client.PrintfLine("250-fixture")
						client.PrintfLine("250 AUTH PLAIN")
					case strings.HasPrefix(line, "AUTH PLAIN"):
						client.PrintfLine("235 authenticated")
					case strings.HasPrefix(line, "MAIL FROM"):
						client.PrintfLine("250 accepted")
					case strings.HasPrefix(line, "RCPT TO"):
						if reject {
							client.PrintfLine("550 rejected")
						} else {
							client.PrintfLine("250 accepted")
						}
					case line == "DATA":
						client.PrintfLine("354 send message")
						raw, e := client.ReadDotBytes()
						if e != nil {
							return
						}
						bodies <- string(raw)
						client.PrintfLine("250 queued")
					case line == "QUIT":
						client.PrintfLine("221 bye")
						return
					default:
						client.PrintfLine("500 unexpected command")
					}
				}
			}()
			smtp := SMTP{Config: mailconfig.Delivery{Settings: mailconfig.Settings{Address: listener.Addr().String(), Username: "fixture", From: "sender@example.test", TLS: "tls"}, Password: "fixture-smtp-password"}, roots: roots}
			e = smtp.SendCode(context.Background(), "recipient@example.test", "register", "724816")
			if reject {
				if e == nil {
					t.Fatal("rejected mail reported submitted")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				select {
				case body := <-bodies:
					if !strings.Contains(body, "724816") || !strings.Contains(body, "register") {
						t.Fatal("missing actual code")
					}
				case <-time.After(time.Second):
					t.Fatal("message was not received")
				}
			}
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Fatal("SMTP connection was not closed")
			}
		})
	}
}
