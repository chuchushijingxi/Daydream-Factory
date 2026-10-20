package accounts

import "time"

type Role string

const (
	RoleUser     Role = "user"
	RoleOperator Role = "operator"
)

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}
