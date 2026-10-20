package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

// ModelCipher 的密钥独立于 PG 和项目保存；结构重构不能更换原密钥或密文。
type ModelCipher struct{ aead cipher.AEAD }

func OpenModelCipher(path string, create bool) (*ModelCipher, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, errors.New("existing model credential requires its original encryption key file")
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		raw = make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return nil, err
		}
		file, e := os.CreateTemp(filepath.Dir(path), ".model-key-*")
		if e != nil {
			return nil, e
		}
		name := file.Name()
		defer os.Remove(name)
		_, e = file.Write(raw)
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		// 发布完整密钥且不覆盖其他进程刚创建的文件，避免并发生成不一致。
		if e = os.Link(name, path); errors.Is(e, os.ErrExist) {
			return OpenModelCipher(path, create)
		}
		if e != nil {
			return nil, e
		}
	} else if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 != 0 || len(raw) != 32 {
		return nil, errors.New("model encryption key requires 32 bytes and private file permissions")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &ModelCipher{aead: aead}, err
}

// 沿用原 AAD，确保已有供应商密文仍可解密；包名与展示名不参与加密身份。
func (c *ModelCipher) Seal(value string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(value), []byte("drama-agent:deepseek")), nil
}

func (c *ModelCipher) Open(value []byte) (string, error) {
	n := c.aead.NonceSize()
	if len(value) < n {
		return "", errors.New("invalid encrypted model credential")
	}
	raw, err := c.aead.Open(nil, value[:n], value[n:], []byte("drama-agent:deepseek"))
	return string(raw), err
}

// ConfigurationCipher reuses the original key. SMTP secrets also prevent
// accidentally creating a replacement key when there are no model credentials.
func (s *Store) ConfigurationCipher(ctx context.Context, path string) (*ModelCipher, error) {
	models, err := s.ModelConfigSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	mail, err := s.MailConfigSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	cipher, err := OpenModelCipher(path, !models.HasCredentials() && len(mail.PasswordCipher) == 0)
	if err != nil {
		return nil, err
	}
	for _, connection := range models.Connections {
		if len(connection.Secret) > 0 {
			if _, err = cipher.Open(connection.Secret); err != nil {
				return nil, errors.New("cannot decrypt stored configuration credential")
			}
		}
	}
	if len(mail.PasswordCipher) > 0 {
		if _, err = cipher.Open(mail.PasswordCipher); err != nil {
			return nil, errors.New("cannot decrypt stored configuration credential")
		}
	}
	return cipher, nil
}
