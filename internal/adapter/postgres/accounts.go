package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	a "drama-agent/internal/application/accounts"
	c "drama-agent/internal/application/credits"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const userColumns = `id,email,username,role,CASE WHEN enabled THEN 'enabled' ELSE 'disabled' END,created_at`

func scanUser(row pgx.Row) (a.User, error) {
	var u a.User
	e := row.Scan(&u.ID, &u.Email, &u.Username, &u.Role, &u.Status, &u.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = fault.ErrNotFound
	}
	return u, e
}
func (s *Store) RegistrationRequired(ctx context.Context) (bool, error) {
	var v bool
	e := s.pool.QueryRow(ctx, `SELECT requires_code FROM drama_catalog_v1.registration_settings WHERE id`).Scan(&v)
	return v, e
}
func (s *Store) PutVerification(ctx context.Context, id, email, purpose, digest string, until time.Time) error {
	runtime, e := s.runtimeState()
	if e != nil {
		return e
	}
	for _, rule := range []struct {
		key    string
		limit  int
		window time.Duration
	}{{"email-minute:" + email + ":" + purpose, 1, time.Minute}, {"email-hour:" + email, 10, time.Hour}} {
		allowed, err := runtime.Allow(ctx, rule.key, rule.limit, rule.window)
		if err != nil {
			return err
		}
		if !allowed {
			return fault.New("AUTH_RATE_LIMITED", "验证码发送过于频繁")
		}
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	_, e = tx.Exec(ctx, `INSERT INTO drama_catalog_v1.email_verifications(id,email,purpose,digest,expires_at) VALUES($1,$2,$3,$4,$5)`, id, email, purpose, digest, until)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) ActivateVerification(ctx context.Context, id string, active bool) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var email, purpose string
	e = tx.QueryRow(ctx, `SELECT email,purpose FROM drama_catalog_v1.email_verifications WHERE id=$1 FOR UPDATE`, id).Scan(&email, &purpose)
	if e != nil {
		return e
	}
	if active {
		_, e = tx.Exec(ctx, `UPDATE drama_catalog_v1.email_verifications SET consumed=true WHERE email=$1 AND purpose=$2 AND id<>$3`, email, purpose, id)
		if e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `UPDATE drama_catalog_v1.email_verifications SET active=$2,consumed=NOT $2 WHERE id=$1`, id, active)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func verifyCode(ctx context.Context, tx pgx.Tx, email, purpose, code string) error {
	var id, digest string
	var attempts int
	var until time.Time
	e := tx.QueryRow(ctx, `SELECT id,digest,attempts,expires_at FROM drama_catalog_v1.email_verifications WHERE email=$1 AND purpose=$2 AND active AND NOT consumed ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, email, purpose).Scan(&id, &digest, &attempts, &until)
	invalid := fault.New("INVALID_VERIFICATION_CODE", "验证码无效或已过期")
	if errors.Is(e, pgx.ErrNoRows) {
		return invalid
	}
	if e != nil {
		return e
	}
	if attempts >= 5 || !time.Now().Before(until) {
		return invalid
	}
	match := len(code) == 6 && subtle.ConstantTimeCompare([]byte(a.Digest(id+":"+code)), []byte(digest)) == 1
	_, e = tx.Exec(ctx, `UPDATE drama_catalog_v1.email_verifications SET attempts=attempts+1,consumed=$2 WHERE id=$1`, id, match)
	if e != nil {
		return e
	}
	if !match {
		return invalid
	}
	return nil
}
func commitVerificationError(ctx context.Context, tx pgx.Tx, e error) error {
	var f *fault.Error
	if errors.As(e, &f) && f.Code == "INVALID_VERIFICATION_CODE" {
		if ce := tx.Commit(ctx); ce != nil {
			return ce
		}
	}
	return e
}

type registrationTx struct {
	store        *Store
	tx           pgx.Tx
	requiresCode bool
}

func (s *Store) WithRegistration(ctx context.Context, register func(a.RegistrationTx) (a.User, error)) (a.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return a.User{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	scope := &registrationTx{store: s, tx: tx}
	if err = tx.QueryRow(ctx, `SELECT requires_code FROM drama_catalog_v1.registration_settings WHERE id FOR SHARE`).Scan(&scope.requiresCode); err != nil {
		return a.User{}, err
	}
	user, err := register(scope)
	if err != nil {
		return a.User{}, commitVerificationError(ctx, tx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return a.User{}, fault.New("REGISTRATION_COMMIT_UNKNOWN", "注册提交结果未确认，请尝试登录核对账号及积分，勿重复注册")
	}
	return user, nil
}

func (r *registrationTx) CreditGiftPolicy(ctx context.Context) (c.GiftPolicy, error) {
	return giftPolicyTx(ctx, r.tx)
}

func (r *registrationTx) RecordCreditGift(ctx context.Context, grant c.GiftGrant) error {
	return r.store.grantCreditGiftTx(ctx, r.tx, grant.UserID, grant.Kind, grant.Period, grant.Amount, grant.PolicyRevision)
}

func (r *registrationTx) CreateUser(ctx context.Context, in a.Registration) (a.User, error) {
	if err := verifyCode(ctx, r.tx, in.Email, "register", in.Code); err != nil {
		return a.User{}, err
	}
	var authorization a.AuthorizationCode
	if r.requiresCode {
		var accepted time.Time
		err := r.tx.QueryRow(ctx, `SELECT `+authorizationColumns+`,now() FROM drama_catalog_v1.authorization_codes WHERE digest=$1 FOR UPDATE`, a.Digest(in.AuthorizationCode)).Scan(&authorization.ID, &authorization.CreatedAt, &authorization.ExpiresAt, &authorization.Disabled, &authorization.UsedBy, &authorization.UsedAt, &accepted)
		if errors.Is(err, pgx.ErrNoRows) {
			return a.User{}, fault.New("AUTHORIZATION_CODE_INVALID", "注册授权码无效")
		}
		if err != nil {
			return a.User{}, err
		}
		if err = authorization.RequireAvailable(accepted); err != nil {
			return a.User{}, err
		}
	}
	user, err := scanUser(r.tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$4,'user',$3) RETURNING `+userColumns, in.ID, in.Email, in.PasswordHash, in.Username))
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return user, fault.New("EMAIL_EXISTS", "邮箱已注册")
	}
	if err != nil {
		return user, err
	}
	if r.requiresCode {
		_, err = r.tx.Exec(ctx, `UPDATE drama_catalog_v1.authorization_codes SET used_by=$2,used_at=now() WHERE id=$1`, authorization.ID, user.ID)
	}
	return user, err
}
func (s *Store) Login(ctx context.Context, in a.LoginCredentials) (a.User, error) {
	email, password, code, audience := in.Email, in.Password, in.Code, in.Audience
	if !a.ValidAudience(audience) {
		return a.User{}, fault.ErrInvalid
	}
	runtime, e := s.runtimeState()
	if e != nil {
		return a.User{}, e
	}
	allowed, e := runtime.Allow(ctx, "login:"+email, 10, 15*time.Minute)
	if e != nil {
		return a.User{}, e
	}
	if !allowed {
		return a.User{}, fault.New("AUTH_RATE_LIMITED", "登录尝试过于频繁")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return a.User{}, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	failLogin := func(err error) (a.User, error) {
		if ce := tx.Commit(ctx); ce != nil {
			return a.User{}, ce
		}
		return a.User{}, err
	}
	var u a.User
	var hash []byte
	var enabled bool
	e = tx.QueryRow(ctx, `SELECT id,email,username,role,enabled,created_at,password_hash FROM drama_catalog_v1.users WHERE email=$1 FOR UPDATE`, email).Scan(&u.ID, &u.Email, &u.Username, &u.Role, &enabled, &u.CreatedAt, &hash)
	invalid := fault.New("INVALID_CREDENTIALS", "账号或凭证不正确")
	if errors.Is(e, pgx.ErrNoRows) {
		return failLogin(invalid)
	}
	if e != nil {
		return a.User{}, e
	}
	if code == "" && bcrypt.CompareHashAndPassword(hash, password) != nil {
		return failLogin(invalid)
	}
	if code != "" {
		if e = verifyCode(ctx, tx, email, "login", code); e != nil {
			return failLogin(e)
		}
	}
	if !enabled {
		return failLogin(fault.New("ACCOUNT_DISABLED", "账号已禁用"))
	}
	if audience == "operations" && u.Role != ports.RoleOperator {
		return failLogin(fault.New("ROLE_FORBIDDEN", "普通账号不能登录运营后台"))
	}
	// Keep the account row locked through Redis replacement and PG commit. A disable/reset
	// cannot commit between credential validation and session installation.
	replaced, e := runtime.PutSession(ctx, u.ID, audience, in.SessionDigest, u.Role, in.ExpiresAt)
	if e != nil {
		return a.User{}, e
	}
	rollbackSession := func() {
		_, _ = runtime.DeleteSessionDigest(context.WithoutCancel(ctx), u.ID, audience, in.SessionDigest)
	}
	kind := "login"
	if replaced {
		kind = "replaced"
	}
	if e = sessionEvent(ctx, tx, u.ID, audience, kind); e != nil {
		rollbackSession()
		return a.User{}, e
	}
	if e = runtime.ResetLimit(ctx, "login:"+email); e != nil {
		rollbackSession()
		return a.User{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		rollbackSession()
		return a.User{}, e
	}
	u.Status = "enabled"
	return u, nil
}

func (s *Store) ResetPassword(ctx context.Context, email, code string, hash []byte) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM drama_catalog_v1.users WHERE email=$1 FOR UPDATE`, email).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return fault.New("INVALID_VERIFICATION_CODE", "验证码无效或已过期")
	}
	if e != nil {
		return e
	}
	if e = verifyCode(ctx, tx, email, "reset", code); e != nil {
		return commitVerificationError(ctx, tx, e)
	}
	_, e = tx.Exec(ctx, `UPDATE drama_catalog_v1.users SET password_hash=$2 WHERE id=$1`, id, hash)
	if e == nil {
		e = s.revokeSessions(ctx, tx, id, "password_reset")
	}
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) CurrentUser(ctx context.Context, id string) (a.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM drama_catalog_v1.users WHERE id=$1`, id))
}
func (s *Store) InitializeOperator(ctx context.Context, id string, hash []byte) (a.User, bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return a.User{}, false, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:operator-init',0))`); e != nil {
		return a.User{}, false, e
	}
	u, e := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM drama_catalog_v1.users WHERE email=$1`, a.InitialOperatorEmail))
	if e == nil {
		if u.Role != ports.RoleOperator {
			return u, false, fault.New("INITIAL_IDENTITY_CONFLICT", "初始化身份已存在且不是运营账号")
		}
		return u, false, tx.Commit(ctx)
	}
	if !errors.Is(e, fault.ErrNotFound) {
		return u, false, e
	}
	u, e = scanUser(tx.QueryRow(ctx, `INSERT INTO drama_catalog_v1.users(id,email,username,role,password_hash) VALUES($1,$2,$4,'operator',$3) RETURNING `+userColumns, id, a.InitialOperatorEmail, hash, a.InitialOperatorUsername))
	if e != nil {
		return u, false, e
	}
	e = tx.Commit(ctx)
	return u, e == nil, e
}
func (s *Store) ListUsers(ctx context.Context, q string, page, size int) (ports.Page[a.User], error) {
	out := ports.Page[a.User]{Items: []a.User{}, Page: page, PageSize: size}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	e = tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.users WHERE strpos(email,$1)>0`, strings.ToLower(q)).Scan(&out.Total)
	if e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT `+userColumns+` FROM drama_catalog_v1.users WHERE strpos(email,$1)>0 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, strings.ToLower(q), size, (page-1)*size)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, u)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) SetUserEnabled(ctx context.Context, id string, enabled bool) (a.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return a.User{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	u, err := scanUser(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.users SET enabled=$2 WHERE id=$1 RETURNING `+userColumns, id, enabled))
	if err != nil {
		return a.User{}, err
	}
	if !enabled {
		if err = s.revokeSessions(ctx, tx, id, "disabled"); err != nil {
			return a.User{}, err
		}
	}
	runtime, err := s.runtimeState()
	if err != nil {
		return a.User{}, err
	}
	if err = runtime.SetSessionsDisabled(ctx, id, !enabled); err != nil {
		return a.User{}, err
	}
	return u, tx.Commit(ctx)
}
func (s *Store) SetRegistrationRequired(ctx context.Context, v bool) error {
	_, e := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.registration_settings SET requires_code=$1 WHERE id`, v)
	return e
}

const authorizationColumns = `id,created_at,expires_at,disabled,used_by,used_at`

func scanAuthorization(row pgx.Row) (a.AuthorizationCode, error) {
	var v a.AuthorizationCode
	e := row.Scan(&v.ID, &v.CreatedAt, &v.ExpiresAt, &v.Disabled, &v.UsedBy, &v.UsedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		e = fault.ErrNotFound
	}
	return v, e
}
func (s *Store) CreateAuthorizationCode(ctx context.Context, id, digest string, until time.Time) (a.AuthorizationCode, error) {
	return scanAuthorization(s.pool.QueryRow(ctx, `INSERT INTO drama_catalog_v1.authorization_codes(id,digest,expires_at) VALUES($1,$2,$3) RETURNING `+authorizationColumns, id, digest, until))
}
func (s *Store) ListAuthorizationCodes(ctx context.Context, page, size int) (ports.Page[a.AuthorizationCode], error) {
	out := ports.Page[a.AuthorizationCode]{Items: []a.AuthorizationCode{}, Page: page, PageSize: size}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	e = tx.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.authorization_codes`).Scan(&out.Total)
	if e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT `+authorizationColumns+` FROM drama_catalog_v1.authorization_codes ORDER BY created_at DESC,id DESC LIMIT $1 OFFSET $2`, size, (page-1)*size)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		v, err := scanAuthorization(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) DisableAuthorizationCode(ctx context.Context, id string) error {
	r, e := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.authorization_codes SET disabled=true WHERE id=$1`, id)
	if e == nil && r.RowsAffected() != 1 {
		return fault.ErrNotFound
	}
	return e
}
