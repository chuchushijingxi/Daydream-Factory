package postgres_test

import (
	"context"
	"drama-agent/internal/adapter/postgres"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/identity"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestCreditOperationsBatchPreservesAllLedgerFields(t *testing.T) {
	ctx := context.Background()
	store, conn := identityDB(t)
	prefix := identity.New("batch_ledger")
	owner := createUser(t, store, prefix+"@example.test")
	if _, err := store.Add(ctx, c.Change{AccountID: owner.ID, Amount: 100, Category: c.Recharge, OperationID: prefix + "grant", Source: "fixture", Subject: owner.ID, Reason: "batch"}); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	expected := map[string]c.Entry{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("%s:%d", prefix, i)
		entry, err := store.Deduct(ctx, c.Change{AccountID: owner.ID, Amount: 1, OperationID: id, Source: "project", Subject: owner.ID, Reason: "generation"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		expected[id] = entry
	}
	ids = append(ids, ids[0], prefix+"missing")
	got, err := store.CreditOperations(ctx, ids)
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal("batch changed ledger fields", err)
	}
	if empty, err := (&postgres.Store{}).CreditOperations(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatal("empty read touched PG", err)
	}
	var before, after int64
	var stats bool
	if err = conn.QueryRow(ctx, `SELECT to_regclass('public.pg_stat_statements') IS NOT NULL`).Scan(&stats); err != nil {
		t.Fatal(err)
	}
	query := `SELECT coalesce(sum(calls),0)::bigint FROM public.pg_stat_statements WHERE query LIKE 'SELECT id,operation_id,account_id,%' AND query LIKE '%operation_id=ANY%';`
	if stats {
		if err = conn.QueryRow(ctx, query).Scan(&before); err != nil {
			t.Fatal(err)
		}
	}
	got, err = store.CreditOperations(ctx, ids)
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal(err)
	}
	if stats {
		if err = conn.QueryRow(ctx, query).Scan(&after); err != nil || after-before != 1 {
			t.Fatal("SQL round trips", before, after, err)
		}
	} else {
		t.Log("pg_stat_statements absent: SQL count not verified in this environment")
	}
	// 并发读也必须逐字段保留账本事实，不能只核对返回数量。
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := store.CreditOperations(ctx, ids)
			if err != nil || !reflect.DeepEqual(rows, expected) {
				t.Error("concurrent batch changed ledger fields", err)
			}
		}()
	}
	wg.Wait()
}
