package postgres

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureTaskSchedule only reads durable tasks when Redis has lost its ready marker.
// Live recovery is through committed runtime_events; ordinary due scans use Redis.
func (s *Store) EnsureTaskSchedule(ctx context.Context) error {
	r, err := s.runtimeState()
	if err != nil {
		return err
	}
	ready, err := r.ScheduleReady(ctx)
	if err != nil || ready {
		return err
	}
	lease, _, err := r.Lock(ctx, "schedule-rebuild", true)
	if err != nil {
		return err
	}
	defer lease.Release()
	ctx = lease.Context
	ready, err = r.ScheduleReady(ctx)
	if err != nil || ready {
		return err
	}
	last := ""
	for {
		rows, err := s.pool.Query(ctx, `SELECT id,revision,CASE WHEN next_run_at IS NULL THEN NULL ELSE greatest(next_run_at,lease_until) END FROM drama_catalog_v1.tasks WHERE id>$1 ORDER BY id LIMIT 200`, last)
		if err != nil {
			return err
		}
		type entry struct {
			id      string
			version uint64
			at      *time.Time
		}
		entries := []entry{}
		for rows.Next() {
			var item entry
			if err = rows.Scan(&item.id, &item.version, &item.at); err != nil {
				break
			}
			entries = append(entries, item)
		}
		rowErr := rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if rowErr != nil {
			return rowErr
		}
		for _, item := range entries {
			if err = r.ScheduleTask(ctx, item.id, item.version, item.at); err != nil {
				return err
			}
			last = item.id
		}
		if len(entries) < 200 {
			break
		}
	}
	if err = lease.Check(ctx); err != nil {
		return err
	}
	return r.MarkScheduleReady(ctx)
}

// FlushRuntimeEvents is bounded and repeatable. Row locks protect acknowledgement
// from a concurrent change to the same entity; failed Redis work remains pending.
func (s *Store) FlushRuntimeEvents(ctx context.Context) error {
	r, err := s.runtimeState()
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	// A suspended/disconnected projector cannot run its Go cancellation timer.
	// Bound server-side idle time while Redis work holds delivery-row locks.
	if _, err = tx.Exec(ctx, `SET LOCAL idle_in_transaction_session_timeout='5s'`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT key FROM drama_catalog_v1.runtime_events ORDER BY created_at,key LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			break
		}
		keys = append(keys, key)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	for _, key := range keys {
		kind, id, _ := strings.Cut(key, ":")
		switch kind {
		case "task":
			var version uint64
			var at *time.Time
			var owner, scope string
			err = tx.QueryRow(ctx, `SELECT revision,CASE WHEN next_run_at IS NULL THEN NULL ELSE greatest(next_run_at,lease_until) END,coalesce(owner_id,''),scope_id FROM drama_catalog_v1.tasks WHERE id=$1`, id).Scan(&version, &at, &owner, &scope)
			if errors.Is(err, pgx.ErrNoRows) {
				err = nil
				continue
			}
			if err != nil {
				return err
			}
			if err = r.ScheduleTask(ctx, id, version, at); err != nil {
				return err
			}
			projectID := strings.TrimPrefix(scope, "project:")
			if err = r.Invalidate(ctx, "progress:"+owner+":"+projectID); err != nil {
				return err
			}
			if err = r.Publish(ctx, scope); err != nil {
				return err
			}
		case "project":
			var owner string
			err = tx.QueryRow(ctx, `SELECT coalesce(owner_id,'') FROM drama_catalog_v1.projects WHERE id=$1`, id).Scan(&owner)
			if errors.Is(err, pgx.ErrNoRows) {
				err = nil
				continue
			}
			if err != nil {
				return err
			}
			if err = r.Invalidate(ctx, "projects:"+owner, "projects:system", "progress:"+owner+":"+id); err != nil {
				return err
			}
			if err = r.Publish(ctx, key); err != nil {
				return err
			}
		case "notifications":
			cache := "notifications:" + id
			if id == "*" {
				cache = "notifications"
			}
			if err = r.Invalidate(ctx, cache); err != nil {
				return err
			}
			if err = r.Publish(ctx, key); err != nil {
				return err
			}
		case "credits":
			if err = r.Publish(ctx, key); err != nil {
				return err
			}
		case "models", "platform":
			if err = r.Invalidate(ctx, kind); err != nil {
				return err
			}
		}
	}
	if len(keys) > 0 {
		if _, err = tx.Exec(ctx, `DELETE FROM drama_catalog_v1.runtime_events WHERE key=ANY($1)`, keys); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// The durable outbox remains the repair path when this immediate invalidation fails.
func (s *Store) invalidateAfterCommit(ctx context.Context, keys ...string) {
	if s.runtime == nil {
		return
	}
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.runtime.Invalidate(c, keys...); err != nil {
		log.Printf("runtime invalidation queued for repair: %v", err)
	}
}

func (s *Store) RunRuntimeEvents(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastLog time.Time
	for {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.FlushRuntimeEvents(c)
		cancel()
		if err != nil && ctx.Err() == nil && time.Since(lastLog) > 10*time.Second {
			log.Printf("runtime projection pending: %v", err)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
