package project

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	uploadapp "drama-agent/internal/application/upload"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	"drama-agent/internal/domain/spaces"
	"drama-agent/internal/testsupport/modelregistry"
	"drama-agent/internal/testsupport/uploadfiles"
)

type editingMemory struct {
	mu     sync.Mutex
	owner  ports.ProjectEditor
	err    error
	checks int
}

func (m *editingMemory) AcquireProjectEditing(_ context.Context, _, user, session string) (ports.ProjectEditor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return ports.ProjectEditor{}, m.err
	}
	if m.owner.UserID == "" || m.owner.UserID == user && m.owner.SessionID == session {
		m.owner = ports.ProjectEditor{UserID: user, SessionID: session, ExpiresAt: time.Now().Add(90 * time.Second)}
	}
	return m.owner, nil
}
func (m *editingMemory) RenewProjectEditing(ctx context.Context, id, user, session string) (ports.ProjectEditor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return ports.ProjectEditor{}, m.err
	}
	if m.owner.UserID == user && m.owner.SessionID == session {
		m.owner.ExpiresAt = time.Now().Add(90 * time.Second)
	}
	return m.owner, nil
}
func (m *editingMemory) ReleaseProjectEditing(_ context.Context, _, user, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	if m.owner.UserID == user && m.owner.SessionID == session {
		m.owner = ports.ProjectEditor{}
	}
	return nil
}
func (m *editingMemory) CheckProjectEditing(_ context.Context, _, user, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks++
	if m.err != nil {
		return m.err
	}
	if m.owner.UserID != user || m.owner.SessionID != session {
		return fault.New("PROJECT_EDITING_BY_OTHER", "occupied")
	}
	return nil
}
func (m *editingMemory) ProjectEditorName(_ context.Context, id string) (string, error) {
	return "name:" + id, nil
}
func editingFixture(t *testing.T) (*Service, scopeProjects, *editingMemory, d.Project) {
	t.Helper()
	store := scopeProjects{memory: newMemory()}
	p := d.Project{ID: "editing", SpaceID: "team", OwnerID: "owner", CreatorID: "creator", Revision: 1, ScriptSource: d.ScriptSource{Version: 1}, Runs: []d.Run{{ID: "old", Status: d.RunRunning}}}
	if err := store.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	access := &projectAccess{permission: spaces.Edit}
	leases := &editingMemory{}
	service := (&Service{Store: store, Resolver: access, Access: access, Editing: leases, EditorNames: leases, MediaTasks: &taskapp.Service{}}).ForScope(ports.SpaceScope{SpaceID: p.SpaceID, UserID: "editor"}).WithEditingSession("trusted-session")
	return service, store, leases, p
}
func TestEveryProjectUserWriteRequiresItsEditingSession(t *testing.T) {
	service, store, _, p := editingFixture(t)
	ctx := context.Background()
	commands := map[string]func() error{
		"delete":       func() error { _, e := service.DeleteProject(ctx, p.ID, p.Revision); return e },
		"save-asset":   func() error { _, e := service.SaveAsset(ctx, p.ID, p.Revision, "asset", d.AssetInput{}); return e },
		"delete-asset": func() error { _, e := service.DeleteAsset(ctx, p.ID, p.Revision, "asset"); return e },
		"save-look": func() error {
			_, e := service.SaveLook(ctx, p.ID, p.Revision, "asset", "look", d.LookInput{})
			return e
		},
		"delete-look": func() error { _, e := service.DeleteLook(ctx, p.ID, p.Revision, "asset", "look"); return e },
		"visual-prompt": func() error {
			_, e := service.SaveVisualPrompt(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "asset"}, "text")
			return e
		},
		"settings": func() error { _, e := service.SaveSettings(ctx, p.ID, p.Revision, d.ProjectSettings{}); return e },
		"style":    func() error { _, e := service.SaveStyle(ctx, p.ID, p.Revision, "style", "version"); return e },
		"source":   func() error { _, e := service.Upload(ctx, p.ID, p.Revision, "source.txt", []byte("剧本")); return e },
		"image": func() error {
			_, e := service.SaveImage(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "asset"}, "file")
			return e
		},
		"start-catalog": func() error { _, e := service.Start(ctx, p.ID, p.Revision); return e },
		"start-outline": func() error { _, e := service.StartOutline(ctx, p.ID, p.Revision); return e },
		"start-visual":  func() error { _, e := service.StartVisual(ctx, p.ID, p.Revision); return e },
		"retry-index":   func() error { _, e := service.RetryEpisodeIndex(ctx, p.ID, p.Revision); return e },
		"cancel":        func() error { _, e := service.Cancel(ctx, p.ID, p.Revision); return e },
		"start-media":   func() error { _, e := service.StartMedia(ctx, p.ID, MediaCommand{Revision: p.Revision}); return e },
		"cancel-media":  func() error { _, e := service.CancelMedia(ctx, p.ID, "task", 1); return e },
		"batch-image": func() error {
			_, e := service.StartImageBatch(ctx, p.ID, []MediaCommand{{CommandID: "command", Revision: p.Revision, Target: MediaTarget{AssetID: "asset"}}})
			return e
		},
		"direct-commit": func() error { _, e := service.commit(ctx, p, p.Revision); return e },
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) { expectCode(t, command(), "PROJECT_EDITING_BY_OTHER") })
	}
	saved, _ := store.Snapshot(ctx, p.ID)
	if saved.Revision != p.Revision {
		t.Fatal("refused write changed project")
	}
}
func TestProjectEditingRequiresTrustedSessionAndRechecksAtCommit(t *testing.T) {
	s, store, leases, p := editingFixture(t)
	ctx := context.Background()
	first, err := s.AcquireEditing(ctx, p.ID)
	if err != nil || !first.Owned || first.Username != "name:editor" || first.ExpiresAt == nil {
		t.Fatal(first, err)
	}
	other := s.WithEditingSession("another-session")
	view, err := other.AcquireEditing(ctx, p.ID)
	if err != nil || view.Owned || view.UserID != "editor" {
		t.Fatal(view, err)
	}
	if err = other.ReleaseEditing(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.requireEditing(ctx, p); err != nil {
		t.Fatal("other session released owner", err)
	}
	// 用户有权限不代表有占用；Commit再次检查，覆盖进入命令后租约被释放的窗口。
	if err = s.ReleaseEditing(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.commit(ctx, p, p.Revision)
	expectCode(t, err, "PROJECT_EDITING_BY_OTHER")
	if _, err = s.RenewEditing(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if leases.owner.UserID != "" {
		t.Fatal("renew reacquired expired lease")
	}
	missing := s.WithEditingSession("")
	_, err = missing.AcquireEditing(ctx, p.ID)
	expectCode(t, err, "AUTH_REQUIRED")
	copy := *s
	copy.Editing = nil
	// 用新scope绑定到复制后的依赖，不能借旧Store装饰器放行。
	missing = (&copy).ForScope(*s.scope).WithEditingSession("trusted-session")
	_, err = missing.editingProject(ctx, p.ID)
	expectCode(t, err, "RUNTIME_UNAVAILABLE")
	leases.err = fault.New("RUNTIME_UNAVAILABLE", "offline")
	_, err = s.editingProject(ctx, p.ID)
	expectCode(t, err, "RUNTIME_UNAVAILABLE")
	// 后台写不依赖浏览器租约，仍保留revision冲突保护。
	p.Title = "worker"
	worker := (&Service{Store: store.memory}).forSpace(p.SpaceID)
	if _, err = worker.commit(ctx, p, p.Revision); err != nil {
		t.Fatal("worker blocked by editing lease", err)
	}
	_, err = worker.commit(ctx, p, p.Revision)
	if !errors.Is(err, fault.ErrConflict) {
		t.Fatal("worker bypassed revision", err)
	}
}

func TestScopedCreateFromFilePersistsInitialRunWithoutAnExistingEditorLease(t *testing.T) {
	ctx := context.Background()
	selected := ports.SpaceScope{SpaceID: "user", UserID: "user"}
	identity := &teamCreditIdentity{scope: ports.SpaceScope{Kind: spaces.Personal, Role: spaces.Owner, Status: spaces.Active}}
	files := uploadfiles.New()
	files.Resolver, files.Access = identity, identity
	file, err := files.ForScope(selected).Upload(ctx, uploadapp.Input{Filename: "source.txt", Data: []byte("完整虚构故事。")})
	if err != nil {
		t.Fatal(err)
	}
	registry := &modelregistry.Registry{Models: map[string]ports.Model{"model": &fakeModel{}}, DefaultModel: "model", OptionsValue: ports.ModelOptions{Analysis: []ports.ModelOption{{ID: "model"}}}}
	store := scopeProjects{memory: newMemory()}
	base := &Service{Store: store, Resolver: identity, Access: identity, Files: files, Importer: scriptimport.Importer{}, Models: registry, Agent: unitAgent{}, Authorize: UserInitiatedExecution, Prices: teamTextPrice{c.PriceRule{}}, Credits: &capturedGenerationLedger{}}
	// 编辑API在创建返回ID后才可调用，初始聚合与生成意图必须一次Create。
	service := base.ForScope(selected).WithEditingSession("authenticated-session")
	settings := d.ProjectSettings{LLMModel: "model"}
	quote, err := service.QuoteFile(ctx, file.ID, settings)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.WithBillingBasis(quote.Basis).CreateFromFile(ctx, "project", file.ID, 0, CreationChoices{Settings: &settings})
	if err != nil || created.ID == "" || created.LatestRun() == nil || created.LatestRun().Status != d.RunRunning || created.LatestRun().Credit.ActorID != "user" {
		t.Fatal(created, err)
	}
	saved, err := store.Snapshot(ctx, created.ID)
	if err != nil || saved.SourceFileID != file.ID || saved.LatestRun().Credit == nil {
		t.Fatal("initial intent not persisted atomically", saved, err)
	}
	// 用户对已存在项目的下一次写仍必须配置/取得租约，创建例外不能扩成自动抢锁。
	_, err = service.DeleteProject(ctx, created.ID, created.Revision)
	expectCode(t, err, "RUNTIME_UNAVAILABLE")
	failedBase := *base
	failedBase.Agent = nil
	failed, err := failedBase.ForScope(selected).WithEditingSession("authenticated-session").CreateFromFile(ctx, "failed intent", file.ID, 0, CreationChoices{})
	if err != nil || failed.LatestRun() == nil || failed.LatestRun().Status != d.RunFailed || failed.LatestRun().ErrorCode != "MODEL_UNAVAILABLE" {
		t.Fatal("initial failure could not be saved without editor", failed, err)
	}
	saved, err = store.Snapshot(ctx, failed.ID)
	if err != nil || saved.LatestRun().ErrorCode != "MODEL_UNAVAILABLE" {
		t.Fatal(saved, err)
	}
}
