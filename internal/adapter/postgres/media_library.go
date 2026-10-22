package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	"github.com/jackc/pgx/v5"
)

// 单次查询只提取一次相关项目的小元数据。项目含完整模型历史，若在每个
// task/output 的 JOIN 中取 JSON 字段，会反复解压大 payload；不为此另存项目事实。
const libraryProjects = `WITH library_projects AS MATERIALIZED (
 SELECT p.id,p.space_id,p.owner_id,coalesce(m.title,'') AS title,m."deletedAt" AS deleted_at,
 coalesce(m.style#>>'{prompts,zh}','') AS style_prompt
 FROM drama_catalog_v1.projects p
 CROSS JOIN LATERAL jsonb_to_record(p.payload) AS m(title text,"deletedAt" text,style jsonb)
 WHERE p.space_id=$1 AND EXISTS (
 SELECT 1 FROM drama_catalog_v1.tasks t WHERE t.space_id=$1 AND t.scope_id='project:'||p.id
 AND t.status='succeeded' AND t.payload->>'operation' IN ('image.generate','video.generate')
 AND ($2='' OR t.payload->>'operation'=$2||'.generate')
 )) `

// 详情只读取指定任务关联项目，同样避免把完整项目历史带进输出连接。
const libraryDetailProject = `WITH library_projects AS MATERIALIZED (
 SELECT p.id,p.space_id,p.owner_id,coalesce(m.title,'') AS title,m."deletedAt" AS deleted_at,
 coalesce(m.style#>>'{prompts,zh}','') AS style_prompt
 FROM drama_catalog_v1.projects p
 CROSS JOIN LATERAL jsonb_to_record(p.payload) AS m(title text,"deletedAt" text,style jsonb)
 WHERE ($1='' OR p.space_id=$1) AND 'project:'||p.id=(
 SELECT t.scope_id FROM drama_catalog_v1.tasks t WHERE t.id=$2 AND ($1='' OR t.space_id=$1)
 )) `

// 项目风格保存后不可更换，运输输入始终在正文后追加同一完整风格块，构图
// 还可在其后追加。只移除最后一个完全匹配的拼接块，保留正文中的同文引用和
// 尾部构图，不用关键词猜测内容，也不重写原input_snapshot。
const libraryContentPrompt = `
 CROSS JOIN LATERAL (SELECT coalesce(t.input_snapshot#>>'{input,prompt}','') AS prompt,
 E'\n\n'||p.style_prompt AS style_block) frozen
 CROSS JOIN LATERAL (SELECT strpos(reverse(frozen.prompt),reverse(frozen.style_block)) AS style_from_end) assembled
 CROSS JOIN LATERAL (SELECT CASE WHEN p.style_prompt<>'' AND assembled.style_from_end>0
 THEN overlay(frozen.prompt PLACING '' FROM length(frozen.prompt)-assembled.style_from_end-length(frozen.style_block)+2 FOR length(frozen.style_block))
 ELSE frozen.prompt END AS prompt) content `

// scope 由统一空间解析器提供；任务与未删除项目须同空间，不以 scope_id 或文件 ID 授权。
const libraryFrom = ` FROM drama_catalog_v1.tasks t
 JOIN library_projects p ON t.scope_id='project:'||p.id AND p.space_id=t.space_id
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(t.payload#>'{result,files}','[]'::jsonb)) f ` + libraryContentPrompt + `
 WHERE ($1='' OR t.space_id=$1) AND p.deleted_at IS NULL
 AND t.status='succeeded' AND t.payload->>'operation' IN ('image.generate','video.generate')
 AND ((t.payload->>'operation'='image.generate' AND f->>'contentType' LIKE 'image/%')
 OR (t.payload->>'operation'='video.generate' AND f->>'contentType' LIKE 'video/%'))`
const libraryColumns = `coalesce(f->>'fileId',''),t.id,f->>'id',p.id,CASE WHEN source_access.effective>0 THEN p.title ELSE '' END,
 split_part(t.payload->>'operation','.',1),t.created_at,coalesce(t.payload->>'modelId',''),
 coalesce(t.payload#>>'{binding,requestModelId}',''),coalesce(t.input_snapshot#>>'{input,aspectRatio}',''),
 coalesce(t.input_snapshot#>>'{input,resolution}',''),(f->>'width')::int,(f->>'height')::int,
 coalesce((f->>'durationSeconds')::double precision,0),f->>'contentType',(f#>>'{blob,sizeBytes}')::bigint,
 left(content.prompt,120),t.space_id,t.owner_id,t.creator_id`

