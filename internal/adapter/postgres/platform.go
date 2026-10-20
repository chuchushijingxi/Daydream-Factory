package postgres

import (
	"context"
	"errors"

	app "drama-agent/internal/application/platform"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/platform"

	"github.com/jackc/pgx/v5"
)

func (s *Store) PlatformSettings(ctx context.Context) (app.Snapshot, error) {
	return readProjection(ctx, s, "platform:public", s.loadPlatformSettings)
}
func (s *Store) loadPlatformSettings(ctx context.Context) (app.Snapshot, error) {
	var v app.Snapshot
	err := s.pool.QueryRow(ctx, `SELECT revision,name,sidebar_description,coalesce(logo_id,'') FROM drama_catalog_v1.platform_settings WHERE id`).Scan(&v.Revision, &v.Name, &v.SidebarDescription, &v.LogoID)
	if err != nil {
		return app.Snapshot{}, fault.Wrap("PLATFORM_SETTINGS_UNAVAILABLE", "平台设置存储不可用，请检查服务迁移", err)
	}
	return v, nil
}
func (s *Store) CommitPlatformSettings(ctx context.Context, revision uint64, next d.Settings) (app.Snapshot, error) {
	var v app.Snapshot
	err := s.pool.QueryRow(ctx, `UPDATE drama_catalog_v1.platform_settings SET revision=revision+1,name=$2,sidebar_description=$3,logo_id=nullif($4,'') WHERE id AND revision=$1 RETURNING revision,name,sidebar_description,coalesce(logo_id,'')`, revision, next.Name, next.SidebarDescription, next.LogoID).Scan(&v.Revision, &v.Name, &v.SidebarDescription, &v.LogoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Snapshot{}, fault.New("PLATFORM_SETTINGS_CONFLICT", "平台设置已被修改，请重新读取后再保存")
	}
	if err != nil {
		// UPDATE 可能已提交，不能把网络或响应失败当作未保存并自动重发。
		return app.Snapshot{}, fault.Wrap("PLATFORM_SETTINGS_SAVE_UNKNOWN", "平台设置保存结果未确认，请重新读取后核对", err)
	}
	s.invalidateAfterCommit(ctx, "platform")
	return v, nil
}
func (s *Store) SavePlatformLogo(ctx context.Context, id string, png []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO drama_catalog_v1.platform_logos(id,png) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, png)
	if err != nil {
		return fault.Wrap("PLATFORM_SETTINGS_UNAVAILABLE", "平台Logo上传未确认；可重新上传相同图片取得同一内容ID", err)
	}
	return nil
}
func (s *Store) PlatformLogo(ctx context.Context, id string) ([]byte, error) {
	var png []byte
	err := s.pool.QueryRow(ctx, `SELECT png FROM drama_catalog_v1.platform_logos WHERE id=$1`, id).Scan(&png)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault.ErrNotFound
	}
	if err != nil {
		return nil, fault.Wrap("PLATFORM_SETTINGS_UNAVAILABLE", "平台Logo暂不可用", err)
	}
	return png, nil
}
