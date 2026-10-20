package postgres

import (
	"context"
	u "drama-agent/internal/application/modelusage"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// Explicit JSON keys below are the complete allowlist. Raw request/response,
// errors, user titles, tool arguments, callerContext and private URLs never enter it.
const usageSources = `WITH language_runs AS MATERIALIZED (
 SELECT coalesce(p.owner_id,'') user_id,
 coalesce(r#>>'{credit,id}',r->>'id') task_id,
 coalesce(r#>>'{credit,acceptedAt}',r->>'createdAt')::timestamptz task_at,
 r->>'contract' operation,r->>'status' status,r->>'model' model_id,
 r#>>'{credit,connectionId}' connection_id,r->'credit' basis,
 r->>'continueAssetPreparation'='true' continues,(r->>'endedAt')::timestamptz ended_at,
 lc.*
 FROM drama_catalog_v1.projects p
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(nullif(p.payload->'runs','null'::jsonb),'[]'::jsonb)) r
 CROSS JOIN LATERAL (
  SELECT count(*) call_count,coalesce(sum(known_cost),0)::bigint known_cost,
   coalesce(bool_or(unknown_cost),false) unknown_cost,
   sum(input_tokens)::bigint input_tokens,sum(output_tokens)::bigint output_tokens,sum(cache_tokens)::bigint cache_tokens,
   coalesce(jsonb_agg(jsonb_build_object('id',id,'createdAt',created_at,'status',status,'connectionId',connection_id,'requestId',request_id,'durationMs',duration_ms,'inputTokens',input_tokens,'outputTokens',output_tokens,'cacheHitTokens',cache_tokens,'knownCostMicroCny',known_cost,'unknownCost',unknown_cost,'errorCode',error_code)),'[]'::jsonb) safe_calls
  FROM (
   SELECT coalesce(c->>'id','') id,(c->>'createdAt')::timestamptz created_at,c->>'status' status,
    coalesce(c->>'connectionId',r#>>'{credit,connectionId}') connection_id,c->>'requestId' request_id,
    CASE WHEN c->>'completedAt' IS NOT NULL THEN (extract(epoch FROM (c->>'completedAt')::timestamptz-(c->>'createdAt')::timestamptz)*1000)::bigint END duration_ms,
    CASE WHEN c#>>'{usage,known}'='true' THEN (c#>>'{usage,inputTokens}')::bigint END input_tokens,
    CASE WHEN c#>>'{usage,known}'='true' THEN (c#>>'{usage,outputTokens}')::bigint END output_tokens,
    CASE WHEN c#>>'{usage,known}'='true' THEN (c#>>'{usage,cacheHitTokens}')::bigint END cache_tokens,
    CASE WHEN c#>>'{usage,known}'='true' THEN coalesce((c->>'costMicroCny')::bigint,0) ELSE 0 END known_cost,
    coalesce(c#>>'{usage,known}','false')<>'true' unknown_cost,
    CASE WHEN c->>'errorCode' IS NOT NULL THEN 'MODEL_CALL_FAILED' ELSE NULL END error_code
   FROM jsonb_array_elements(coalesce(nullif(r->'calls','null'::jsonb),'[]'::jsonb)) c
   WHERE c ? 'request' AND coalesce(c->>'notSent','false')<>'true'
  ) calls
 ) lc
), language_tasks AS (
 SELECT task_id id,task_id,min(user_id) user_id,
 CASE WHEN bool_or(continues) THEN 'script-parse' ELSE min(operation) END operation,
 CASE WHEN bool_or(status='running') THEN 'running' WHEN bool_or(status='failed') THEN 'failed' WHEN bool_or(status='cancelled') THEN 'cancelled' ELSE 'completed' END status,
 min(model_id) model_id,min(connection_id) connection_id,min(task_at) created_at,
 (jsonb_agg(basis) FILTER(WHERE basis IS NOT NULL))->0 basis,
 CASE WHEN bool_and(ended_at IS NOT NULL) THEN (extract(epoch FROM max(ended_at)-min(task_at))*1000)::bigint END duration_ms,
 NULL::text request_id,
 CASE WHEN NOT bool_or(unknown_cost) THEN sum(input_tokens)::bigint END input_tokens,
 CASE WHEN NOT bool_or(unknown_cost) THEN sum(output_tokens)::bigint END output_tokens,
 CASE WHEN NOT bool_or(unknown_cost) THEN sum(cache_tokens)::bigint END cache_tokens,
 NULL::jsonb media_usage,sum(call_count)::bigint call_count,sum(known_cost)::bigint known_cost,bool_or(unknown_cost) unknown_cost,
 CASE WHEN bool_or(status='failed') THEN 'GENERATION_FAILED' ELSE NULL END error_code
 FROM language_runs GROUP BY task_id
), media_tasks AS MATERIALIZED (
 SELECT t.id task_id,coalesce(t.owner_id,'') user_id,t.payload->>'operation' operation,t.status,
 t.payload->>'modelId' model_id,t.payload#>>'{binding,connectionId}' connection_id,t.created_at,
 t.input_snapshot#>'{callerContext,credit}' basis,
 CASE WHEN t.payload->>'endedAt' IS NOT NULL THEN (extract(epoch FROM (t.payload->>'endedAt')::timestamptz-t.created_at)*1000)::bigint END duration_ms,
 CASE WHEN jsonb_typeof(t.payload#>'{result,files}')='array' THEN (
  SELECT jsonb_build_object('imageCount',count(*) FILTER(WHERE f->>'contentType' LIKE 'image/%'),'videoSeconds',sum((f->>'durationSeconds')::numeric) FILTER(WHERE f->>'contentType' LIKE 'video/%')) FROM jsonb_array_elements(t.payload#>'{result,files}') f
 ) END media_usage,mc.*
 FROM drama_catalog_v1.tasks t
 CROSS JOIN LATERAL (
  SELECT count(*) call_count,coalesce(sum(known_cost),0)::bigint known_cost,coalesce(bool_or(unknown_cost),false) unknown_cost,
   coalesce(jsonb_agg(jsonb_build_object('id',id,'createdAt',created_at,'status',status,'requestId',request_id,'knownCostMicroCny',known_cost,'unknownCost',unknown_cost,'errorCode',error_code)),'[]'::jsonb) safe_calls
  FROM (
   SELECT coalesce(sub->>'id','') id,(sub->>'startedAt')::timestamptz created_at,sub->>'disposition' status,
    sub#>>'{receipt,remote,id}' request_id,
    CASE WHEN sub#>>'{receipt,billing,status}'='known' THEN coalesce((sub#>>'{receipt,billing,knownMicroCny}')::bigint,0) ELSE 0 END known_cost,
    coalesce(sub#>>'{receipt,billing,status}','unknown')<>'known' unknown_cost,
    CASE WHEN sub#>>'{receipt,errorCode}' IS NOT NULL THEN 'MODEL_CALL_FAILED' ELSE NULL END error_code
   FROM jsonb_array_elements(coalesce(nullif(t.payload->'submissions','null'::jsonb),'[]'::jsonb)) sub
   WHERE sub->>'disposition' IN ('may_have_sent','accepted','immediate','rejected')
  ) calls
 ) mc
), task_records AS (
 SELECT * FROM language_tasks UNION ALL
 SELECT task_id,task_id,user_id,operation,status,model_id,connection_id,created_at,basis,duration_ms,NULL::text,
 NULL::bigint,NULL::bigint,NULL::bigint,media_usage,call_count,known_cost,unknown_cost,
 CASE WHEN status='failed' THEN 'GENERATION_FAILED' ELSE NULL END FROM media_tasks
), call_records AS NOT MATERIALIZED (
 SELECT c->>'id' id,lr.task_id,lr.user_id,lr.operation,c->>'status' status,lr.model_id,c->>'connectionId' connection_id,(c->>'createdAt')::timestamptz created_at,
 NULL::jsonb basis,(c->>'durationMs')::bigint duration_ms,c->>'requestId' request_id,
 (c->>'inputTokens')::bigint input_tokens,(c->>'outputTokens')::bigint output_tokens,(c->>'cacheHitTokens')::bigint cache_tokens,
 NULL::jsonb media_usage,1::bigint call_count,(c->>'knownCostMicroCny')::bigint known_cost,(c->>'unknownCost')::boolean unknown_cost,c->>'errorCode' error_code
 FROM language_runs lr CROSS JOIN LATERAL jsonb_array_elements(lr.safe_calls) c
 UNION ALL
 SELECT c->>'id',mt.task_id,mt.user_id,mt.operation,c->>'status',mt.model_id,mt.connection_id,(c->>'createdAt')::timestamptz,
 NULL::jsonb,NULL::bigint,c->>'requestId',NULL::bigint,NULL::bigint,NULL::bigint,NULL::jsonb,1::bigint,
 (c->>'knownCostMicroCny')::bigint,(c->>'unknownCost')::boolean,c->>'errorCode'
 FROM media_tasks mt CROSS JOIN LATERAL jsonb_array_elements(mt.safe_calls) c
), records AS (
 SELECT * FROM task_records WHERE $1='tasks' UNION ALL SELECT * FROM call_records WHERE $1='calls'
), filtered AS MATERIALIZED (
 SELECT rec.*,coalesce(debit.amount,0) prepaid,coalesce(refund.amount,0) refunded,debit.id deduction_id,refund.id refund_id,
 CASE WHEN rec.status IN ('completed','succeeded') THEN coalesce(debit.amount,0)-coalesce(refund.amount,0) ELSE 0 END charged,
 CASE WHEN $1='calls' THEN 'task-billing' WHEN rec.status IN ('running','queued','unknown') THEN 'pending' WHEN rec.status IN ('failed','cancelled') AND debit.id IS NOT NULL AND refund.id IS NULL THEN 'refund-pending' WHEN refund.id IS NOT NULL THEN 'refunded' ELSE 'settled' END settlement
 FROM records rec
 LEFT JOIN drama_catalog_v1.credit_ledger debit ON $1='tasks' AND debit.operation_id='generation:'||coalesce(rec.basis->>'id',rec.task_id)||':deduct'
 LEFT JOIN drama_catalog_v1.credit_ledger refund ON $1='tasks' AND refund.operation_id='generation:'||coalesce(rec.basis->>'id',rec.task_id)||':refund'
 WHERE ($2='' OR rec.user_id=$2) AND ($3='' OR rec.task_id=$3) AND ($4='' OR rec.request_id=$4 OR rec.id=$4)
 AND ($5='' OR rec.model_id=$5) AND ($6='' OR rec.connection_id=$6) AND ($7='' OR rec.operation=$7) AND ($8='' OR rec.status=$8)
 AND ($9::timestamptz IS NULL OR rec.created_at >= $9) AND ($10::timestamptz IS NULL OR rec.created_at < $10)
 AND ($13='' OR rec.user_id=$13 OR rec.task_id=$13 OR rec.id=$13 OR rec.request_id=$13
   OR ($1='tasks' AND rec.task_id IN (SELECT task_id FROM call_records WHERE id=$13 OR request_id=$13)))
)`

func (s *Store) ModelUsage(ctx context.Context, f u.Filter) (u.Result, error) {
	out := u.Result{Items: []u.Record{}, Page: f.Page, PageSize: f.PageSize}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	args := []any{f.View, f.UserID, f.TaskID, f.RequestID, f.ModelID, f.ConnectionID, f.Operation, f.Status, f.From, f.Until}
	order := "DESC"
	if f.Sort == "asc" {
		order = "ASC"
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize, f.Search)
	var raw []byte
	e = tx.QueryRow(ctx, usageQuery(order), args...).Scan(&out.Total, &out.Stats.CallCount, &out.Stats.Charged, &out.Stats.Refunded, &out.Stats.KnownCostMicroCNY, &out.Stats.UnknownCost, &out.ReadAt, &raw)
	if e != nil {
		return out, e
	}
	out.Stats.Count = out.Total
	if e = json.Unmarshal(raw, &out.Items); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// Both scans read the same narrow materialized filtered result. The aggregate
// always emits one row, including an empty result or a page beyond the end.
func usageQuery(order string) string {
	return usageSources + `, stats AS (
 SELECT count(*) total,coalesce(sum(call_count),0) calls,coalesce(sum(charged),0) charged,coalesce(sum(refunded),0) refunded,coalesce(sum(known_cost),0) known_cost,coalesce(bool_or(unknown_cost),false) unknown_cost FROM filtered
 ), selected AS (
 SELECT * FROM filtered ORDER BY created_at ` + order + ` NULLS LAST,id ` + order + ` LIMIT $11 OFFSET $12
 ), paged AS (
 SELECT jsonb_build_object('id',rec.id,'taskId',task_id,'userId',user_id,'username',usr.username,'email',usr.email,'operation',operation,'status',status,'modelId',model_id,'connectionId',connection_id,'requestId',request_id,'createdAt',rec.created_at,'durationMs',duration_ms,'inputTokens',input_tokens,'outputTokens',output_tokens,'cacheHitTokens',cache_tokens,'mediaUsage',media_usage,'callCount',call_count,'knownCostMicroCny',known_cost,'unknownCost',unknown_cost,'basis',basis,'prepaid',prepaid,'refunded',refunded,'charged',charged,'settlement',settlement,'deductionId',deduction_id,'refundId',refund_id,'errorCode',error_code) record,rec.created_at,rec.id
 FROM selected rec LEFT JOIN drama_catalog_v1.users usr ON usr.id=rec.user_id
 ) SELECT total,calls,charged,refunded,known_cost,unknown_cost,transaction_timestamp(),
 coalesce((SELECT jsonb_agg(record ORDER BY created_at ` + order + ` NULLS LAST,id ` + order + `) FROM paged),'[]'::jsonb) FROM stats`
}
