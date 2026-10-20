package postgres

import (
	"context"
	app "drama-agent/internal/application/mailconfig"
	"drama-agent/internal/domain/fault"
)

func (s *Store) MailConfigSnapshot(ctx context.Context) (app.Snapshot, error) {
	var value app.Snapshot
	err := s.pool.QueryRow(ctx, `SELECT revision,enabled,address,username,from_address,tls,password_cipher FROM drama_catalog_v1.mail_configuration WHERE id`).Scan(&value.Revision, &value.Enabled, &value.Address, &value.Username, &value.From, &value.TLS, &value.PasswordCipher)
	if err != nil {
		return app.Snapshot{}, fault.Wrap("MAIL_CONFIGURATION_UNAVAILABLE", "邮件配置存储不可用，请检查服务迁移", err)
	}
	return value, nil
}
func (s *Store) CommitMailConfig(ctx context.Context, revision uint64, next app.Snapshot) error {
	if revision >= app.MaxRevision || next.Revision != revision+1 {
		return fault.New("MAIL_CONFIGURATION_INVALID", "邮件配置版本无效")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE drama_catalog_v1.mail_configuration SET revision=$2,enabled=$3,address=$4,username=$5,from_address=$6,tls=$7,password_cipher=$8 WHERE id AND revision=$1`, revision, next.Revision, next.Enabled, next.Address, next.Username, next.From, next.TLS, next.PasswordCipher)
	if err != nil {
		return fault.New("MAIL_CONFIG_SAVE_UNKNOWN", "邮件配置保存结果未确认，请重新读取后核对")
	}
	if tag.RowsAffected() != 1 {
		return fault.New("MAIL_CONFIGURATION_CONFLICT", "邮件配置已被修改，请重新读取后再保存")
	}
	return nil
}
