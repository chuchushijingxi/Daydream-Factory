import { useEffect, useRef, useState } from 'react';
import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import type { LucideIcon } from 'lucide-react';
import {
  Users,
  Coins,
  Settings2,
  PanelLeft,
  Bell,
  Palette,
  ChevronDown,
} from 'lucide-react';
import { Button } from '../../components/ui/button';
import { ModelConfigurationPage } from '../model-config/ModelConfigurationPage';
import { UsersPage } from './UsersPage';
import { OperationsPaymentsPage } from './OperationsPaymentsPage';
import { OperationsPaymentPackagesPage } from './OperationsPaymentPackagesPage';
import { GrantsPage } from './GrantsPage';
import { OperationsCreditsPage } from '../credits/OperationsCreditsPage';
import { UsagePage } from './UsagePage';
import { OperationsAccountMenu } from './OperationsAccountMenu';
import { MailSettingsPage } from './MailSettingsPage';
import { CreditPolicyPage } from '../credits/CreditPolicyPage';
import { NotificationsPage } from './NotificationsPage';
import { PlatformSettingsPage } from './PlatformSettingsPage';
import { PlatformLogo } from '../../components/workspace/PlatformLogo';
import { usePlatform } from '../../state/PlatformContext';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { TeamsPage } from './teams/TeamsPage';
import { TeamDetailPage } from './teams/TeamDetailPage';
type NavLeaf = {
  path: string;
  label: string;
  match?: (pathname: string) => boolean;
};
type NavEntry =
  | ({ kind: 'group' } & NavLeaf & { Icon: LucideIcon; links: NavLeaf[] })
  | ({ kind: 'link' } & NavLeaf & { Icon: LucideIcon });
const paymentsOrders: NavLeaf['match'] = (pathname) =>
  ['/operations/payments/orders', '/operations/payments/anomalies'].includes(pathname);
const navEntries = (c: CatalogText): NavEntry[] => [
  {
    kind: 'group',
    path: 'people',
    label: c('用户与组织'),
    Icon: Users,
    links: [
      { path: 'users', label: c('用户管理') },
      { path: 'teams', label: c('团队管理'), match: (p) => p.startsWith('/operations/teams') },
      { path: 'grants', label: c('注册授权码') },
    ],
  },
  {
    kind: 'group',
    path: 'billing',
    label: c('积分与支付'),
    Icon: Coins,
    links: [
      { path: 'credits', label: c('积分管理') },
      { path: 'credit-policy', label: c('积分赠送设置') },
      { path: 'payments/orders', label: c('支付管理'), match: paymentsOrders },
      { path: 'payments/packages', label: c('充值档位与限额') },
    ],
  },
  {
    kind: 'group',
    path: 'modeling',
    label: c('模型'),
    Icon: Settings2,
    links: [
      { path: 'models', label: c('模型配置') },
      { path: 'usage', label: c('模型使用记录') },
    ],
  },
  {
    kind: 'group',
    path: 'messaging',
    label: c('消息'),
    Icon: Bell,
    links: [
      { path: 'notifications', label: c('站内信') },
      { path: 'mail', label: c('邮件设置') },
    ],
  },
  { kind: 'link', path: 'platform', label: c('平台设置'), Icon: Palette },
];
const leafActive = (leaf: NavLeaf, pathname: string) =>
  leaf.match ? leaf.match(pathname) : pathname === '/operations/' + leaf.path;
