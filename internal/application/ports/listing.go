package ports

import (
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/spaces"
)

// ListFilter selects a subset of resources already visible in the current space.
// Mine uses the immutable creator, never the current content assignee.
type ListFilter string

const (
	ListAll    ListFilter = "all"
	ListMine   ListFilter = "mine"
	ListShared ListFilter = "shared"
)

func (f ListFilter) Normalize() (ListFilter, error) {
	if f == "" {
		return ListAll, nil
	}
	if f != ListAll && f != ListMine && f != ListShared {
		return "", fault.ErrInvalid
	}
	return f, nil
}

// ListMetadata is a caller-specific projection, not persistent aggregate content.
type ListMetadata struct {
	CreatorID       string        `json:"creatorId"`
	CreatorName     string        `json:"creatorName"`
	SharedWithMe    bool          `json:"sharedWithMe"`
	EffectiveAccess spaces.Access `json:"effectiveAccess"`
}
