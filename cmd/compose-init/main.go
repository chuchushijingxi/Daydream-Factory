// Command compose-init initializes only this Compose project's private
// configuration volume, and switches it out of demo mode before going live.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var secretNames = []string{"app.local.yaml", "postgres-password", "minio-password", "minio-user"}

func main() {
	root := flag.String("root", "/runtime", "mounted runtime-config volume")
	production := flag.Bool("production", false, "switch an existing configuration to production and close the demo checkout")
	flag.Parse()
	run, failure := initialize, "Cannot initialize Compose configuration; check or restore its original private volume."
	if *production {
		run, failure = disableDemo, "Cannot switch the Compose configuration to production; check its private volume."
	}
	message, err := run(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, failure)
		os.Exit(1)
	}
	fmt.Println(message)
}

func private(path string, allowEmpty bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || (!allowEmpty && info.Size() == 0) {
		return errors.New("secret is not private")
	}
	return nil
}

func secret() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// createPrivate writes a new 0600 file and syncs it; an existing file is an error.
func createPrivate(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.WriteString(value); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// replacePrivate atomically replaces a file in the same directory.
func replacePrivate(path, value string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".config-update-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.WriteString(value); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func validRedisSecret(password string) bool {
	if len(password) != 48 {
		return false
	}
	_, err := hex.DecodeString(password)
	return err == nil && strings.ToLower(password) == password
}

func addRedis(target string) error {
	config := filepath.Join(target, "app.local.yaml")
	raw, err := os.ReadFile(config)
	if err != nil {
		return err
	}
	passwordFile := filepath.Join(target, "redis-password")
	if strings.Contains("\n"+string(raw), "\nredis:") {
		return private(passwordFile, false)
	}
	var password string
	if _, err = os.Lstat(passwordFile); err == nil {
		if err = private(passwordFile, true); err != nil {
			return err
		}
		existing, readErr := os.ReadFile(passwordFile)
		if readErr != nil {
			return readErr
		}
		if password = string(existing); !validRedisSecret(password) {
			return errors.New("incomplete Redis upgrade")
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		if password, err = secret(); err != nil {
			return err
		}
		if err = createPrivate(passwordFile, password); err != nil {
			return err
		}
	} else {
		return err
	}
	return replacePrivate(config, strings.TrimRight(string(raw), " \t\r\n")+
		"\nredis:\n  address: redis:6379\n  password: "+password+"\n  namespace: daydream-factory:compose\n")
}

func initialize(root string) (string, error) {
	target := filepath.Join(root, "config")
	if _, err := os.Lstat(target); err == nil {
		// 已有配置不能通过补造密码修复，否则会与已有 PG/S3 事实分离。
		for _, name := range secretNames {
			if err = private(filepath.Join(target, name), false); err != nil {
				return "", err
			}
		}
		if err = addRedis(target); err != nil {
			return "", err
		}
		return "Existing PG/S3 secrets preserved; Redis configuration ready.", nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	// 配置与基础设施凭证一起原子发布；中断目录不会被当作有效配置。
	staging, err := os.MkdirTemp(root, ".config-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	databasePassword, err := secret()
	if err != nil {
		return "", err
	}
	storagePassword, err := secret()
	if err != nil {
		return "", err
	}
	// 开源模板默认可体验完整流程：演示收银台开启（不真实扣款）。只写入新部署的配置，已有部署不被改动。
	// 正式上线前用 -production 改为正式环境并关闭演示收银台，见 README「上线前」。
	config := "environment: development\npayment:\n  demo:\n    enabled: true\n" +
		"database:\n  url: postgres://drama:" + databasePassword + "@postgres:5432/drama_agent?sslmode=disable\n" +
		"nsq:\n  address: 127.0.0.1:4150\n  topic: drama.tasks\n  channel: workers\n" +
		"storage:\n  endpoint: http://minio:9000\n  bucket: drama-agent\n  accessKey: drama\n  secretKey: " + storagePassword + "\n"
	values := []string{config, databasePassword, storagePassword, "drama"}
	for i, name := range secretNames {
		if err = createPrivate(filepath.Join(staging, name), values[i]); err != nil {
			return "", err
		}
	}
	if err = os.Rename(staging, target); err != nil {
		return "", err
	}
	if err = addRedis(target); err != nil {
		return "", err
	}
	return "Private Compose configuration initialized; credentials are not logged.", nil
}

// disableDemo 把运行环境改为 production 并关闭演示收银台，不触碰任何凭证。
func disableDemo(root string) (string, error) {
	config := filepath.Join(root, "config", "app.local.yaml")
	if err := private(config, false); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(config)
	if err != nil {
		return "", err
	}
	text := strings.Replace(string(raw), "environment: development", "environment: production", 1)
	text = strings.Replace(text, "  demo:\n    enabled: true", "  demo:\n    enabled: false", 1)
	if text != string(raw) {
		if err = replacePrivate(config, text); err != nil {
			return "", err
		}
	}
	return "Compose configuration is in production mode; the demo checkout is closed.", nil
}
