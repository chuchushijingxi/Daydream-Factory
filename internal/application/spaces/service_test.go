package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	c "drama-agent/internal/domain/credits"
	"drama-agent/internal/domain/fault"
	n "drama-agent/internal/domain/notifications"
	d "drama-agent/internal/domain/spaces"
)

type memoryState struct {
	sharingResources map[string]d.Resource
	shares           map[string]d.Share
	wallet           c.Wallet
	allocations      map[string]c.HolderAllocation
	allocationEvents map[string]c.AllocationEvent
	creditOperations map[string]c.Entry
	grantSchedule    d.GrantSchedule
	team             d.Team
	members          map[string]Member
	users            map[string]User
	invites          map[string]Invite
	requests         map[string]JoinRequest
	transfers        map[string]OwnershipTransfer
	reminders        map[string]bool
	audit            []AuditEvent
	notifications    []n.SystemInput
}

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (st memoryState) clone() memoryState {
	st.sharingResources = cloneMap(st.sharingResources)
	st.shares = cloneMap(st.shares)
	st.allocations = cloneMap(st.allocations)
	st.allocationEvents = cloneMap(st.allocationEvents)
	st.creditOperations = cloneMap(st.creditOperations)
	st.members = cloneMap(st.members)
	st.users = cloneMap(st.users)
	st.invites = cloneMap(st.invites)
	st.requests = cloneMap(st.requests)
	st.transfers = cloneMap(st.transfers)
	st.reminders = cloneMap(st.reminders)
	st.audit = append([]AuditEvent{}, st.audit...)
	st.notifications = append([]n.SystemInput{}, st.notifications...)
	return st
}

type memoryStore struct {
	Store
	mu         sync.Mutex
	state      memoryState
	failNotify bool
	failAudit  bool
	at         time.Time
}
type memoryTx struct {
	Tx
	store *memoryStore
	state *memoryState
}

