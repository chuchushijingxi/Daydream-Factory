package spaces

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"drama-agent/internal/domain/fault"
)

func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 40 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", fault.New("TEAM_INVALID", "团队名称须为1至40个字符")
	}
	return name, nil
}

func (p Policy) Valid() bool { return p == Isolated || p == ReadAll || p == EditAll }

func (t Team) RequireActive(at time.Time) error {
	switch t.Status(at) {
	case Disabled:
		return fault.ErrNotFound
	case Expired:
		return fault.New("TEAM_EXPIRED", "团队已到期，暂不能加入或邀请")
	default:
		return nil
	}
}

// 人数包括所有者；调低上限不会移出现有成员，只禁止继续加入。
func (t Team) RequireCapacity(count int) error {
	if t.MemberLimit < 1 || count < 0 {
		return fault.ErrInvalid
	}
	if count >= t.MemberLimit {
		return fault.New("TEAM_MEMBER_LIMIT", "团队人数已达上限")
	}
	return nil
}

func RequireInvite(status string, expires time.Time, revoked *time.Time, at time.Time) error {
	if status != "pending" || revoked != nil || !at.Before(expires) {
		return fault.New("INVITE_EXPIRED", "邀请已过期、作废或处理")
	}
	return nil
}

func RequirePendingRequest(status string) error {
	if status != "pending" {
		return fault.New("TEAM_REQUEST_CONFLICT", "加入申请已处理")
	}
	return nil
}

func RequireOwnershipAcceptance(status, fromOwner, currentOwner, toUser, actor string) error {
	if status != "pending" || fromOwner != currentOwner {
		return fault.New("TEAM_OWNERSHIP_CONFLICT", "所有权转让已失效")
	}
	if toUser != actor {
		return fault.ErrNotFound
	}
	return nil
}

// ReminderKinds 按北京时间的到期日发出分段提醒；不补发已错过的更早提醒。
func (t Team) ReminderKinds(at time.Time) []string {
	if t.Disabled {
		return nil
	}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	expiry := t.ExpiresAt.In(zone)
	now := at.In(zone)
	expiryDate := time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, zone)
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	days := int(expiryDate.Sub(nowDate) / (24 * time.Hour))
	switch {
	case days == 0:
		return []string{"expires_today"}
	case days == 1 && t.Status(at) == Active:
		return []string{"expires_1d"}
	case days == 7 && t.Status(at) == Active:
		return []string{"expires_7d"}
	default:
		return nil
	}
}
