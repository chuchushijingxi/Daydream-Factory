package postgres

import (
	"context"
	"errors"
	"time"

	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/domain/fault"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func (s *Store) Profile(ctx context.Context, id string) (a.Profile, error) {
	var out a.Profile
	var digest *string
	u := &out.User
	err := s.pool.QueryRow(ctx, `SELECT `+userColumns+`,avatar_digest FROM drama_catalog_v1.users WHERE id=$1 AND enabled`, id).Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	if digest != nil {
		url := "/api/auth/avatar?v=" + *digest
		out.AvatarURL = &url
	}
	return out, err
}
func (s *Store) UpdateUsername(ctx context.Context, id, username string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET username=$2 WHERE id=$1 AND enabled`, id, username)
	if err == nil && tag.RowsAffected() != 1 {
		return fault.ErrNotFound
	}
	return err
}
func (s *Store) SetAvatar(ctx context.Context, id string, data []byte, digest string) error {
	var version *string
	if data != nil {
		version = &digest
	}
	tag, err := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.users SET avatar_data=$2,avatar_digest=$3 WHERE id=$1 AND enabled`, id, data, version)
	if err == nil && tag.RowsAffected() != 1 {
		return fault.ErrNotFound
	}
	return err
}
func (s *Store) Avatar(ctx context.Context, id string) ([]byte, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT avatar_data FROM drama_catalog_v1.users WHERE id=$1 AND enabled AND avatar_data IS NOT NULL`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return data, err
}

func (s *Store) ChangePassword(ctx context.Context, owner, token string, current, hash []byte) error {
	id, _, _, err := a.SessionParts(token)
	if err != nil {
		return err
	}
	if id != owner {
		return fault.New("AUTH_REQUIRED", "会话身份不一致")
	}
	runtime, err := s.runtimeState()
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var email string
	var old []byte
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT email,password_hash,enabled FROM drama_catalog_v1.users WHERE id=$1 FOR UPDATE`, id).Scan(&email, &old, &enabled); err != nil {
		return err
	}
	allowed, err := runtime.Allow(ctx, "login:"+email, 10, 15*time.Minute)
	if err != nil {
		return err
	}
	if !allowed {
		return fault.New("AUTH_RATE_LIMITED", "密码尝试过于频繁，请稍后再试")
	}
	if !enabled {
		return fault.New("ACCOUNT_DISABLED", "账号已禁用")
	}
	if _, err = runtime.Authenticate(ctx, token); err != nil {
		var rejected *fault.Error
		if errors.As(err, &rejected) && (rejected.Code == "SESSION_REPLACED" || rejected.Code == "SESSION_REVOKED" || rejected.Code == "SESSION_EXPIRED") {
			return fault.Wrap("SESSION_REVOKED", "会话已失效，请重新登录", err)
		}
		return err
	}
	if bcrypt.CompareHashAndPassword(old, current) != nil {
		return fault.New("CURRENT_PASSWORD_INVALID", "原密码不正确")
	}
	if err = s.revokeSessions(ctx, tx, id, "password_changed"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE drama_catalog_v1.users SET password_hash=$2 WHERE id=$1`, id, hash); err != nil {
		return err
	}
	if err = runtime.ResetLimit(ctx, "login:"+email); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
