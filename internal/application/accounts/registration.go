package accounts

import (
	"context"

	"drama-agent/internal/application/credits"
)

type Registration struct {
	ID                string
	Email             string
	Username          string
	Code              string
	AuthorizationCode string
	PasswordHash      []byte
}

// RegistrationTx 仅在注册回调内有效，建用户、核销资格与赠送共用一个事务。
type RegistrationTx interface {
	CreateUser(context.Context, Registration) (User, error)
	credits.RegistrationGiftStore
}

type RegistrationStore interface {
	WithRegistration(context.Context, func(RegistrationTx) (User, error)) (User, error)
}

func Register(ctx context.Context, store RegistrationStore, in Registration) (User, error) {
	return store.WithRegistration(ctx, func(tx RegistrationTx) (User, error) {
		policy, err := tx.CreditGiftPolicy(ctx)
		if err != nil {
			return User{}, err
		}
		user, err := tx.CreateUser(ctx, in)
		if err != nil {
			return User{}, err
		}
		if err = credits.GrantRegistrationGift(ctx, tx, user.ID, policy); err != nil {
			return User{}, err
		}
		return user, nil
	})
}
