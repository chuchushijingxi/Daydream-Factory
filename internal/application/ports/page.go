package ports

import "drama-agent/internal/domain/fault"

// Page 是全部列表视图共用的分页信封；它出现在应用端口签名里，因此归属 ports，
// 各业务上下文不得在 accounts 等具体模块再定义一份或跨包借用。
type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// MaxSafeInteger 是 2^53-1：浏览器 JS Number 的安全整数上界。
// 分页、修订号与积分金额都用它拒绝超出前端可精确表示的输入。
const MaxSafeInteger = 1<<53 - 1

// CheckPage 拒绝越界的分页输入；上界防误传，不承载业务容量假设。
func CheckPage(page, size int) error {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return fault.ErrInvalid
	}
	return nil
}
