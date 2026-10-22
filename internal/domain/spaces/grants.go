package spaces

import (
	"drama-agent/internal/domain/fault"
	"time"
)

const GrantInterval = 30 * 24 * time.Hour
const maxGrantSequence uint64 = 9007199254740991

// GrantSchedule 是调度事实，序号包含已经跳过的停用周期，确保账务编号永不复用。
type GrantSchedule struct {
	NextAt   time.Time
	Sequence uint64
}

func InitialGrant(at time.Time) GrantSchedule {
	return GrantSchedule{NextAt: at.Add(GrantInterval), Sequence: 1}
}
func (g GrantSchedule) AfterGrant() (GrantSchedule, error) {
	if g.NextAt.IsZero() || g.Sequence == 0 || g.Sequence >= maxGrantSequence {
		return GrantSchedule{}, fault.ErrInvalid
	}
	g.NextAt = g.NextAt.Add(GrantInterval)
	g.Sequence++
	return g, nil
}
func (g GrantSchedule) AfterRenewal(at time.Time) (GrantSchedule, error) {
	if g.Sequence == 0 || g.Sequence >= maxGrantSequence {
		return GrantSchedule{}, fault.ErrInvalid
	}
	return GrantSchedule{NextAt: at.Add(GrantInterval), Sequence: g.Sequence + 1}, nil
}

// AfterEnable 沿原有30天节奏跳过停用期；不补发，也不以启用时刻重新锚定。
func (g GrantSchedule) AfterEnable(at time.Time) (GrantSchedule, error) {
	if g.NextAt.IsZero() || g.Sequence == 0 || g.Sequence > maxGrantSequence {
		return GrantSchedule{}, fault.ErrInvalid
	}
	if g.NextAt.After(at) {
		return g, nil
	}
	seconds := int64(GrantInterval / time.Second)
	skipped := (at.Unix() - g.NextAt.Unix()) / seconds
	next := time.Unix(g.NextAt.Unix()+skipped*seconds, int64(g.NextAt.Nanosecond())).UTC()
	if !next.After(at) {
		skipped++
		next = next.Add(GrantInterval)
	}
	if skipped < 0 || uint64(skipped) > maxGrantSequence-g.Sequence {
		return GrantSchedule{}, fault.ErrInvalid
	}
	g.NextAt = next
	g.Sequence += uint64(skipped)
	return g, nil
}