// 素材按自己的负责人授权；来源项目的当前标题另按项目权限投影，
// 不能因持有某个输出就看到已无权访问的项目改名，或通过标题搜索探测它。
func libraryProjectAccessSQL() string {
	return `CASE WHEN v.user_id IS NULL THEN 0 ELSE ` + spaceAccessSQL("v", "p.space_id", "p.owner_id", "'project'", projectSharePermissionSQL("p", "v")) + ` END`
}

func scanLibrary(row rowScanner, detail bool) (medialibrary.Item, error) {
	var v medialibrary.Item
	args := []any{&v.FileID, &v.TaskID, &v.OutputID, &v.ProjectID, &v.ProjectTitle, &v.Kind, &v.CreatedAt, &v.ModelID, &v.RequestModelID, &v.AspectRatio, &v.Resolution, &v.Width, &v.Height, &v.DurationSeconds, &v.ContentType, &v.SizeBytes, &v.PromptPreview, &v.SpaceID, &v.OwnerID, &v.CreatorID, &v.CreatorName, &v.SharedWithMe, &v.EffectiveAccess}
	if detail {
		args = append(args, &v.Prompt)
	}
	err := row.Scan(args...)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fault.ErrNotFound
	}
	return v, err
}
func (s *Store) ListGeneratedMedia(ctx context.Context, scope ports.SpaceScope, q medialibrary.Query) ([]medialibrary.Item, error) {
	if scope.SpaceID == "" || scope.UserID == "" {
		return nil, fault.New("AUTH_REQUIRED", "缺少可信归属")
	}
	filter, err := q.Filter.Normalize()
	if err != nil {
		return nil, err
	}
	if q.Limit < 1 || q.Limit > 101 {
		return nil, fault.ErrInvalid
	}
	var at *time.Time
	task, output := "", ""
	if q.After != nil {
		at = &q.After.At
		task = q.After.TaskID
		output = q.After.OutputID
	}
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q.Search) + "%"
	access := spaceAccessSQL("v", "t.space_id", "t.owner_id", "'media'", "0")
	visibleFrom := strings.Replace(libraryFrom, " CROSS JOIN LATERAL jsonb_array_elements", ` CROSS JOIN visible_scope v CROSS JOIN LATERAL(SELECT `+access+` AS effective)access CROSS JOIN LATERAL(SELECT `+libraryProjectAccessSQL()+` AS effective)source_access CROSS JOIN LATERAL jsonb_array_elements`, 1)
	rows, err := s.pool.Query(ctx, libraryProjects+`,`+visibleScopeSQL("$1", "$9")+` SELECT `+libraryColumns+`,coalesce((SELECT username FROM drama_catalog_v1.users WHERE id=t.creator_id),''),false,access.effective`+visibleFrom+`
 AND access.effective>0 AND ($10='all' OR ($10='mine' AND t.creator_id=v.user_id))
 AND ($2='' OR t.payload->>'operation'=$2||'.generate')
 AND ($3='' OR content.prompt ILIKE $4 OR (source_access.effective>0 AND p.title ILIKE $4))
 AND ($5::timestamptz IS NULL OR (t.created_at,t.id,f->>'id')<($5,$6,$7))
 ORDER BY t.created_at DESC,t.id DESC,f->>'id' DESC LIMIT $8`, scope.SpaceID, q.Kind, q.Search, pattern, at, task, output, q.Limit, scope.UserID, filter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []medialibrary.Item{}
	for rows.Next() {
		v, e := scanLibrary(rows, false)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) GeneratedMedia(ctx context.Context, scope ports.SpaceScope, task, output string) (medialibrary.Item, error) {
	// 详情不按当前选择限制资源；只用可信用户身份投影其来源项目信息。
	// 最终素材权限仍由应用层按实际 space/owner 调用 SpaceAccess。
	visibleFrom := strings.Replace(libraryFrom, " CROSS JOIN LATERAL jsonb_array_elements", ` LEFT JOIN visible_scope v ON v.space_id=t.space_id CROSS JOIN LATERAL(SELECT `+libraryProjectAccessSQL()+` AS effective)source_access CROSS JOIN LATERAL jsonb_array_elements`, 1)
	return scanLibrary(s.pool.QueryRow(ctx, libraryDetailProject+`,`+visibleScopeSQL("(SELECT space_id FROM library_projects LIMIT 1)", "$4")+` SELECT `+libraryColumns+`,coalesce((SELECT username FROM drama_catalog_v1.users WHERE id=t.creator_id),''),false,0,content.prompt`+visibleFrom+` AND t.id=$2 AND f->>'id'=$3`, scope.SpaceID, task, output, scope.UserID), true)
}
