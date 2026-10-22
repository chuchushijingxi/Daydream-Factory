package spaces

import (
	"context"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	p "drama-agent/internal/domain/payment"
	d "drama-agent/internal/domain/spaces"
)

// RechargeTargets 是支付充值目标端口的团队实现：个人空间，加本人为所有者、
// 且未到期未停用的团队。团队钱包账户即团队空间 ID；只在下单时校验，到账不复核。
func (s *Service) RechargeTargets() ports.RechargeTargets { return rechargeTargets{s} }

type rechargeTargets struct{ s *Service }

func personalTarget(user string) p.Target {
	return p.Target{Kind: "personal", ID: user, Name: "个人空间"}
}
func (t rechargeTargets) List(ctx context.Context, a ports.Actor) ([]p.Target, error) {
	if err := a.Require(); err != nil {
		return nil, err
	}
	rows, err := t.s.Store.MySpaces(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	out := []p.Target{personalTarget(a.UserID)}
	for _, row := range rows {
		if row.Kind == d.TeamKind && row.Role == d.Owner && row.Status == d.Active {
			out = append(out, p.Target{Kind: "team", ID: row.ID, Name: row.Name})
		}
	}
	return out, nil
}
func (t rechargeTargets) Resolve(ctx context.Context, a ports.Actor, target p.Target) (ports.ResolvedRechargeTarget, error) {
	if err := a.Require(); err != nil {
		return ports.ResolvedRechargeTarget{}, err
	}
	forbidden := fault.New("PAYMENT_TARGET_FORBIDDEN", "不能向此空间充值")
	switch target.Kind {
	case "personal":
		if target.ID != a.UserID {
			return ports.ResolvedRechargeTarget{}, forbidden
		}
		return ports.ResolvedRechargeTarget{Target: personalTarget(a.UserID), AccountID: a.UserID}, nil
	case "team":
		rows, err := t.s.Store.MySpaces(ctx, a.UserID)
		if err != nil {
			return ports.ResolvedRechargeTarget{}, err
		}
		for _, row := range rows {
			if row.Kind != d.TeamKind || row.ID != target.ID {
				continue
			}
			// 管理员与成员按无权限处理，不暴露团队状态（T2）。
			if row.Role != d.Owner {
				return ports.ResolvedRechargeTarget{}, forbidden
			}
			if row.Status != d.Active {
				return ports.ResolvedRechargeTarget{}, fault.New("PAYMENT_TARGET_INACTIVE", "团队已到期或停用，暂不能充值")
			}
			return ports.ResolvedRechargeTarget{Target: p.Target{Kind: "team", ID: row.ID, Name: row.Name}, AccountID: row.ID}, nil
		}
	}
	return ports.ResolvedRechargeTarget{}, forbidden
}
