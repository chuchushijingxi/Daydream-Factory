package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"drama-agent/internal/domain/identity"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUnallocatedTxLocksSameAccountUntilCallerCompletes(t *testing.T) {
	store, ctx := spaceContractStore(t)
	account := identity.New("unallocated")
	if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id,balance,recharge_balance) VALUES($1,17,17)`, account); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, account)
	})
	first, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(context.Background())
	amount, err := store.UnallocatedTx(ctx, first, account)
	if err != nil || amount != 17 {
		t.Fatal("personal account unallocated differs from balance", amount, err)
	}
	second, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(context.Background())
	if _, err = second.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
		t.Fatal(err)
	}
	_, err = lockedCreditWalletTx(ctx, second, account)
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "55P03" {
		t.Fatal("unallocated did not retain account lock for credit mutations", err)
	}
	if err = second.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	third, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Rollback(context.Background())
	if amount, err = store.UnallocatedTx(ctx, third, account); err != nil || amount != 17 {
		t.Fatal("caller commit did not release account lock", amount, err)
	}
}

func TestUnallocatedTxExpiryAndAccountCreationRollBackWithCaller(t *testing.T) {
	store, ctx := spaceContractStore(t)
	account, newAccount := identity.New("unallocated-expiry"), identity.New("unallocated-new")
	if _, err := store.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.credit_accounts(account_id,balance,recharge_balance,gift_balance,daily_balance,daily_expires_at) VALUES($1,17,7,10,10,$2)`, account, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.credit_ledger WHERE account_id=ANY($1)`, []string{account, newAccount})
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM drama_catalog_v1.credit_accounts WHERE account_id=ANY($1)`, []string{account, newAccount})
	})
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if amount, err := store.UnallocatedTx(ctx, tx, account); err != nil || amount != 7 {
		t.Fatal("expired daily credits inflated available amount", amount, err)
	}
	if amount, err := store.UnallocatedTx(ctx, tx, newAccount); err != nil || amount != 0 {
		t.Fatal("new personal account is not empty", amount, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var balance int64
	var exists bool
	if err = store.pool.QueryRow(ctx, `SELECT balance FROM drama_catalog_v1.credit_accounts WHERE account_id=$1`, account).Scan(&balance); err != nil || balance != 17 {
		t.Fatal("expiry committed outside caller transaction", balance, err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_catalog_v1.credit_accounts WHERE account_id=$1)`, newAccount).Scan(&exists); err != nil || exists {
		t.Fatal("account creation committed outside caller transaction", exists, err)
	}
	tx, err = store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = store.UnallocatedTx(ctx, tx, account); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var amount int64
	if err = store.pool.QueryRow(ctx, `SELECT amount FROM drama_catalog_v1.credit_ledger WHERE account_id=$1 AND kind='expire'`, account).Scan(&amount); err != nil || amount != 10 {
		t.Fatal("expiry receipt did not commit with caller", amount, err)
	}
}
