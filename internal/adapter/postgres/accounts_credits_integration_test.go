package postgres_test

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	a "drama-agent/internal/application/accounts"
	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	taskdomain "drama-agent/internal/domain/task"
	"drama-agent/internal/testsupport/pgfixture"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func identityDB(t *testing.T) (*postgres.Store, *pgx.Conn) {
	t.Helper()
	database := os.Getenv("DRAMA_TEST_DATABASE_URL")
	if database == "" {
		path := os.Getenv("DRAMA_TEST_CONFIG_FILE")
		if path == "" {
			t.Skip("isolated PG configuration required")
		}
		cfg, err := appconfig.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		database = cfg.Database.URL
	}
	u, _ := url.Parse(database)
	if !strings.HasPrefix(u.Path, "/drama_accounts_test_") {
		t.Skip("identity tests require their dedicated database")
	}
	ctx := context.Background()
	s, e := pgfixture.Open(t, ctx, database)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	conn, e := pgx.Connect(ctx, database)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return s, conn
}
func codeFor(t *testing.T, s *postgres.Store, email, purpose, code string, until time.Time) {
	t.Helper()
	id := a.RandomToken()
	if e := s.PutVerification(context.Background(), id, email, purpose, a.Digest(id+":"+code), until); e != nil {
		t.Fatal(e)
	}
	if e := s.ActivateVerification(context.Background(), id, true); e != nil {
		t.Fatal(e)
	}
}
func createUser(t *testing.T, s *postgres.Store, email string) a.User {
	t.Helper()
	ctx := context.Background()
	v, raw, e := (&a.Service{Store: s}).CreateCode(ctx, ports.Actor{UserID: "test-operator", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	_ = v
	codeFor(t, s, email, "register", "312456", time.Now().Add(time.Minute))
	u, e := (&a.Service{Store: s, Hasher: passwordhash.Bcrypt{}}).Register(ctx, email, "测试用户", "test-password-at-least-12", "312456", raw)
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func codeIs(err error, code string) bool {
	var f *fault.Error
	return errors.As(err, &f) && f.Code == code
}
func TestAccountsPGTransactionsSessionsAndCredits(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	service := &a.Service{Store: s, Hasher: passwordhash.Bcrypt{}}
	prefix := identity.New("identity")
	user := createUser(t, s, prefix+"@example.test")
	other := createUser(t, s, prefix+"b@example.test")
	var spaces int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.spaces WHERE kind='personal' AND id=ANY($1)`, []string{user.ID, other.ID}).Scan(&spaces); err != nil || spaces != 2 {
		t.Fatal("registration did not create personal spaces", spaces, err)
	}
	t.Run("password-and-single-session", func(t *testing.T) {
		first, e := service.Login(ctx, user.Email, "test-password-at-least-12", "", "workspace")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = service.Login(ctx, user.Email, "wrong-password", "", "workspace"); !codeIs(e, "INVALID_CREDENTIALS") {
			t.Fatal(e)
		}
		if _, e = service.Login(ctx, user.Email, "test-password-at-least-12", "", "operations"); !codeIs(e, "ROLE_FORBIDDEN") {
			t.Fatal(e)
		}
		if _, e = service.Authenticate(ctx, first.Token); e != nil {
			t.Fatal("failed login invalidated old session", e)
		}
		var wg sync.WaitGroup
		tokens := make(chan string, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, e := service.Login(ctx, user.Email, "test-password-at-least-12", "", "workspace")
				if e != nil {
					t.Error(e)
					return
				}
				tokens <- v.Token
			}()
		}
		wg.Wait()
		close(tokens)
		valid := 0
		newest := ""
		for token := range tokens {
			if _, e := service.Authenticate(ctx, token); e == nil {
				valid++
				newest = token
			}
		}
		if valid != 1 {
			t.Fatal("effective sessions", valid)
		}
		if e = s.Logout(ctx, first.Token); e != nil {
			t.Fatal(e)
		}
		if _, e = service.Authenticate(ctx, newest); e != nil {
			t.Fatal("old logout removed replacement", e)
		}
		if _, e = s.SetUserEnabled(ctx, user.ID, false); e != nil {
			t.Fatal(e)
		}
		if _, e = service.Authenticate(ctx, newest); !codeIs(e, "ACCOUNT_DISABLED") {
			t.Fatal(e)
		}
		s.SetUserEnabled(ctx, user.ID, true)
	})
	t.Run("code-login-consumes-once-and-does-not-register", func(t *testing.T) {
		codeFor(t, s, other.Email, "login", "624812", time.Now().Add(time.Minute))
		var wg sync.WaitGroup
		var success atomic.Int32
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, e := service.Login(ctx, other.Email, "", "624812", "workspace"); e == nil {
					success.Add(1)
				}
			}()
		}
		wg.Wait()
		if success.Load() != 1 {
			t.Fatal(success.Load())
		}
		missing := prefix + "-missing@example.test"
		codeFor(t, s, missing, "login", "624812", time.Now().Add(time.Minute))
		if _, e := service.Login(ctx, missing, "", "624812", "workspace"); e == nil {
			t.Fatal("code login created user")
		}
	})
	t.Run("code-purpose-expiry-attempt-limit-reset", func(t *testing.T) {
		email := prefix + "reset@example.test"
		u := createUser(t, s, email)
		login, e := service.Login(ctx, email, "test-password-at-least-12", "", "workspace")
		if e != nil {
			t.Fatal(e)
		}
		codeFor(t, s, email, "reset", "812643", time.Now().Add(time.Minute))
		if _, e = service.Login(ctx, email, "", "812643", "workspace"); e == nil {
			t.Fatal("reset code accepted for login")
		}
		if e = service.ResetPassword(ctx, email, "812643", "new-test-password-123"); e != nil {
			t.Fatal(e)
		}
		if _, e = service.Authenticate(ctx, login.Token); e == nil {
			t.Fatal("reset kept session")
		}
		if _, e = service.Login(ctx, email, "new-test-password-123", "", "workspace"); e != nil {
			t.Fatal(e)
		}
		if e = service.ResetPassword(ctx, email, "812643", "third-password-1234"); e == nil {
			t.Fatal("code reused")
		}
		_ = u
		expired := prefix + "expired@example.test"
		codeFor(t, s, expired, "register", "812643", time.Now().Add(-time.Minute))
		if _, e = service.Register(ctx, expired, "测试用户", "new-test-password-123", "812643", ""); !codeIs(e, "INVALID_VERIFICATION_CODE") {
			t.Fatal(e)
		}
		attempts := prefix + "attempts@example.test"
		codeFor(t, s, attempts, "register", "812643", time.Now().Add(time.Minute))
		for i := 0; i < 5; i++ {
			service.Register(ctx, attempts, "测试用户", "new-test-password-123", "000000", "")
		}
		if _, e = service.Register(ctx, attempts, "测试用户", "new-test-password-123", "812643", ""); !codeIs(e, "INVALID_VERIFICATION_CODE") {
			t.Fatal("attempt limit", e)
		}
	})
	t.Run("authorization-code-single-use-atomic-rollback", func(t *testing.T) {
		_, raw, e := service.CreateCode(ctx, ports.Actor{UserID: "test-operator", Role: ports.RoleOperator}, time.Now().Add(time.Hour))
		if e != nil {
			t.Fatal(e)
		}
		emails := []string{prefix + "c@example.test", prefix + "d@example.test"}
		for _, email := range emails {
			codeFor(t, s, email, "register", "124863", time.Now().Add(time.Minute))
		}
		var wg sync.WaitGroup
		var count atomic.Int32
		for _, email := range emails {
			wg.Add(1)
			go func(email string) {
				defer wg.Done()
				if _, e := service.Register(ctx, email, "测试用户", "test-password-at-least-12", "124863", raw); e == nil {
					count.Add(1)
				}
			}(email)
		}
		wg.Wait()
		if count.Load() != 1 {
			t.Fatal(count.Load())
		}
		var n int
		if e = conn.QueryRow(ctx, `SELECT count(*) FROM drama_catalog_v1.users WHERE email=ANY($1)`, emails).Scan(&n); e != nil || n != 1 {
			t.Fatal(n, e)
		}
	})
	t.Run("operator-init-does-not-reset-or-promote", func(t *testing.T) {
		u, _, e := service.InitializeOperator(ctx, "fixture-operator-password-123")
		if e != nil {
			t.Fatal(e)
		}
		var before []byte
		if e = conn.QueryRow(ctx, `SELECT password_hash FROM drama_catalog_v1.users WHERE id=$1`, u.ID).Scan(&before); e != nil {
			t.Fatal(e)
		}
		_, created, e := service.InitializeOperator(ctx, "another-fixture-password-123")
		if e != nil || created {
			t.Fatal(created, e)
		}
		var after []byte
		conn.QueryRow(ctx, `SELECT password_hash FROM drama_catalog_v1.users WHERE id=$1`, u.ID).Scan(&after)
		if string(before) != string(after) {
			t.Fatal("password overwritten")
		}
		if _, e = conn.Exec(ctx, `UPDATE drama_catalog_v1.users SET role='user' WHERE id=$1`, u.ID); e != nil {
			t.Fatal(e)
		}
		defer conn.Exec(context.Background(), `UPDATE drama_catalog_v1.users SET role='operator' WHERE id=$1`, u.ID)
		if _, _, e = service.InitializeOperator(ctx, "another-fixture-password-123"); !codeIs(e, "INITIAL_IDENTITY_CONFLICT") {
			t.Fatal("conflict promoted user", e)
		}
		var role string
		conn.QueryRow(ctx, `SELECT role FROM drama_catalog_v1.users WHERE id=$1`, u.ID).Scan(&role)
		if role != "user" {
			t.Fatal("automatic promotion", role)
		}

		if _, e = service.Register(ctx, a.InitialOperatorEmail, "测试用户", "test-password-at-least-12", "123456", ""); !codeIs(e, "REGISTRATION_FORBIDDEN") {
			t.Fatal(e)
		}
	})
	t.Run("credits-concurrency-idempotency-and-refund-upper-bound", func(t *testing.T) {
		grant := c.Change{Category: c.Gift, AccountID: user.ID, Amount: 100, OperationID: prefix + "grant", Source: "fixture", Subject: "operator", Reason: "fixture"}
		receipt, e := s.Add(ctx, grant)
		if e != nil {
			t.Fatal(e)
		}
		again, e := s.Add(ctx, grant)
		if e != nil || again.ID != receipt.ID {
			t.Fatal(again, e)
		}
		grant.Amount = 101
		if _, e = s.Add(ctx, grant); !codeIs(e, "CREDIT_IDEMPOTENCY_CONFLICT") {
			t.Fatal(e)
		}
		var count atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, e := s.Deduct(ctx, c.Change{AccountID: user.ID, Amount: 10, OperationID: identity.New("debit"), Source: "fixture", Subject: user.ID, Reason: "fixture"}); e == nil {
					count.Add(1)
				}
			}()
		}
		wg.Wait()
		if count.Load() != 10 {
			t.Fatal("overspend", count.Load())
		}
		v, e := s.Balance(ctx, user.ID)
		if e != nil || v != 0 {
			t.Fatal(v, e)
		}
		debit, e := s.Deduct(ctx, c.Change{AccountID: other.ID, Amount: 1, OperationID: prefix + "insufficient", Source: "fixture", Subject: other.ID, Reason: "fixture"})
		if !codeIs(e, "CREDIT_INSUFFICIENT") {
			t.Fatal(debit, e)
		}
		s.Add(ctx, c.Change{Category: c.Gift, AccountID: other.ID, Amount: 50, OperationID: prefix + "bgrant", Source: "fixture", Subject: "operator", Reason: "fixture"})
		debit, e = s.Deduct(ctx, c.Change{AccountID: other.ID, Amount: 50, OperationID: prefix + "bdebit", Source: "fixture", Subject: other.ID, Reason: "fixture"})
		if e != nil {
			t.Fatal(e)
		}
		count.Store(0)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, e := s.Refund(ctx, c.Change{OriginalDeductionID: debit.ID, Amount: 10, OperationID: identity.New("refund"), Subject: other.ID, Reason: "fixture"}); e == nil {
					count.Add(1)
				}
			}()
		}
		wg.Wait()
		if count.Load() != 5 {
			t.Fatal("over refund", count.Load())
		}
		entries, n, e := s.CreditEntries(ctx, other.ID, 1, 100)
		if e != nil || n != 7 {
			t.Fatal(n, e)
		}
		var signed int64
		for _, v := range entries {
			if v.Kind == "deduct" {
				signed -= v.Amount
			} else {
				signed += v.Amount
			}
		}
		balance, _ := s.Balance(ctx, other.ID)
		if signed != balance || balance != 50 {
			t.Fatal(signed, balance)
		}
		raw, _ := json.Marshal(entries)
		if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "session") {
			t.Fatal("credential in ledger")
		}
	})
}

func TestTaskOwnerStoreIsolationAndWorkerOwner(t *testing.T) {
	s, conn := identityDB(t)
	ctx := context.Background()
	tasks := postgres.NewTaskStore(s)
	scope := identity.New("task_owner")
	a, b, op := "owner-"+scope, "other-"+scope, "operator-"+scope
	for _, space := range []string{a, b, op} {
		if _, err := conn.Exec(ctx, `INSERT INTO drama_catalog_v1.spaces(id,kind) VALUES($1,'personal')`, space); err != nil {
			t.Fatal(err)
		}
	}
	create := func(owner string) (taskdomain.Task, error) {
		return tasks.ForTaskSpace(owner).CreateTask(ctx, taskdomain.Task{SpaceID: owner, CreatorID: owner, OwnerID: owner, ID: identity.New("task"), ScopeID: scope, CommandID: "same-command", Status: taskdomain.Queued, Snapshot: taskdomain.Snapshot{Input: json.RawMessage(`{"private":"fixture"}`)}, State: taskdomain.State{Operation: "image.generate", ModelID: "mock-image", ActiveKey: "same-target", CommandDigest: "same-digest"}})
	}
	first, e := create(a)
	if e != nil {
		t.Fatal(e)
	}
	second, e := create(b)
	if e != nil {
		t.Fatal("owners collided", e)
	}
	for _, owner := range []string{b, op} {
		store := tasks.ForTaskSpace(owner)
		if _, e = store.Task(ctx, first.ID); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("foreign task", e)
		}
		if _, e = store.TaskInput(ctx, first.ID); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("foreign input", e)
		}
		if _, e = store.CancelTask(ctx, scope, first.ID, first.Revision); !errors.Is(e, fault.ErrNotFound) {
			t.Fatal("foreign cancel", e)
		}
	}
	rows, e := tasks.ForTaskSpace(b).ListTasks(ctx, scope, "", nil, 100)
	if e != nil || len(rows) != 1 || rows[0].ID != second.ID {
		t.Fatal(rows, e)
	}
	another := taskdomain.Task{SpaceID: a, CreatorID: a, OwnerID: a, ID: identity.New("task"), ScopeID: scope, CommandID: "different-command", Status: taskdomain.Queued, Snapshot: taskdomain.Snapshot{Input: json.RawMessage(`{}`)}, State: taskdomain.State{Operation: "image.generate", ActiveKey: "same-target"}}
	if _, e = tasks.ForTaskSpace(a).CreateTask(ctx, another); !codeIs(e, "TASK_ACTIVE") {
		t.Fatal("active target not protected", e)
	}
	claimed, ok, e := tasks.ClaimTask(ctx, first.ID, "worker-token", time.Minute)
	if e != nil || !ok || claimed.OwnerID != a || claimed.SpaceID != a || claimed.CreatorID != a {
		t.Fatal(claimed.OwnerID, ok, e)
	}
	changed := claimed
	changed.OwnerID = b
	if _, e = tasks.CommitTask(ctx, changed, claimed.Revision, claimed.LeaseToken, true); e == nil {
		t.Fatal("worker transferred owner")
	}
	claimed.State.ErrorCode = "FIXTURE"
	committed, e := tasks.CommitTask(ctx, claimed, claimed.Revision, claimed.LeaseToken, true)
	if e != nil {
		t.Fatal("trusted owner recovery", e)
	}
	cancelled, e := tasks.ForTaskSpace(a).CancelTask(ctx, scope, committed.ID, committed.Revision)
	if e != nil || cancelled.Status != taskdomain.Cancelled || cancelled.OwnerID != a {
		t.Fatal("owned cancel", cancelled.Status, e)
	}
	untouched, e := tasks.ForTaskSpace(b).Task(ctx, second.ID)
	if e != nil || untouched.Status != taskdomain.Queued {
		t.Fatal("other task changed", e)
	}
}
