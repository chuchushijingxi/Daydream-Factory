package project

import (
	"context"
	"errors"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

// chargePricing 在业务受理时冻结身份与折扣；worker只使用Charge，不重查成员或今天的价格。
func (s *Service) chargePricing(ctx context.Context, p d.Project, model string, free bool) (ports.CreditTarget, int, error) {
	var scope ports.SpaceScope
	var err error
	if s.scope == nil {
		// 仅无持久空间的纯内存价格夹具；正式资源必须提供当前用户，不能从负责人反推发起人。
		if p.SpaceID != "" {
			return ports.CreditTarget{}, 0, fault.New("AUTH_REQUIRED", "缺少计费发起人")
		}
		percent := 100
		if free {
			percent = 0
		}
		return ports.CreditTarget{}, percent, nil
	}
	if p.SpaceID == "" {
		return ports.CreditTarget{}, 0, fault.ErrInvalid
	}
	if p.ID == "" {
		scope, err = s.resolveSpace(ctx)
	} else {
		scope, err = ports.RequireSpaceAccess(ctx, s.Access, *s.scope, spaces.Resource{SpaceID: p.SpaceID, OwnerID: p.OwnerID, Kind: spaces.ProjectResource, ID: p.ID}, spaces.Read)
	}
	if err != nil {
		return ports.CreditTarget{}, 0, err
	}
	target, err := ports.ChargeAccount(scope)
	if err != nil {
		return ports.CreditTarget{}, 0, err
	}
	if free {
		return target, 0, nil
	}
	percent := 100
	if scope.Kind == spaces.TeamKind {
		if s.Discounts == nil {
			return ports.CreditTarget{}, 0, fault.New("MODEL_PRICE_UNAVAILABLE", "团队模型折扣尚未配置")
		}
		percent, err = s.Discounts.CreditDiscount(ctx, target.AccountID, model)
		if err != nil {
			return ports.CreditTarget{}, 0, err
		}
		if percent < 1 || percent > 100 {
			return ports.CreditTarget{}, 0, fault.New("MODEL_PRICE_UNAVAILABLE", "团队模型折扣无效")
		}
	}
	return target, percent, nil
}

func generationDebit(project string, charge *c.Charge) c.Change {
	return c.Change{AccountID: charge.AccountID, HolderID: charge.HolderID, Amount: charge.Amount, OperationID: creditDeductionID(charge.ID), Source: project, Subject: charge.ActorID, Reason: "generation"}
}
func generationRefund(chargeID string, debit c.Entry) c.Change {
	return c.Change{HolderID: debit.HolderID, OriginalDeductionID: debit.ID, Amount: debit.Amount, OperationID: creditRefundID(chargeID), Subject: debit.Subject, Reason: "generation-incomplete"}
}
func generationCreditError(err error, charge *c.Charge) error {
	var f *fault.Error
	if !errors.As(err, &f) {
		return err
	}
	if f.Code != "CREDIT_INSUFFICIENT" && f.Code != "CREDIT_HOLDER_INSUFFICIENT" && f.Code != "CREDIT_UNALLOCATED_INSUFFICIENT" {
		return err
	}
	if charge.HolderID != "" {
		return fault.New("MEMBER_CREDITS_INSUFFICIENT", "名下积分不足，请申请分配积分")
	}
	// 个人账户ID等于发起人ID；团队账户与个人身份独立，不能从负责人判断扣款账户。
	if charge.AccountID != "" && charge.AccountID != charge.ActorID {
		return fault.New("TEAM_UNALLOCATED_INSUFFICIENT", "团队未分配积分不足")
	}
	return fault.New("CREDIT_INSUFFICIENT", f.Message)
}
func creditInsufficient(code string) bool {
	return code == "CREDIT_INSUFFICIENT" || code == "MEMBER_CREDITS_INSUFFICIENT" || code == "TEAM_UNALLOCATED_INSUFFICIENT"
}
