package main

import (
	"context"
	"fmt"
	"time"

	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/ports"
	spaceapp "drama-agent/internal/application/spaces"
	d "drama-agent/internal/domain/spaces"
)

// seedDemoTeam 在全新安装上建立演示团队：所有者、管理员、成员各一个公开账号，并给团队发放、
// 给成员分配积分，部署后即可体验团队协作、积分分配与充值。已有真实用户的安装不写入任何演示数据。
// 走与运营后台相同的应用服务，团队合同、发放、成员与分配都留有正常的审计记录。
func seedDemoTeam(ctx context.Context, store *postgres.Store) (bool, error) {
	present, err := store.InitializeDemoAccounts(ctx)
	if err != nil || !present {
		// 已有真实用户的安装没有演示账号，不建演示团队。
		return false, err
	}
	owner, admin, member := postgres.DemoAccounts[0], postgres.DemoAccounts[1], postgres.DemoAccounts[2]
	service := &spaceapp.Service{Store: store, Resolver: store, Credits: store, CreditQueries: store, AllocationQueries: store, CreditUpdates: store, TeamUpdates: store}
	ownerActor := ports.Actor{UserID: owner.ID, Role: ports.RoleUser}
	// 所有者已有团队（重复部署）时不再重复建团。
	spaces, err := service.List(ctx, ownerActor)
	if err != nil {
		return false, err
	}
	for _, space := range spaces {
		if space.Kind == d.TeamKind {
			return false, nil
		}
	}
	operator := ports.Actor{UserID: "daydream-initial-operator", Role: ports.RoleOperator}
	team, err := service.Open(ctx, operator, spaceapp.TeamInput{
		OwnerID:     owner.ID,
		Name:        "演示团队",
		MemberLimit: 5,
		ExpiresAt:   time.Now().UTC().AddDate(1, 0, 0),
		GrantAmount: 5000,
	})
	if err != nil {
		return false, fmt.Errorf("open demo team: %w", err)
	}
	at := time.Now().UTC()
	if err = store.SpaceTx(ctx, team.ID, func(tx spaceapp.Tx) error {
		for _, id := range []string{admin.ID, member.ID} {
			if e := tx.AddMember(ctx, id, d.Member, at); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		return false, fmt.Errorf("add demo members: %w", err)
	}
	if err = service.SetRole(ctx, ownerActor, team.ID, admin.ID, d.Admin); err != nil {
		return false, fmt.Errorf("promote demo admin: %w", err)
	}
	for _, grant := range []struct {
		holder  string
		amount  int64
		command string
	}{{admin.ID, 500, "demo-seed-admin-credits"}, {member.ID, 800, "demo-seed-member-credits"}} {
		if _, err = service.Allocate(ctx, ownerActor, team.ID, grant.holder, spaceapp.AllocationInput{CommandID: grant.command, Amount: grant.amount}); err != nil {
			return false, fmt.Errorf("allocate demo credits: %w", err)
		}
	}
	return true, nil
}