func (s *memoryStore) SpaceTx(ctx context.Context, id string, fn func(Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state.clone()
	if err := fn(&memoryTx{store: s, state: &next}); err != nil {
		return err
	}
	s.state = next
	return nil
}
func (tx *memoryTx) Team(context.Context) (d.Team, error) {
	if tx.state.team.ID == "" {
		return d.Team{}, fault.ErrNotFound
	}
	return tx.state.team, nil
}
func (tx *memoryTx) CreateTeam(_ context.Context, team d.Team, _ []ModelDiscount) error {
	tx.state.team = team
	tx.state.members[team.OwnerID] = Member{UserID: team.OwnerID, Role: d.Owner}
	return nil
}
func (tx *memoryTx) SaveTeam(_ context.Context, team d.Team, rev uint64) error {
	if tx.state.team.Revision != rev {
		return fault.ErrConflict
	}
	tx.state.team = team
	return nil
}
func (tx *memoryTx) SetTeamDiscounts(context.Context, []ModelDiscount) error { return nil }
func (tx *memoryTx) Member(_ context.Context, id string) (Member, error) {
	v, ok := tx.state.members[id]
	if !ok {
		return Member{}, fault.ErrNotFound
	}
	return v, nil
}
func (tx *memoryTx) Members(context.Context) ([]Member, error) {
	var out []Member
	for _, v := range tx.state.members {
		out = append(out, v)
	}
	return out, nil
}
func (tx *memoryTx) MemberCount(context.Context) (int, error) { return len(tx.state.members), nil }
func (tx *memoryTx) AddMember(_ context.Context, id string, role d.Role, at time.Time) error {
	if _, ok := tx.state.members[id]; ok {
		return fault.ErrConflict
	}
	tx.state.members[id] = Member{UserID: id, Role: role, JoinedAt: &at}
	if role.HoldsCredits() {
		tx.openHolder(id)
	}
	return nil
}

// openHolder mirrors ensureHolderOpenTx: it opens a missing or closed empty holder and never resets a balance.
func (tx *memoryTx) openHolder(id string) {
	held, ok := tx.state.allocations[id]
	if !ok || (!held.Open && held.Balance == 0) {
		tx.state.allocations[id] = c.HolderAllocation{AccountID: tx.state.team.ID, HolderID: id, Open: true}
	}
}
func (tx *memoryTx) RemoveMember(_ context.Context, id string) error {
	delete(tx.state.members, id)
	return nil
}
func (tx *memoryTx) SetMemberRole(_ context.Context, id string, role d.Role) error {
	m, ok := tx.state.members[id]
	if !ok {
		return fault.ErrNotFound
	}
	m.Role = role
	tx.state.members[id] = m
	if role.HoldsCredits() {
		tx.openHolder(id)
	}
	return nil
}
func (tx *memoryTx) FindUser(_ context.Context, id string) (User, error) {
	for _, v := range tx.state.users {
		if v.ID == id || v.Email == id {
			return v, nil
		}
	}
	return User{}, fault.ErrNotFound
}
func (tx *memoryTx) Invite(_ context.Context, id string) (Invite, error) {
	v, ok := tx.state.invites[id]
	if !ok {
		return Invite{}, fault.ErrNotFound
	}
	return v, nil
}
func (tx *memoryTx) SaveInvite(_ context.Context, v Invite) error {
	tx.state.invites[v.ID] = v
	return nil
}
func (tx *memoryTx) JoinRequest(_ context.Context, id string) (JoinRequest, error) {
	v, ok := tx.state.requests[id]
	if !ok {
		return JoinRequest{}, fault.ErrNotFound
	}
	return v, nil
}
func (tx *memoryTx) SaveJoinRequest(_ context.Context, v JoinRequest) error {
	tx.state.requests[v.ID] = v
	return nil
}
func (tx *memoryTx) OwnershipTransfer(_ context.Context, id string) (OwnershipTransfer, error) {
	v, ok := tx.state.transfers[id]
	if !ok {
		return OwnershipTransfer{}, fault.ErrNotFound
	}
	return v, nil
}
func (tx *memoryTx) PendingOwnershipTransfers(context.Context) ([]OwnershipTransfer, error) {
	var pending []OwnershipTransfer
	for _, transfer := range tx.state.transfers {
		if transfer.SpaceID == tx.state.team.ID && transfer.Status == "pending" {
			pending = append(pending, transfer)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })
	return pending, nil
}
func (tx *memoryTx) SaveOwnershipTransfer(_ context.Context, v OwnershipTransfer) error {
	if v.Status == "pending" {
		for _, existing := range tx.state.transfers {
			if existing.SpaceID == v.SpaceID && existing.Status == "pending" && existing.ID != v.ID {
				return fault.New("SPACE_CONFLICT", "已有待确认的所有权转让")
			}
		}
	}
	tx.state.transfers[v.ID] = v
	return nil
}
func (tx *memoryTx) AppendAudit(_ context.Context, v AuditEvent) error {
	if tx.store.failAudit {
		return errors.New("audit storage failed")
	}
	tx.state.audit = append(tx.state.audit, v)
	return nil
}
func (tx *memoryTx) SystemNotify(_ context.Context, v n.SystemInput) error {
	if tx.store.failNotify {
		return errors.New("notification storage failed")
	}
	tx.state.notifications = append(tx.state.notifications, v)
	return nil
}
func (tx *memoryTx) RecordReminder(_ context.Context, kind, key string) (bool, error) {
	id := kind + ":" + key
	if tx.state.reminders[id] {
		return false, nil
	}
	tx.state.reminders[id] = true
	return true, nil
}
func (s *memoryStore) TeamView(context.Context, string) (TeamView, error) {
	t := s.state.team
	return TeamView{ID: t.ID, OwnerID: t.OwnerID, Name: t.Name, Policy: t.Policy, Revision: t.Revision, MemberLimit: t.MemberLimit}, nil
}
func (s *memoryStore) SpaceInvite(_ context.Context, id string) (Invite, error) {
	return (&memoryTx{state: &s.state}).Invite(context.Background(), id)
}
func (s *memoryStore) InviteByDigest(_ context.Context, digest string) (Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.state.invites {
		if v.TokenDigest == digest {
			return v, nil
		}
	}
	return Invite{}, fault.ErrNotFound
}
func (s *memoryStore) DueTeamReminders(context.Context, time.Time, int) ([]d.Team, error) {
	return []d.Team{s.state.team}, nil
}
func (s *memoryStore) SpaceMembers(context.Context, string) ([]Member, error) {
	return (&memoryTx{state: &s.state}).Members(context.Background())
}
func (s *memoryStore) Resolve(_ context.Context, a ports.Actor, id string) (ports.SpaceScope, error) {
	m, ok := s.state.members[a.UserID]
	if !ok || s.state.team.Disabled || s.state.team.ID != id {
		return ports.SpaceScope{}, fault.ErrNotFound
	}
	return ports.SpaceScope{SpaceID: id, UserID: a.UserID, Kind: d.TeamKind, Role: m.Role, Status: s.state.team.Status(s.at), ReadOnly: s.state.team.ReadOnly(s.at, s.state.wallet.Balance)}, nil
}

type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return true, nil
}
func (allowLimiter) ResetLimit(context.Context, string) error { return nil }
func fixture() (context.Context, *Service, *memoryStore) {
	at := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	members := map[string]Member{}
	users := map[string]User{}
	for id, role := range map[string]d.Role{"owner": d.Owner, "admin": d.Admin, "member": d.Member} {
		joined := at.Add(-time.Hour)
		members[id] = Member{UserID: id, Name: id, Email: id + "@example.test", Role: role, JoinedAt: &joined}
		users[id] = User{ID: id, Name: id, Email: id + "@example.test", Enabled: true}
	}
	for _, id := range []string{"new-a", "new-b"} {
		users[id] = User{ID: id, Name: id, Email: id + "@example.test", Enabled: true}
	}
	store := &memoryStore{at: at, state: memoryState{team: d.Team{ID: "team_test", Name: "测试团队", OwnerID: "owner", MemberLimit: 4, ExpiresAt: at.Add(30 * 24 * time.Hour), Policy: d.ReadAll, Revision: 1}, members: members, users: users, invites: map[string]Invite{}, requests: map[string]JoinRequest{}, transfers: map[string]OwnershipTransfer{}, reminders: map[string]bool{}}}
	store.state.allocations = map[string]c.HolderAllocation{"member": {AccountID: store.state.team.ID, HolderID: "member", Open: true}}
	store.state.allocationEvents = map[string]c.AllocationEvent{}
	store.state.creditOperations = map[string]c.Entry{}
	store.state.grantSchedule = d.InitialGrant(at)
	return context.Background(), &Service{Store: store, Resolver: store, Limiter: allowLimiter{}, Now: func() time.Time { return store.at }, AllocationQueries: store}, store
}
func actor(id string) ports.Actor { return ports.Actor{UserID: id, Role: ports.RoleUser} }
func hasCode(err error, code string) bool {
	var f *fault.Error
	return errors.As(err, &f) && f.Code == code
}

