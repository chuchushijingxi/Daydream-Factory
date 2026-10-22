package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/task"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TaskStore struct {
	store        *Store
	space        *string
	commandActor *string
}

func NewTaskStore(s *Store) *TaskStore { return &TaskStore{store: s} }

var _ ports.TaskStore = (*TaskStore)(nil)

const taskColumns = `id,scope_id,command_id,status,revision,next_run_at,lease_token,lease_until,payload,created_at,updated_at,space_id,owner_id,creator_id`

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner) (d.Task, error) {
	var t d.Task
	var raw []byte
	err := row.Scan(&t.ID, &t.ScopeID, &t.CommandID, &t.Status, &t.Revision, &t.NextRunAt, &t.LeaseToken, &t.LeaseUntil, &raw, &t.CreatedAt, &t.UpdatedAt, &t.SpaceID, &t.OwnerID, &t.CreatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, fault.ErrNotFound
	}
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(raw, &t.State)
	return t, err
}
func (s *TaskStore) CreateTask(ctx context.Context, t d.Task) (d.Task, error) {
	if s.space != nil && (*s.space == "" || t.SpaceID != *s.space) {
		return t, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	if t.SpaceID == "" || t.OwnerID == "" || t.CreatorID == "" {
		return t, fault.ErrInvalid
	}
	input, err := json.Marshal(t.Snapshot)
	if err != nil {
		return t, err
	}
	body, err := json.Marshal(t.State)
	if err != nil {
		return t, err
	}
	got, err := scanTask(s.store.pool.QueryRow(ctx, `INSERT INTO drama_catalog_v1.tasks(id,scope_id,command_id,status,revision,next_run_at,lease_token,input_snapshot,payload,space_id,owner_id,creator_id) VALUES($1,$2,$3,$4,1,now(),'',$5,$6,$7,$8,$9) ON CONFLICT (space_id,owner_id,scope_id,command_id) DO NOTHING RETURNING `+taskColumns, t.ID, t.ScopeID, t.CommandID, t.Status, input, body, t.SpaceID, t.OwnerID, t.CreatorID))
	if errors.Is(err, fault.ErrNotFound) {
		got, err = scanTask(s.store.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM drama_catalog_v1.tasks WHERE space_id=$1 AND owner_id=$2 AND scope_id=$3 AND command_id=$4`, t.SpaceID, t.OwnerID, t.ScopeID, t.CommandID))
		if err == nil && got.State.CommandDigest != t.State.CommandDigest {
			return got, fault.New("COMMAND_ID_CONFLICT", "同一命令对应不同请求")
		}
		return got, err
	}
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" && e.ConstraintName == "tasks_space_active_uidx" {
		return d.Task{}, fault.New("TASK_ACTIVE", "该目标已有活动任务")
	}
	return got, err
}
func (s *TaskStore) Task(ctx context.Context, id string) (d.Task, error) {
	return scanTask(s.store.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM drama_catalog_v1.tasks WHERE id=$1 AND ($2::text IS NULL OR (space_id=$2 AND $2<>''))`, id, s.space))
}
func (s *TaskStore) TaskInput(ctx context.Context, id string) (d.Snapshot, error) {
	var raw []byte
	var out d.Snapshot
	err := s.store.pool.QueryRow(ctx, `SELECT input_snapshot FROM drama_catalog_v1.tasks WHERE id=$1 AND ($2::text IS NULL OR (space_id=$2 AND $2<>''))`, id, s.space).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fault.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

// TaskInputs 按 scope 与归属一次取回多条快照；缺失的任务不进入结果，
// 由调用方决定缺失语义（与单条 TaskInput 的 NOT_FOUND 一致）。
func (s *TaskStore) TaskInputs(ctx context.Context, scope string, ids []string) (map[string]d.Snapshot, error) {
	out := map[string]d.Snapshot{}
	if scope == "" || len(ids) == 0 {
		return out, nil
	}
	rows, err := s.store.pool.Query(ctx, `SELECT id,input_snapshot FROM drama_catalog_v1.tasks WHERE scope_id=$1 AND id=ANY($2) AND ($3::text IS NULL OR (space_id=$3 AND $3<>''))`, scope, ids, s.space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var snapshot d.Snapshot
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
		out[id] = snapshot
	}
	return out, rows.Err()
}
func (s *TaskStore) TaskByCommand(ctx context.Context, scope, command string) (d.Task, error) {
	if s.space == nil || *s.space == "" || s.commandActor == nil || *s.commandActor == "" {
		return d.Task{}, fault.New("AUTH_REQUIRED", "缺少可信空间与操作者")
	}
	return scanTask(s.store.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM drama_catalog_v1.tasks WHERE space_id=$1 AND owner_id=$2 AND scope_id=$3 AND command_id=$4`, *s.space, *s.commandActor, scope, command))
}
func (s *TaskStore) ListTasks(ctx context.Context, scope, operation string, c *ports.TaskCursor, limit int) ([]d.Task, error) {
	var at *time.Time
	id := ""
	if c != nil {
		at = &c.At
		id = c.ID
	}
	rows, err := s.store.pool.Query(ctx, `SELECT `+taskColumns+` FROM drama_catalog_v1.tasks WHERE scope_id=$1 AND ($2='' OR payload->>'operation'=$2) AND ($3::timestamptz IS NULL OR (created_at,id)<($3,$4)) AND ($6::text IS NULL OR (space_id=$6 AND $6<>'')) ORDER BY created_at DESC,id DESC LIMIT $5`, scope, operation, at, id, limit, s.space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []d.Task{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *TaskStore) ClaimTask(ctx context.Context, id, token string, lease time.Duration) (d.Task, bool, error) {
	if token == "" || lease <= 0 {
		return d.Task{}, false, fault.ErrInvalid
	}
	t, err := scanTask(s.store.pool.QueryRow(ctx, `UPDATE drama_catalog_v1.tasks SET lease_token=$2,lease_until=now()+($3*interval '1 millisecond'),revision=revision+1,updated_at=now() WHERE id=$1 AND next_run_at<=now() AND (lease_until IS NULL OR lease_until<=now()) AND ($4::text IS NULL OR(space_id=$4 AND $4<>'')) RETURNING `+taskColumns, id, token, lease.Milliseconds(), s.space))
	if errors.Is(err, fault.ErrNotFound) {
		return d.Task{}, false, nil
	}
	return t, err == nil, err
}
func (s *TaskStore) CommitTask(ctx context.Context, t d.Task, expected uint64, token string, release bool) (d.Task, error) {
	if s.space != nil && (*s.space == "" || t.SpaceID != *s.space) {
		return d.Task{}, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	raw, err := json.Marshal(t.State)
	if err != nil {
		return t, err
	}
	got, err := scanTask(s.store.pool.QueryRow(ctx, `UPDATE drama_catalog_v1.tasks SET status=$4,payload=$5,next_run_at=$6,revision=revision+1,updated_at=now(),lease_token=CASE WHEN $7 THEN '' ELSE lease_token END,lease_until=CASE WHEN $7 THEN NULL ELSE lease_until END WHERE id=$1 AND revision=$2 AND lease_token=$3 AND lease_until>now() AND space_id=$8 AND owner_id=$9 AND creator_id=$10 RETURNING `+taskColumns, t.ID, expected, token, t.Status, raw, t.NextRunAt, release, t.SpaceID, t.OwnerID, t.CreatorID))
	if errors.Is(err, fault.ErrNotFound) {
		return got, fault.ErrConflict
	}
	return got, err
}
func (s *TaskStore) RenewTask(ctx context.Context, id, token string, lease time.Duration) error {
	r, err := s.store.pool.Exec(ctx, `UPDATE drama_catalog_v1.tasks SET lease_until=now()+($3*interval '1 millisecond') WHERE id=$1 AND lease_token=$2 AND lease_until>now() AND ($4::text IS NULL OR (space_id=$4 AND $4<>''))`, id, token, lease.Milliseconds(), s.space)
	if err == nil && r.RowsAffected() != 1 {
		return fault.ErrConflict
	}
	return err
}
func (s *TaskStore) CancelTask(ctx context.Context, scope, id string, revision uint64) (d.Task, error) {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return d.Task{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	t, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM drama_catalog_v1.tasks WHERE id=$1 AND scope_id=$2 AND ($3::text IS NULL OR (space_id=$3 AND $3<>'')) FOR UPDATE`, id, scope, s.space))
	if err != nil {
		return t, err
	}
	if t.Status == d.Cancelled {
		return t, tx.Commit(ctx)
	}
	if t.Status.Terminal() {
		return t, fault.New("TASK_TERMINAL", "任务已结束")
	}
	if t.Revision != revision {
		return t, fault.ErrConflict
	}
	if err = t.Transition(d.Cancelled); err != nil {
		return t, err
	}
	t.NextRunAt = nil
	// 有远端任务且费用未结时仍可继续观察；终态不因此复活生成。
	if !t.State.Binding.Free && t.Billing().Status != "known" && t.Remote() != nil {
		at := time.Now().UTC().Add(time.Duration(t.State.Policy.PollMillis) * time.Millisecond)
		t.NextRunAt = &at
	}
	raw, err := json.Marshal(t.State)
	if err != nil {
		return t, err
	}
	t, err = scanTask(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.tasks SET status=$2,payload=$3,next_run_at=$4,lease_token='',lease_until=NULL,revision=revision+1,updated_at=now() WHERE id=$1 AND space_id=$5 AND revision=$6 RETURNING `+taskColumns, id, t.Status, raw, t.NextRunAt, t.SpaceID, t.Revision))
	if err == nil {
		err = tx.Commit(ctx)
	}
	return t, err
}
func (s *TaskStore) AppendTaskEvidence(ctx context.Context, id string, e ports.TaskEvidence) (d.Task, error) {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return d.Task{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	t, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM drama_catalog_v1.tasks WHERE id=$1 AND ($2::text IS NULL OR(space_id=$2 AND $2<>'')) FOR UPDATE`, id, s.space))
	if err != nil {
		return t, err
	}
	changed := false
	if e.Receipt != nil {
		found := false
		for i := range t.State.Submissions {
			sub := &t.State.Submissions[i]
			if sub.ID != e.SubmissionID {
				continue
			}
			found = true
			if sub.Receipt != nil {
				a, _ := json.Marshal(sub.Receipt)
				b, _ := json.Marshal(e.Receipt)
				if string(a) != string(b) {
					return t, fault.ErrConflict
				}
				return t, tx.Commit(ctx)
			}
			sub.Receipt = e.Receipt
			sub.Disposition = e.Receipt.Disposition()
			changed = true
		}
		if !found {
			return t, fault.ErrNotFound
		}
	}
	if e.Observation != nil {
		found := t.State.LastObservation != nil && t.State.LastObservation.ID == e.Observation.ID
		for _, o := range t.State.RetainedObservations {
			if o.ID == e.Observation.ID {
				found = true
			}
		}
		if !found {
			if e.Observation.Receipt.Billing.Status == "known" && (t.State.LastObservation == nil || !t.State.LastObservation.At.After(e.Observation.At)) {
				if sub := t.LatestSubmission(); sub != nil && sub.Receipt != nil {
					sub.Receipt.Billing = e.Observation.Receipt.Billing
				}
			}
			t.Observe(*e.Observation)
			changed = true
		}
	}
	if !changed {
		return t, tx.Commit(ctx)
	}
	// 迟到证据只补事实，不采用结果、不覆盖当前租约与状态。
	if !t.Status.Terminal() && t.NextRunAt == nil {
		at := time.Now().UTC()
		t.NextRunAt = &at
	}
	if t.Status.Terminal() && t.Remote() != nil && t.Billing().Status != "known" && t.NextRunAt == nil {
		at := time.Now().UTC()
		t.NextRunAt = &at
	}
	raw, err := json.Marshal(t.State)
	if err != nil {
		return t, err
	}
	t, err = scanTask(tx.QueryRow(ctx, `UPDATE drama_catalog_v1.tasks SET payload=$2,next_run_at=$3,revision=revision+1,updated_at=now() WHERE id=$1 AND space_id=$4 AND revision=$5 RETURNING `+taskColumns, id, raw, t.NextRunAt, t.SpaceID, t.Revision))
	if err == nil {
		err = tx.Commit(ctx)
	}
	return t, err
}
func (s *TaskStore) AcquireTaskScan(ctx context.Context) (ports.TaskScan, bool, error) {
	runtime, err := s.store.runtimeState()
	if err != nil {
		return ports.TaskScan{}, false, err
	}
	lease, ok, err := runtime.Lock(ctx, "task-scan", false)
	if err != nil || !ok {
		return ports.TaskScan{}, ok, err
	}
	return ports.TaskScan{Check: lease.Check, Release: lease.Release}, true, nil
}
func (s *TaskStore) DueTasks(ctx context.Context, cursor *ports.TaskCursor, limit int) ([]ports.TaskDue, error) {
	if err := s.store.EnsureTaskSchedule(ctx); err != nil {
		return nil, err
	}
	return s.store.runtime.ScheduledTasks(ctx, cursor, limit)
}

func (s *TaskStore) ForTaskSpace(space string) ports.TaskStore {
	return &TaskStore{store: s.store, space: &space}
}
func (s *TaskStore) ForTaskScope(scope ports.SpaceScope) ports.TaskStore {
	return &TaskStore{store: s.store, space: &scope.SpaceID, commandActor: &scope.UserID}
}
func (s *TaskStore) TaskSpace(ctx context.Context, id string) (string, string, string, error) {
	var space, owner, creator string
	err := s.store.pool.QueryRow(ctx, `SELECT space_id,owner_id,creator_id FROM drama_catalog_v1.tasks WHERE id=$1`, id).Scan(&space, &owner, &creator)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return space, owner, creator, err
}
