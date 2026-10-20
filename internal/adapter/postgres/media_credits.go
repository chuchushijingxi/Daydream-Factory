package postgres

import (
	"context"
	"drama-agent/internal/application/ports"
)

// No project payload/body is read, and no additional balance or task table is
// maintained. The original immutable task basis and ledger are authoritative.
func (s *Store) UnsettledMediaCredits(ctx context.Context) ([]ports.MediaCreditRecovery, error) {
	rows, e := s.pool.Query(ctx, `SELECT t.owner_id,substring(t.scope_id from 9),t.input_snapshot#>>'{callerContext,credit,id}',(t.input_snapshot#>>'{callerContext,credit,amount}')::bigint
 FROM drama_catalog_v1.tasks t
 JOIN drama_catalog_v1.credit_ledger debit ON debit.operation_id='generation:'||(t.input_snapshot#>>'{callerContext,credit,id}')||':deduct'
 WHERE t.status IN ('failed','cancelled') AND t.owner_id IS NOT NULL AND t.scope_id LIKE 'project:%'
 AND t.payload->>'requirePermit'='true'
 AND NOT EXISTS(SELECT 1 FROM drama_catalog_v1.credit_ledger refund WHERE refund.operation_id='generation:'||(t.input_snapshot#>>'{callerContext,credit,id}')||':refund')
 ORDER BY t.created_at,t.id LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ports.MediaCreditRecovery{}
	for rows.Next() {
		var item ports.MediaCreditRecovery
		if e = rows.Scan(&item.OwnerID, &item.ProjectID, &item.ChargeID, &item.Amount); e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
