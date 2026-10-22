package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	space   *string
	scope   *ports.SpaceScope
	pool    *pgxpool.Pool
	runtime ports.RuntimeState
}

func Open(ctx context.Context, url string, settings ...appconfig.Database) (*Store, error) {
	conf, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL configuration")
	}
	if len(settings) > 0 {
		if settings[0].MaxConnections <= 0 {
			return nil, fmt.Errorf("invalid PostgreSQL pool settings")
		}
		conf.MaxConns = settings[0].MaxConnections
	}
	p, err := pgxpool.NewWithConfig(ctx, conf)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("PostgreSQL unavailable")
	}
	return &Store{pool: p}, nil
}

func (s *Store) Close() { s.pool.Close() }

// UseRuntime installs the one Redis runtime; durable data ownership is unchanged.
func (s *Store) UseRuntime(state ports.RuntimeState) { s.runtime = state }
func (s *Store) runtimeState() (ports.RuntimeState, error) {
	if s.runtime == nil {
		return nil, fault.New("RUNTIME_UNAVAILABLE", "Redis运行状态尚未组装")
	}
	return s.runtime, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	// DDL is a database transaction, including its one-time migration lock.
	// Concurrent API starts must not replace triggers/constraints simultaneously.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('drama-agent:schema',0))`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schemaSQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// 空间、负责人和原创建人是独立列的权威事实，payload 不重复存储。
func marshalProjectPayload(p d.Project) ([]byte, error) {
	return json.Marshal(struct {
		d.Project
		OwnerID   string `json:"ownerId,omitempty"`
		SpaceID   string `json:"spaceId,omitempty"`
		CreatorID string `json:"creatorId,omitempty"`
	}{Project: p})
}

func (s *Store) Create(ctx context.Context, p d.Project) error {
	if s.space != nil && (*s.space == "" || p.SpaceID != *s.space) {
		return fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	if p.ID == "" || p.SpaceID == "" || p.OwnerID == "" || p.CreatorID == "" || p.Revision != 1 {
		return fault.ErrInvalid
	}
	raw, err := marshalProjectPayload(p)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.projects(id,revision,payload,space_id,owner_id,creator_id) VALUES($1,$2,$3,$4,$5,$6)`, p.ID, p.Revision, raw, p.SpaceID, p.OwnerID, p.CreatorID)
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return fault.ErrConflict
	}
	if err == nil {
		s.invalidateAfterCommit(ctx, "projects:"+p.SpaceID, "projects:system", "progress:"+p.SpaceID+":"+p.ID)
	}
	return err
}

func (s *Store) Snapshot(ctx context.Context, id string) (d.Project, error) {
	if s.space != nil && *s.space == "" {
		return d.Project{}, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	var raw []byte
	var space, owner, creator string
	err := s.pool.QueryRow(ctx, `SELECT payload,space_id,owner_id,creator_id FROM drama_catalog_v1.projects WHERE id=$1 AND ($2::text IS NULL OR (space_id=$2 AND $2<>''))`, id, s.space).Scan(&raw, &space, &owner, &creator)
	if errors.Is(err, pgx.ErrNoRows) {
		return d.Project{}, fault.ErrNotFound
	}
	if err != nil {
		return d.Project{}, err
	}
	var p d.Project
	err = json.Unmarshal(raw, &p)
	p.SpaceID, p.OwnerID, p.CreatorID = space, owner, creator
	return p, err
}

func (s *Store) Commit(ctx context.Context, p d.Project, expected uint64) error {
	if s.space != nil && (*s.space == "" || p.SpaceID != *s.space) {
		return fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	if p.ID == "" || p.SpaceID == "" || p.OwnerID == "" || p.CreatorID == "" || p.Revision != expected+1 {
		return fault.ErrInvalid
	}
	raw, err := marshalProjectPayload(p)
	if err != nil {
		return err
	}
	// revision 与完整 payload 同一 SQL 更新；并发冲突不部分写入、不自动覆盖。
	result, err := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.projects SET payload=$3,revision=$2,updated_at=now() WHERE id=$1 AND revision=$4 AND space_id=$5 AND creator_id=$6 AND owner_id=$7`, p.ID, p.Revision, raw, expected, p.SpaceID, p.CreatorID, p.OwnerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fault.ErrConflict
	}
	s.invalidateAfterCommit(ctx, "projects:"+p.SpaceID, "projects:system", "progress:"+p.SpaceID+":"+p.ID)
	return nil
}

func (s *Store) Acquire(ctx context.Context, id string) (ports.Lease, bool, error) {
	r, err := s.runtimeState()
	if err != nil {
		return ports.Lease{}, false, err
	}
	return r.Lock(ctx, "project:"+id, false)
}

func (s *Store) ForSpace(space string) *Store {
	return &Store{pool: s.pool, space: &space, runtime: s.runtime}
}
func (s *Store) ForProjectSpace(space string) ports.ProjectStore           { return s.ForSpace(space) }
func (s *Store) ForUploadedFileSpace(space string) ports.UploadedFileStore { return s.ForSpace(space) }
func (s *Store) ForProjectScope(scope ports.SpaceScope) ports.ProjectStore {
	scoped := s.ForSpace(scope.SpaceID)
	scoped.scope = &scope
	return scoped
}
