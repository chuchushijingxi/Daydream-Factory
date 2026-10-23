package project

import (
	"context"
	"errors"
	"time"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

// Service 组装项目用例与外部端口；业务不变量由 Project 聚合维护。
// 正式变更只通过 Snapshot/Commit，模型调用另经许可与记账边界。
type Service struct {
	Credits                ports.CreditLedger
	Prices                 ports.CreditPrices
	Discounts              ports.CreditDiscounts
	agentUserID            string
	billingBasis           string
	scope                  *ports.SpaceScope
	Resolver               ports.SpaceResolver
	Access                 ports.SpaceAccess
	MediaTasks             *taskapp.Service
	Store                  ports.ProjectStore
	Agent                  ports.Agent
	Importer               ports.Importer
	Models                 ports.ModelProvider
	Styles                 []d.StyleDefinition
	Authorize              Authorization
	OriginalScripts        ports.OriginalScripts
	Files                  ports.FileReader
	Evidence               ports.EvidenceIndex
	ReceiptTimeout         time.Duration
	TransportRetryInterval time.Duration
}

func (s *Service) commit(ctx context.Context, p d.Project, revision uint64) (d.Project, error) {
	p.Revision = revision + 1
	err := s.Store.Commit(ctx, p, revision)
	if err == nil {
		err = s.settleCredits(ctx, p)
	}
	return p, err
}

// update 只为费用与回执合并并发取消后的最新快照，冲突后重新读回再结算。
// 用户命令及目录采用显式 revision；陈旧业务写入不能在这里静默重试。
func (s *Service) update(ctx context.Context, id string, change func(*d.Project) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := s.Store.Snapshot(ctx, id)
		if err != nil {
			return err
		}
		rev := p.Revision
		if err = change(&p); err != nil {
			return err
		}
		_, err = s.commit(ctx, p, rev)
		if errors.Is(err, fault.ErrConflict) {
			continue
		}
		return err
	}
}

func (s *Service) receiptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.ReceiptTimeout > 0 {
		return context.WithTimeout(context.WithoutCancel(ctx), s.ReceiptTimeout)
	}
	return context.WithCancel(context.WithoutCancel(ctx))
}
