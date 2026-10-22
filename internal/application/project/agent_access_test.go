package project

import (
	"context"
	"testing"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
)

type changingAgentAccess struct {
	calls     int
	denyAfter int
	actor     string
	owner     string
}

func (a *changingAgentAccess) Access(ctx context.Context, actor ports.Actor, r spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	a.calls++
	a.actor = actor.UserID
	a.owner = r.OwnerID
	permission := spaces.Edit
	if a.calls > a.denyAfter {
		permission = spaces.Read
	}
	if ports.RequestedSpace(ctx) != "" {
		panic("Agent reused request permission cache")
	}
	return permission, ports.SpaceScope{SpaceID: r.SpaceID, UserID: actor.UserID}, nil
}
func agentPermissionFixture(t *testing.T, denyAfter int) (*Service, *memory, d.Project, *changingAgentAccess) {
	t.Helper()
	store := newMemory()
	args := string(mustJSON(SaveOutline{Outline: d.OutlineInput{Summary: "新保存内容"}}))
	p := d.Project{ID: "agent-project", SpaceID: "team", OwnerID: "transferred-owner", CreatorID: "original-creator", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Runs: []d.Run{{ID: "run", Contract: d.OutlineContract, SourceVersion: 1, Status: d.RunRunning, Stage: "outline", Credit: &c.Charge{AccountID: "team", HolderID: "initiator", ActorID: "initiator"}, Calls: []d.Call{{ID: "call", Stage: "outline", Status: "received", Actions: []d.Action{{ID: "action", Name: "save_outline", Arguments: args, Status: "pending"}}}}}}}
	if err := store.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	access := &changingAgentAccess{denyAfter: denyAfter}
	service := (&Service{Store: store, Access: access, agentUserID: "initiator"}).forSpace(p.SpaceID)
	return service, store, p, access
}
func TestAgentPermissionRevokedBeforeToolStopsAndPreservesPriorContent(t *testing.T) {
	for _, denyAfter := range []int{0, 2} {
		// 0=读前撤销，2=工具已读后、CAS写入前撤销；两者都必须阻止正文采用。
		service, store, p, access := agentPermissionFixture(t, denyAfter)
		ctx := ports.WithRequestedSpace(context.Background(), "stale-http-scope")
		_, err := service.executeTool(ctx, p.ID, "action")
		expectCode(t, err, permissionChangedCode)
		saved, err := store.Snapshot(context.Background(), p.ID)
		if err != nil || saved.Outline != nil || saved.LatestRun().Status != d.RunFailed || saved.LatestRun().ErrorCode != permissionChangedCode || saved.LatestRun().Error != "因权限变更停止" {
			t.Fatal(saved, err)
		}
		if access.actor != "initiator" || access.owner != "transferred-owner" {
			t.Fatal("transfer changed Agent actor/resource", access)
		}
		// 外发证据仍可由可信后台写入，不能因权限撤销丢失已发生费用与完整答复。
		if err = service.update(context.Background(), p.ID, func(current *d.Project) error {
			current.LatestRun().Calls[0].Response = "complete response"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		saved, _ = store.Snapshot(context.Background(), p.ID)
		if saved.LatestRun().Calls[0].Response != "complete response" {
			t.Fatal("receipt lost after permission stop")
		}
	}
}
func TestAgentCanWriteWithPermissionWithoutBrowserEditingLease(t *testing.T) {
	service, store, p, access := agentPermissionFixture(t, 100)
	_, err := service.executeTool(context.Background(), p.ID, "action")
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := store.Snapshot(context.Background(), p.ID)
	if saved.Outline == nil || saved.Outline.Summary != "新保存内容" || access.calls < 3 {
		t.Fatal("Agent did not write with fresh checks", saved, access.calls)
	}
}

func TestAgentPermissionRevokedBeforeNextModelReadDoesNotSendRequest(t *testing.T) {
	service, store, p, _ := agentPermissionFixture(t, 0)
	ctx := ports.WithRequestedSpace(context.Background(), "inherited-request-scope")
	if _, err := service.generateAttempt(ctx, p.ID, "run", nil); err == nil {
		t.Fatal("new model read continued after edit permission revoked")
	} else {
		expectCode(t, err, permissionChangedCode)
	}
	saved, err := store.Snapshot(context.Background(), p.ID)
	if err != nil || len(saved.LatestRun().Calls) != 1 || saved.LatestRun().Status != d.RunFailed || saved.LatestRun().ErrorCode != permissionChangedCode {
		t.Fatal("permission stop created a provider intent or lost reason", saved, err)
	}
}

type disabledAgentAccess struct{ calls int }

func (a *disabledAgentAccess) Access(context.Context, ports.Actor, spaces.Resource) (spaces.Access, ports.SpaceScope, error) {
	a.calls++
	return spaces.None, ports.SpaceScope{}, fault.New("ACCOUNT_DISABLED", "账号已禁用")
}
func TestAgentAccountDisabledStopsAndRecordsPermissionReasonWithoutAdoptingContent(t *testing.T) {
	service, store, p, _ := agentPermissionFixture(t, 100)
	access := &disabledAgentAccess{}
	service.Access = access
	// 使用真实执行入口；在持久任务恢复时账号禁用，既有pending动作不得被采用。
	err := service.Execute(context.Background(), p.ID)
	expectCode(t, err, permissionChangedCode)
	saved, err := store.Snapshot(context.Background(), p.ID)
	if err != nil || access.calls != 1 || saved.Outline != nil || saved.LatestRun().Status != d.RunFailed || saved.LatestRun().ErrorCode != permissionChangedCode || saved.LatestRun().Error != "因权限变更停止" {
		t.Fatal("disabled account did not stop without content adoption", saved, access.calls, err)
	}
	if action := saved.LatestRun().Calls[0].Actions[0]; action.Status != "pending" {
		t.Fatal("disabled account adopted a pending tool", action)
	}
}