export function OperationsLayout() {
  const { settings: brand } = usePlatform();
  const { c } = useCatalogText();
  const nav = navEntries(c);
  const { pathname } = useLocation(),
    [collapsed, setCollapsed] = useState(false);
  const activeGroupPath = nav.find(
    (entry) => entry.kind === 'group' && entry.links.some((leaf) => leafActive(leaf, pathname)),
  )?.path;
  const [openGroup, setOpenGroup] = useState<string | null>(activeGroupPath ?? null);
  useEffect(() => {
    if (activeGroupPath) setOpenGroup(activeGroupPath);
  }, [activeGroupPath]);
  const toggle = useRef<HTMLButtonElement>(null);
  const toggleGroup = (path: string) => setOpenGroup((prev) => (prev === path ? null : path));
  return (
    <div
      className="op-shell"
      onKeyDown={(event) => {
        if (
          event.key === 'Escape' &&
          !event.defaultPrevented &&
          !collapsed &&
          matchMedia('(max-width:800px)').matches
        ) {
          setCollapsed(true);
          toggle.current?.focus({ preventScroll: true });
        }
      }}
    >
      <aside className="jm-sidebar op-sidebar" data-collapsed={collapsed}>
        <header className="jm-sidebar-head">
          {!collapsed ? (
            <Link className="account-brand" to="/operations/users">
              <PlatformLogo />
              <span className="account-brand-name" title={brand?.name}>
                {brand?.name}
              </span>
            </Link>
          ) : null}
          <Button
            ref={toggle}
            size="icon"
            variant="ghost"
            aria-label={collapsed ? c('展开运营导航') : c('收起运营导航')}
            aria-expanded={!collapsed}
            onClick={() => setCollapsed((v) => !v)}
          >
            <PanelLeft />
          </Button>
        </header>
        {!collapsed ? <p className="op-sidebar-caption">{c('运营后台')}</p> : null}
        <nav aria-label={c('运营导航')} className="jm-sidebar-links">
          {nav.map((entry) => {
            const active = leafActive(entry, pathname);
            if (entry.kind === 'link')
              return (
                <Link
                  key={entry.path}
                  className="jm-sidebar-nav"
                  onClick={() => {
                    if (matchMedia('(max-width:800px)').matches) setCollapsed(true);
                  }}
                  to={'/operations/' + entry.path}
                  aria-label={entry.label}
                  title={collapsed ? entry.label : undefined}
                  aria-current={active ? 'page' : undefined}
                >
                  <entry.Icon />
                  {!collapsed ? <span>{entry.label}</span> : null}
                </Link>
              );
            const open = !collapsed && openGroup === entry.path;
            return (
              <div
                key={entry.path}
                className="op-nav-group"
                data-open={open}
                data-current={entry.links.some((leaf) => leafActive(leaf, pathname))}
              >
                <button
                  type="button"
                  className="jm-sidebar-nav op-nav-toggle"
                  aria-label={entry.label}
                  aria-expanded={collapsed ? undefined : open}
                  title={collapsed ? entry.label : undefined}
                  onClick={() => {
                    if (collapsed) {
                      setCollapsed(false);
                      setOpenGroup(entry.path);
                      return;
                    }
                    toggleGroup(entry.path);
                  }}
                >
                  <entry.Icon />
                  {!collapsed ? <span>{entry.label}</span> : null}
                  {!collapsed ? <ChevronDown className="op-nav-caret" /> : null}
                </button>
                <div className="op-nav-sub-wrap">
                  <div className="op-nav-sub">
                    {entry.links.map((leaf) => (
                      <Link
                        key={leaf.path}
                        className="jm-sidebar-nav"
                        onClick={() => {
                          if (matchMedia('(max-width:800px)').matches) setCollapsed(true);
                        }}
                        to={'/operations/' + leaf.path}
                        aria-label={leaf.label}
                        aria-current={leafActive(leaf, pathname) ? 'page' : undefined}
                      >
                        <span>{leaf.label}</span>
                      </Link>
                    ))}
                  </div>
                </div>
              </div>
            );
          })}
        </nav>
        <footer className="op-sidebar-footer">
          <OperationsAccountMenu compact={collapsed} />
        </footer>
      </aside>
      {!collapsed ? (
        <button
          className="op-nav-backdrop"
          aria-label={c('收起运营导航遮罩')}
          onClick={() => {
            setCollapsed(true);
            toggle.current?.focus({ preventScroll: true });
          }}
        />
      ) : null}
      <main className="op-main">
        <Routes>
          <Route path="/operations/teams" element={<TeamsPage />} />
          <Route path="/operations/teams/:id" element={<TeamDetailPage />} />
          <Route path="/operations/users" element={<UsersPage />} />
          <Route path="/operations/payments/orders" element={<OperationsPaymentsPage />} />
          <Route path="/operations/payments/anomalies" element={<OperationsPaymentsPage />} />
          <Route path="/operations/payments/packages" element={<OperationsPaymentPackagesPage />} />
          <Route path="/operations/grants" element={<GrantsPage />} />
          <Route path="/operations/credits" element={<OperationsCreditsPage />} />
          <Route path="/operations/models" element={<ModelConfigurationPage />} />
          <Route path="/operations/usage" element={<UsagePage />} />
          <Route path="/operations/mail" element={<MailSettingsPage />} />
          <Route path="/operations/platform" element={<PlatformSettingsPage />} />
          <Route path="/operations/credit-policy" element={<CreditPolicyPage />} />
          <Route path="/operations/notifications" element={<NotificationsPage />} />
          <Route path="*" element={<Navigate to="/operations/users" replace />} />
        </Routes>
      </main>
    </div>
  );
}