func TestAdministratorLimitsAndMemberPrivacy(t *testing.T) {
	ctx, s, store := fixture()
	for _, target := range []string{"owner", "admin"} {
		if err := s.RemoveMember(ctx, actor("admin"), store.state.team.ID, target); !hasCode(err, "ROLE_FORBIDDEN") {
			t.Fatal(target, err)
		}
	}
	if err := s.SetRole(ctx, actor("admin"), store.state.team.ID, "member", d.Admin); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal(err)
	}
	if _, err := s.BeginOwnership(ctx, actor("admin"), store.state.team.ID, "member"); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal(err)
	}
	if err := s.Leave(ctx, actor("owner"), store.state.team.ID); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal(err)
	}
	members, err := s.Members(ctx, actor("member"), store.state.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.Email != "" || m.JoinedAt != nil {
			t.Fatal("member received private member fields", m)
		}
	}
	operator := ports.Actor{UserID: "operator", Role: ports.RoleOperator}
	if err := s.RemoveMember(ctx, operator, store.state.team.ID, "member"); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("operator gained member operations", err)
	}
}
func TestInviteSecretCapacityAndNotificationsShareTransaction(t *testing.T) {
	ctx, s, store := fixture()
	link, err := s.CreateInviteLink(ctx, actor("owner"), store.state.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(link.Token) || store.state.invites[link.ID].TokenDigest == link.Token || link.ExpiresAt.Sub(link.CreatedAt) != 7*24*time.Hour {
		t.Fatal("unsafe invite token or expiry", link)
	}
	raw, _ := json.Marshal(link.Invite)
	if strings.Contains(string(raw), link.Token) || strings.Contains(string(raw), link.TokenDigest) {
		t.Fatal("token or digest exposed in invite DTO")
	}
	store.failNotify = true
	if _, err = s.InviteEmail(ctx, actor("owner"), store.state.team.ID, "new-a@example.test"); err == nil {
		t.Fatal("notification failed but invite succeeded")
	}
	if len(store.state.invites) != 1 || len(store.state.audit) != 1 {
		t.Fatal("failed notification committed invite or audit")
	}
	store.failNotify = false
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, user := range []string{"new-a", "new-b"} {
		wg.Add(1)
		go func(user string) { defer wg.Done(); _, e := s.AcceptLink(ctx, actor(user), link.Token); results <- e }(user)
	}
	wg.Wait()
	close(results)
	success, full := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if hasCode(e, "TEAM_MEMBER_LIMIT") {
			full++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || full != 1 || len(store.state.members) != 4 {
		t.Fatal(success, full, len(store.state.members))
	}
}
func TestOwnershipRequiresCurrentOwnerAndSwapsRolesAtomically(t *testing.T) {
	ctx, s, store := fixture()
	transfer, err := s.BeginOwnership(ctx, actor("owner"), store.state.team.ID, "member")
	if err != nil {
		t.Fatal(err)
	}
	store.state.team.OwnerID = "admin"
	if err = s.AcceptOwnership(ctx, actor("member"), store.state.team.ID, transfer.ID); !hasCode(err, "TEAM_OWNERSHIP_CONFLICT") {
		t.Fatal(err)
	}
	store.state.team.OwnerID = "owner"
	store.failNotify = true
	if err = s.AcceptOwnership(ctx, actor("member"), store.state.team.ID, transfer.ID); err == nil {
		t.Fatal("ownership transfer ignored notification failure")
	}
	if store.state.members["owner"].Role != d.Owner || store.state.team.OwnerID != "owner" {
		t.Fatal("partial ownership exchange committed")
	}
	store.failNotify = false
	if err = s.AcceptOwnership(ctx, actor("member"), store.state.team.ID, transfer.ID); err != nil {
		t.Fatal(err)
	}
	if store.state.members["owner"].Role != d.Admin || store.state.members["member"].Role != d.Owner || store.state.team.OwnerID != "member" {
		t.Fatal("roles did not exchange")
	}
}
func TestExpiredDisabledAndLoweredLimitPreserveMembers(t *testing.T) {
	ctx, s, store := fixture()
	store.state.team.ExpiresAt = s.now()
	if _, err := s.CreateInviteLink(ctx, actor("owner"), store.state.team.ID); !hasCode(err, "TEAM_EXPIRED") {
		t.Fatal(err)
	}
	store.state.team.Disabled = true
	if err := s.Leave(ctx, actor("member"), store.state.team.ID); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal(err)
	}
	store.state.team.Disabled = false
	limit := 1
	operator := ports.Actor{UserID: "operator", Role: ports.RoleOperator}
	if _, err := s.OperatorUpdate(ctx, operator, store.state.team.ID, TeamPatch{Revision: 1, MemberLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	if len(store.state.members) != 3 || store.state.team.MemberLimit != 1 {
		t.Fatal("lowering limit removed members")
	}
}
func TestReminderIsPerExpiryAndOnlyTargetsManagers(t *testing.T) {
	ctx, s, store := fixture()
	store.state.team.ExpiresAt = s.now().Add(7 * 24 * time.Hour)
	if count, err := s.Schedule(ctx, s.now(), 20); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if count, err := s.Schedule(ctx, s.now(), 20); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	var recipients []string
	for _, message := range store.state.notifications {
		recipients = append(recipients, message.RecipientUserID)
	}
	if len(recipients) != 2 || !sort.StringsAreSorted(recipients) || recipients[0] != "admin" || recipients[1] != "owner" {
		t.Fatal(recipients)
	}
	store.state.team.ExpiresAt = store.state.team.ExpiresAt.Add(time.Minute)
	if count, err := s.Schedule(ctx, s.now(), 20); err != nil || count != 1 {
		t.Fatal("new expiry reused old reminder period", count, err)
	}
}
func TestWorkspaceOperatorAccountIsAuditedAsUserAction(t *testing.T) {
	ctx, s, store := fixture()
	a := ports.Actor{UserID: "owner", Role: ports.RoleOperator}
	if _, err := s.Update(ctx, a, store.state.team.ID, SettingsInput{Revision: 1, Name: "新名称", Policy: d.ReadAll}); err != nil {
		t.Fatal(err)
	}
	if len(store.state.audit) != 1 || store.state.audit[0].ActorKind != "user" {
		t.Fatal(store.state.audit)
	}
}

func TestJoinApprovalAndRejectionAreAtomicWithResultNotice(t *testing.T) {
	ctx, s, store := fixture()
	request, err := s.RequestJoin(ctx, actor("new-a"), store.state.team.ID, "希望加入")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.state.notifications) != 2 {
		t.Fatal("request did not notify all managers")
	}
	store.failNotify = true
	if err = s.DecideJoin(ctx, actor("admin"), store.state.team.ID, request.ID, true); err == nil {
		t.Fatal("approval ignored result notification failure")
	}
	if _, ok := store.state.members["new-a"]; ok || store.state.requests[request.ID].Status != "pending" {
		t.Fatal("partial approval committed")
	}
	store.failNotify = false
	if err = s.DecideJoin(ctx, actor("admin"), store.state.team.ID, request.ID, true); err != nil {
		t.Fatal(err)
	}
	if store.state.requests[request.ID].Status != "approved" || store.state.members["new-a"].Role != d.Member {
		t.Fatal("approved request did not join")
	}
	if err = s.DecideJoin(ctx, actor("admin"), store.state.team.ID, request.ID, false); !hasCode(err, "TEAM_REQUEST_CONFLICT") {
		t.Fatal("decision was reversible", err)
	}
	other, err := s.RequestJoin(ctx, actor("new-b"), store.state.team.ID, "")
	if err != nil {
		t.Fatal("full team should still accept a pending application", err)
	}
	if err = s.DecideJoin(ctx, actor("owner"), store.state.team.ID, other.ID, true); !hasCode(err, "TEAM_MEMBER_LIMIT") {
		t.Fatal(err)
	}
	if err = s.DecideJoin(ctx, actor("owner"), store.state.team.ID, other.ID, false); err != nil {
		t.Fatal(err)
	}
	if store.state.requests[other.ID].Status != "rejected" {
		t.Fatal("rejection did not persist")
	}
}

func TestOpenIsOperatorOnlyAndDefaultsToReadAll(t *testing.T) {
	ctx, s, store := fixture()
	in := TeamInput{OwnerID: "owner", Name: " 新团队 ", MemberLimit: 2, ExpiresAt: s.now().Add(time.Hour), GrantAmount: 0, ModelDiscounts: []ModelDiscount{{ModelInstanceID: "image", Percent: 85}}}
	if _, err := s.Open(ctx, actor("owner"), in); !hasCode(err, "ROLE_FORBIDDEN") {
		t.Fatal(err)
	}
	store.state.team = d.Team{}
	store.state.members = map[string]Member{}
	view, err := s.Open(ctx, ports.Actor{UserID: "operator", Role: ports.RoleOperator}, in)
	if err != nil {
		t.Fatal(err)
	}
	if view.Policy != d.ReadAll || view.Name != "新团队" || len(store.state.members) != 1 || store.state.members["owner"].Role != d.Owner {
		t.Fatal(view, store.state.members)
	}
	if len(store.state.audit) != 1 || store.state.audit[0].ActorKind != "operator" {
		t.Fatal(store.state.audit)
	}
}

func TestReusableLinkAssignsNewNotificationIdentityAfterRejoining(t *testing.T) {
	ctx, s, store := fixture()
	link, err := s.CreateInviteLink(ctx, actor("owner"), store.state.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptLink(ctx, actor("new-a"), link.Token); err != nil {
		t.Fatal(err)
	}
	firstOperation := store.state.notifications[0].OperationID
	if err = s.Leave(ctx, actor("new-a"), store.state.team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptLink(ctx, actor("new-a"), link.Token); err != nil {
		t.Fatal(err)
	}
	if len(store.state.notifications) != 4 || firstOperation == store.state.notifications[2].OperationID {
		t.Fatal("second join reused the first join's notice identity")
	}
}

func TestManagementWritesAuthorizePathTeamIndependentlyOfSelectedSpace(t *testing.T) {
	for _, test := range []struct {
		name          string
		user          string
		selectedSpace string
		wantError     string
	}{
		{"manager-selected-another-space", "admin", "another-space", ""},
		{"nonmember-selecting-path-team", "new-a", "team_test", "NOT_FOUND"},
		{"member-selecting-owner-personal-space", "member", "owner", "ROLE_FORBIDDEN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, service, store := fixture()
			ctx = ports.WithRequestedSpace(ctx, test.selectedSpace)
			teamID := store.state.team.ID
			previous := store.state.team
			view, err := service.Update(ctx, actor(test.user), teamID, SettingsInput{
				Revision: previous.Revision,
				Name:     "路径团队的新名称",
				Policy:   d.EditAll,
			})
			if test.wantError == "" {
				if err != nil || view.ID != teamID || view.Name != "路径团队的新名称" || store.state.team.Policy != d.EditAll {
					t.Fatal("current selection blocked authorized management of the path team", view, err)
				}
				if len(store.state.audit) != 2 || store.state.audit[0].SpaceID != teamID || store.state.audit[0].ActorID != test.user {
					t.Fatal("management audit was not attributed to the authorized path team", store.state.audit)
				}
				return
			}
			if test.wantError == "NOT_FOUND" {
				if !errors.Is(err, fault.ErrNotFound) {
					t.Fatal("selection granted a nonmember access to the path team", err)
				}
			} else if !hasCode(err, test.wantError) {
				t.Fatal("selection granted a member management authority", err)
			}
			if store.state.team != previous || len(store.state.audit) != 0 {
				t.Fatal("denied management changed the path team or appended audit", store.state.team, store.state.audit)
			}
		})
	}
}

func TestNewOwnershipProposalReplacesPreviousConfirmation(t *testing.T) {
	for _, target := range []string{"admin", "member"} {
		t.Run("new-target-"+target, func(t *testing.T) {
			ctx, service, store := fixture()
			teamID := store.state.team.ID
			previous, err := service.BeginOwnership(ctx, actor("owner"), teamID, "member")
			if err != nil {
				t.Fatal(err)
			}
			next, err := service.BeginOwnership(ctx, actor("owner"), teamID, target)
			if err != nil {
				t.Fatal(err)
			}
			if previous.ID == next.ID || store.state.transfers[previous.ID].Status != "cancelled" || store.state.transfers[next.ID].Status != "pending" {
				t.Fatal("new proposal did not replace the previous confirmation", store.state.transfers)
			}
			if store.state.team.OwnerID != "owner" || store.state.members["owner"].Role != d.Owner {
				t.Fatal("proposal changed ownership before recipient confirmation")
			}
			pending, err := (&memoryTx{state: &store.state}).PendingOwnershipTransfers(ctx)
			if err != nil || len(pending) != 1 || pending[0].ID != next.ID {
				t.Fatal(pending, err)
			}
			var superseded *AuditEvent
			for i := range store.state.audit {
				if store.state.audit[i].Action == "ownership_superseded" {
					superseded = &store.state.audit[i]
				}
			}
			if superseded == nil || superseded.Target != previous.ID {
				t.Fatal("supersession audit omitted previous transfer", store.state.audit)
			}
			var relation map[string]string
			if err = json.Unmarshal(superseded.Detail, &relation); err != nil || relation["oldTransferId"] != previous.ID || relation["newTransferId"] != next.ID {
				t.Fatal(relation, err)
			}
			auditsBefore := len(store.state.audit)
			if err = service.AcceptOwnership(ctx, actor("member"), teamID, previous.ID); !hasCode(err, "TEAM_OWNERSHIP_CONFLICT") {
				t.Fatal("superseded confirmation still changed ownership", err)
			}
			if store.state.team.OwnerID != "owner" || len(store.state.audit) != auditsBefore {
				t.Fatal("stale confirmation changed team or audit")
			}
			if len(store.state.notifications) != 2 || store.state.notifications[0].Link.TargetID != previous.ID || store.state.notifications[1].Link.TargetID != next.ID {
				t.Fatal("new proposal did not retain distinct notification links", store.state.notifications)
			}
			if err = service.AcceptOwnership(ctx, actor(target), teamID, next.ID); err != nil {
				t.Fatal(err)
			}
			if store.state.team.OwnerID != target || store.state.members["owner"].Role != d.Admin || store.state.members[target].Role != d.Owner {
				t.Fatal("new recipient could not accept ownership")
			}
		})
	}
}

func TestOwnershipReplacementNotificationFailureKeepsPreviousProposalEffective(t *testing.T) {
	ctx, service, store := fixture()
	teamID := store.state.team.ID
	previous, err := service.BeginOwnership(ctx, actor("owner"), teamID, "member")
	if err != nil {
		t.Fatal(err)
	}
	auditsBefore, noticesBefore := len(store.state.audit), len(store.state.notifications)
	store.failNotify = true
	next, err := service.BeginOwnership(ctx, actor("owner"), teamID, "admin")
	if err == nil {
		t.Fatal("replacement ignored notification failure")
	}
	if len(store.state.transfers) != 1 || store.state.transfers[previous.ID].Status != "pending" || len(store.state.audit) != auditsBefore || len(store.state.notifications) != noticesBefore {
		t.Fatal("failed replacement committed old cancellation, new proposal, audit or notice", store.state.transfers)
	}
	if _, exists := store.state.transfers[next.ID]; exists {
		t.Fatal("failed replacement persisted its new proposal")
	}
	store.failNotify = false
	if err = service.AcceptOwnership(ctx, actor("member"), teamID, previous.ID); err != nil {
		t.Fatal("rollback invalidated the previous confirmation", err)
	}
	if store.state.team.OwnerID != "member" {
		t.Fatal("previous recipient could not accept after rollback")
	}
}

func TestInvalidOwnershipRecipientDoesNotReplaceExistingProposal(t *testing.T) {
	for _, target := range []string{"new-a", "admin"} {
		t.Run(target, func(t *testing.T) {
			ctx, service, store := fixture()
			teamID := store.state.team.ID
			previous, err := service.BeginOwnership(ctx, actor("owner"), teamID, "member")
			if err != nil {
				t.Fatal(err)
			}
			if target == "admin" {
				user := store.state.users[target]
				user.Enabled = false
				store.state.users[target] = user
			}
			if _, err = service.BeginOwnership(ctx, actor("owner"), teamID, target); err == nil {
				t.Fatal("invalid recipient was accepted")
			}
			if len(store.state.transfers) != 1 || store.state.transfers[previous.ID].Status != "pending" || len(store.state.audit) != 1 || len(store.state.notifications) != 1 {
				t.Fatal("invalid proposal replaced the current confirmation", store.state.transfers)
			}
		})
	}
}

func TestConcurrentOwnershipProposalsLeaveOnlyLatestPending(t *testing.T) {
	ctx, service, store := fixture()
	teamID := store.state.team.ID
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, target := range []string{"admin", "member"} {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			_, err := service.BeginOwnership(ctx, actor("owner"), teamID, target)
			results <- err
		}(target)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	pending, cancelled := 0, 0
	for _, transfer := range store.state.transfers {
		switch transfer.Status {
		case "pending":
			pending++
		case "cancelled":
			cancelled++
		}
	}
	if pending != 1 || cancelled != 1 || len(store.state.notifications) != 2 || store.state.team.OwnerID != "owner" {
		t.Fatal("concurrent proposals did not serialize replacement before confirmation", store.state.transfers)
	}
}

func TestNotificationNamesAreMarkdownEscapedWithoutChangingStoredNames(t *testing.T) {
	ctx, service, store := fixture()
	teamName, userName := "团*队* ![图](x)", "新[人](x)&copy;"
	store.state.team.Name = teamName
	user := store.state.users["new-a"]
	user.Name = userName
	store.state.users[user.ID] = user
	invite, err := service.InviteEmail(ctx, actor("owner"), store.state.team.ID, user.Email)
	if err != nil {
		t.Fatal(err)
	}
	inviteNotice := store.state.notifications[0]
	if inviteNotice.Title != "你收到团队邀请" || !strings.Contains(inviteNotice.Body, n.EscapeMarkdownText(teamName)) || strings.Contains(inviteNotice.Body, teamName) {
		t.Fatal("invite notice interpreted the dynamic name as Markdown or changed its plain title", inviteNotice)
	}
	if _, err = service.AcceptInvite(ctx, actor(user.ID), invite.ID); err != nil {
		t.Fatal(err)
	}
	for _, notice := range store.state.notifications[1:] {
		if !strings.Contains(notice.Body, n.EscapeMarkdownText(teamName)) || !strings.Contains(notice.Body, n.EscapeMarkdownText(userName)) || strings.Contains(notice.Body, userName) {
			t.Fatal("join notice did not preserve dynamic team/member names", notice)
		}
		if !strings.Contains(notice.Body, "已接受邀请并加入团队。") {
			t.Fatal("entire message was escaped instead of name interpolation", notice)
		}
	}
	if store.state.team.Name != teamName || store.state.users[user.ID].Name != userName {
		t.Fatal("notification formatting rewrote source names")
	}
}

type teamWatchRecorder struct{ spaces []string }

func (r *teamWatchRecorder) WatchTeam(_ context.Context, space string) (<-chan struct{}, func(), error) {
	r.spaces = append(r.spaces, space)
	return make(chan struct{}), func() {}, nil
}

func TestTeamStreamIsMemberOnlyAndRecheckedAfterRemoval(t *testing.T) {
	ctx, s, store := fixture()
	watches := &teamWatchRecorder{}
	s.TeamUpdates = watches
	id := store.state.team.ID
	for _, user := range []string{"owner", "member"} {
		if _, closeWatch, err := s.WatchTeam(ctx, actor(user), id); err != nil {
			t.Fatal(user, err)
		} else {
			closeWatch()
		}
	}
	if len(watches.spaces) != 2 || watches.spaces[0] != id {
		t.Fatal("team stream did not subscribe to the path team", watches.spaces)
	}
	operator := ports.Actor{UserID: "operator", Role: ports.RoleOperator}
	for _, a := range []ports.Actor{actor("new-a"), operator} {
		if _, _, err := s.WatchTeam(ctx, a, id); !errors.Is(err, fault.ErrNotFound) {
			t.Fatal("non-member opened team stream", a.UserID, err)
		}
	}
	if len(watches.spaces) != 2 {
		t.Fatal("rejected caller reached the update source")
	}
	if err := s.RemoveMember(ctx, actor("owner"), id, "member"); err != nil {
		t.Fatal(err)
	}
	if err := s.TeamMember(ctx, actor("member"), id); !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("removed member still passes the per-frame stream check", err)
	}
}
