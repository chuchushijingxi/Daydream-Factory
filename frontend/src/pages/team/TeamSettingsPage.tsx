import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useLocation, useMatch, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { sessionBoundary } from '../../api/session-boundary';
import type { SpaceDetail } from '../../api/spaces-types';
import { teamClient } from '../../api/team-client';
import { useSpace } from '../../state/SpaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useLanguage } from '../../contexts/LanguageContext';
import { ViewTabs } from '../../components/ui/view-tabs';
import { Feedback, useRead } from '../operations/controls';
import { dateOnly, InitialAvatar, RoleBadge, TeamState } from './team-ui';
import { OverviewPanel } from './OverviewPanel';
import { CreditsPanel } from './CreditsPanel';
import { MembersPanel } from './MembersPanel';
import { InvitesPanel } from './InvitesPanel';
import { SettingsPanel } from './SettingsPanel';
import { ActivityPanel } from './ActivityPanel';
import { Button } from '../../components/ui/button';
import { UserPlus } from 'lucide-react';
import { toast } from '../../components/ui/sonner';
import { TeamRevisionContext, useTeamUpdates } from './useTeamUpdates';

type TeamTab = 'overview' | 'members' | 'credits' | 'activity' | 'settings';
type PeopleView = 'members' | 'requests' | 'invites';
export function TeamSettingsPage() {
  const { spaceId = '' } = useParams(),
    space = useSpace(),
    { c } = useCatalogText(),
    { language } = useLanguage();
  const page = useRef<HTMLElement>(null);
  const navigate = useNavigate(),
    contractRoute = useMatch('/team/:spaceId/contract') !== null,
    teamPath = '/team/' + encodeURIComponent(spaceId);
  const [params] = useSearchParams(),
    location = useLocation();
  // 通知中的 ?view=requests 直达加入申请；旧的 ?tab=invites 链接仍落到邀请记录；
  // 积分入口（/credits 与顶栏弹层）以 ?tab=credits 直达积分分区。
  const requestedView: PeopleView | null =
    params.get('view') === 'requests'
      ? 'requests'
      : params.get('tab') === 'invites'
        ? 'invites'
        : null;
  const requestedTab: TeamTab | null = requestedView
    ? 'members'
    : params.get('tab') === 'credits'
      ? 'credits'
      : null;
  const [tab, setTab] = useState<TeamTab>(requestedTab || 'overview'),
    [people, setPeople] = useState<PeopleView>(requestedView || 'members'),
    [requestCount, setRequestCount] = useState<number | null>(null);
  const [visited, setVisited] = useState(
    () => new Set<string>(contractRoute ? ['settings'] : [requestedTab || 'overview']),
  );
  // 已在团队页时再次点击通知或积分入口也要切到目标分区：按每次导航的 location.key 响应。
  useEffect(() => {
    if (!requestedTab) return;
    setVisited((old) => (old.has(requestedTab) ? old : new Set([...old, requestedTab])));
    setTab(requestedTab);
    if (requestedView) setPeople(requestedView);
  }, [location.key]);
  const latest = useRef<{ key: string; value: SpaceDetail } | null>(null);
  const detailKey = JSON.stringify([
    spaceId,
    space.epoch,
    space.current?.role,
    sessionBoundary.epoch,
  ]);
  useEffect(() => {
    let active = true;
    if (space.ready && spaceId && space.spaceId !== spaceId) {
      // 无权访问的团队直达链接只提示一次并回到首页，不停留在重试页。
      void space.switchSpace(spaceId, { keepRoute: true, quiet: true }).then((ok) => {
        if (!active || ok) return;
        toast.error(c('无权限访问该团队，已返回首页。'), { id: 'team-access-denied' });
        navigate('/projects', { replace: true });
      });
    }
    return () => {
      active = false;
    };
  }, [spaceId, space.ready, space.spaceId]);
  const read = useRead(
    (signal) => teamClient.detail(spaceId, signal),
    [spaceId, space.epoch, space.current?.role],
    space.ready && space.spaceId === spaceId,
  );
  if (read.value) latest.current = { key: detailKey, value: read.value };
  const revision = useTeamUpdates(spaceId, space.ready && space.spaceId === spaceId),
    seenRevision = useRef(revision);
  useEffect(() => {
    if (seenRevision.current === revision) return;
    seenRevision.current = revision;
    read.reload();
  }, [revision]);
  // 同一空间重读不销毁未确认命令；真实身份/角色/空间变更仍立即隔离旧控制器。
  const detail = read.value || (latest.current?.key === detailKey ? latest.current.value : null),
    manager = detail?.role === 'owner' || detail?.role === 'admin';
  const [invitation, setInvitation] = useState({ key: '', open: false });
  const invitationOpen = invitation.key === detailKey && invitation.open;
  const selected: TeamTab | 'contract' = manager
    ? contractRoute
      ? 'settings'
      : tab
    : contractRoute
      ? 'contract'
      : 'members';
  function openInvitation(open: boolean) {
    if (open) setVisited((old) => new Set([...old, 'members']));
    setInvitation({ key: detailKey, open });
  }
  function go(target: TeamTab, view?: PeopleView) {
    setVisited((old) => new Set([...old, target]));
    setTab(target);
    if (view) setPeople(view);
    if (contractRoute) navigate(teamPath);
  }
  useEffect(() => {
    if (manager) {
      // Returning through a sidebar/history link must also mount the selected
      // panel, while a direct contract URL does not preload the overview.
      setVisited((old) => (old.has(selected) ? old : new Set([...old, selected])));
    }
  }, [selected, manager, contractRoute]);
  function revealTab(target?: HTMLElement) {
    const navigation =
      target?.closest<HTMLElement>('[role="tablist"]') ||
      page.current?.querySelector<HTMLElement>('[role="tablist"]');
    const active = target || navigation?.querySelector<HTMLElement>('[aria-selected="true"]');
    if (
      !navigation ||
      !page.current?.contains(navigation) ||
      !active ||
      !navigation.contains(active) ||
      navigation.clientWidth <= 0
    )
      return;
    const bounds = navigation.getBoundingClientRect();
    const item = active.getBoundingClientRect();
    const left = bounds.left + navigation.clientLeft;
    const right = left + navigation.clientWidth;
    // 只滚动页签自身；focus({preventScroll:true})与整页滚动位置仍保持。
    const offset =
      item.left < left ? item.left - left : item.right > right ? item.right - right : 0;
    if (offset) navigation.scrollLeft = Math.max(0, navigation.scrollLeft + offset);
  }
  useLayoutEffect(() => {
    if (!manager) return;
    const navigations = [
      ...(page.current?.querySelectorAll<HTMLElement>('[role="tablist"]') || []),
    ];
    const reveal = () => {
      for (const navigation of navigations) {
        const active = navigation.querySelector<HTMLElement>('[aria-selected="true"]');
        if (active) revealTab(active);
      }
    };
    reveal();
    const observer = new ResizeObserver(reveal);
    for (const navigation of navigations) observer.observe(navigation);
    window.addEventListener('resize', reveal);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', reveal);
    };
  }, [selected, language, manager, spaceId, contractRoute]);

  // 详情随团队实时提示重读，人数等事实优先取它，空间列表只作首屏兜底。
  const facts = detail || (space.current?.id === spaceId ? space.current : null);
  const refresh = () => {
    read.refresh();
    void space.refresh();
  };
  if (space.spaceId !== spaceId)
    return (
      <section className="op-page team-page team-workspace-page">
        <Feedback busy error="" retry={() => {}} />
      </section>
    );
  return (
    <TeamRevisionContext.Provider value={revision}>
      <section
        ref={page}
        className="op-page team-page team-workspace-page"
        onFocusCapture={(event) => {
          const target = event.target;
          if (target instanceof HTMLElement && target.getAttribute('role') === 'tab')
            revealTab(target);
        }}
      >
        <header className="team-hero">
          <InitialAvatar name={detail?.name || ''} kind="team" />
          <div className="team-hero-copy">
            <div className="team-hero-title">
              <h1>{detail?.name || c('团队')}</h1>
              {detail && <RoleBadge role={detail.role} />}
            </div>
            {facts && (
              <p className="team-hero-meta">
                <span>
                  {c('{count} / {limit} 人', { count: facts.memberCount, limit: facts.memberLimit })}
                </span>
                {manager && facts.expiresAt && (
                  <span>{c('有效期至 {date}', { date: dateOnly(facts.expiresAt) })}</span>
                )}
              </p>
            )}
          </div>
        </header>
        {manager && facts && (
          <div className="team-notices">
            <TeamState team={facts} />
          </div>
        )}
        <Feedback busy={read.busy} error={read.error} retry={refresh} />
        {detail?.kind === 'team' && (
          <>
            {manager && (
              <div className="team-workflow-navigation">
                <ViewTabs
                  label={c('团队管理')}
                  prefix="team-tab-"
                  panelId="team-panel"
                  value={selected}
                  onChange={(value) => go(value as TeamTab)}
                  items={[
                    { value: 'overview', label: c('概览') },
                    { value: 'members', label: c('成员') },
                    { value: 'credits', label: c('积分') },
                    { value: 'activity', label: c('动态') },
                    { value: 'settings', label: c('设置') },
                  ]}
                />
                {/* 邀请是团队页的常用动作，与分区导航同排，任何分区都能直接打开。 */}
                <div className="team-hero-actions">
                  <Button size="sm" onClick={() => openInvitation(true)}>
                    <UserPlus aria-hidden="true" />
                    {c('邀请成员')}
                  </Button>
                </div>
              </div>
            )}
            <div
              id="team-panel"
              key={detailKey}
              className={manager ? undefined : 'team-member-panel'}
              role={manager ? 'tabpanel' : undefined}
              aria-labelledby={manager ? 'team-tab-' + selected : undefined}
            >
              {contractRoute && !manager && (
                <div className="team-card">
                  <p role="alert" className="op-inline-error">
                    {c('无权限访问')}
                  </p>
                  <Button variant="outline" onClick={() => navigate(teamPath)}>
                    {c('返回成员列表')}
                  </Button>
                </div>
              )}
              {manager && detail.team && visited.has('overview') && (
                <div hidden={selected !== 'overview'}>
                  <OverviewPanel
                    key={spaceId}
                    id={spaceId}
                    team={detail.team}
                    role={detail.role}
                    pendingRequests={requestCount}
                    onRequestCount={setRequestCount}
                    onOpen={(target) =>
                      target === 'plan'
                        ? navigate(teamPath + '/contract')
                        : target === 'requests'
                          ? go('members', 'requests')
                          : go(target)
                    }
                  />
                </div>
              )}
              {(!manager || visited.has('members')) && (
                <div hidden={selected !== 'members'} className="team-people">
                  {manager && (
                    <ViewTabs
                      label={c('成员管理')}
                      prefix="team-people-"
                      panelId="team-people-panel"
                      variant="pill"
                      value={people}
                      onChange={(value) => setPeople(value as PeopleView)}
                      items={[
                        { value: 'members', label: c('全部成员') },
                        {
                          value: 'requests',
                          label: requestCount
                            ? c('加入申请 {count}', { count: requestCount })
                            : c('加入申请'),
                        },
                        { value: 'invites', label: c('邀请记录') },
                      ]}
                    />
                  )}
                  <div
                    id="team-people-panel"
                    role={manager ? 'tabpanel' : undefined}
                    aria-labelledby={manager ? 'team-people-' + people : undefined}
                  >
                    <div hidden={manager && people !== 'members'}>
                      <MembersPanel
                        key={spaceId + detail.role}
                        id={spaceId}
                        role={detail.role}
                        onChanged={refresh}
                      />
                    </div>
                    {manager && (
                      <InvitesPanel
                        key={spaceId}
                        id={spaceId}
                        view={people === 'members' ? null : people}
                        blocked={
                          !facts ||
                          facts.status !== 'active' ||
                          facts.memberCount >= facts.memberLimit
                        }
                        onChanged={refresh}
                        onRequestCount={setRequestCount}
                        open={invitationOpen}
                        onOpenChange={openInvitation}
                      />
                    )}
                  </div>
                </div>
              )}
              {manager && visited.has('credits') && (
                <div hidden={selected !== 'credits'}>
                  <CreditsPanel id={spaceId} role={detail.role} team={detail.team} />
                </div>
              )}
              {manager && visited.has('activity') && (
                <div hidden={selected !== 'activity'}>
                  <ActivityPanel id={spaceId} />
                </div>
              )}
              {manager && detail.team && visited.has('settings') && (
                <div hidden={selected !== 'settings'}>
                  <SettingsPanel
                    key={spaceId + detail.role}
                    team={detail.team}
                    role={detail.role}
                    onSaved={refresh}
                    contract={contractRoute}
                  />
                </div>
              )}
            </div>
          </>
        )}
      </section>
    </TeamRevisionContext.Provider>
  );
}
