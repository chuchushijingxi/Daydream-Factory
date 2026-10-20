// Package identity 提供跨业务上下文共用的随机身份原语，不承载项目业务。
package identity

import (
	"crypto/rand"
	"encoding/hex"
)

// New 保持既有前缀与16字节随机身份格式；名称或显示别名不能替代身份。
func New(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
