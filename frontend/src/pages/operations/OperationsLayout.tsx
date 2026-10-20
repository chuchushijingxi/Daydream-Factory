import { useState, useRef } from 'react';
import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import {
  Users,
  KeyRound,
  Coins,
  Settings2,
  ScrollText,
  PanelLeft,
  Mail,
  Gift,
  Bell,
  Palette,
} from 'lucide-react';
import { Button } from '../../components/ui/button';
import { ModelConfigurationPage } from '../model-config/ModelConfigurationPage';
import { UsersPage } from './UsersPage';
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
const items = (c: CatalogText) =>
  [
    { path: 'users', label: c('用户管理'), Icon: Users },
    { path: 'grants', label: c('注册授权码'), Icon: KeyRound },
    { path: 'credits', label: c('积分管理'), Icon: Coins },
    { path: 'credit-policy', label: c('积分赠送设置'), Icon: Gift },
    { path: 'notifications', label: c('站内信'), Icon: Bell },
    { path: 'models', label: c('模型配置'), Icon: Settings2 },
    { path: 'usage', label: c('模型使用记录'), Icon: ScrollText },
    { path: 'mail', label: c('邮件设置'), Icon: Mail },
    { path: 'platform', label: c('平台设置'), Icon: Palette },
  ] as const;
export function OperationsLayout() {
  const { settings: brand } = usePlatform();
  const { c } = useCatalogText();
  const nav = items(c);
  const { pathname } = useLocation(),
    [collapsed, setCollapsed] = useState(false);
  const toggle = useRef<HTMLButtonElement>(null);
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
              <PlatformLogo /><span className="account-brand-name" title={brand?.name}>{brand?.name}</span>
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
          {nav.map(({ path, label, Icon }) => (
            <Link
              key={path}
              className="jm-sidebar-nav"
              onClick={() => {
                if (matchMedia('(max-width:800px)').matches) setCollapsed(true);
              }}
              to={'/operations/' + path}
              aria-label={label}
              title={collapsed ? label : undefined}
              aria-current={pathname === '/operations/' + path ? 'page' : undefined}
            >
              <Icon />
              {!collapsed ? <span>{label}</span> : null}
            </Link>
          ))}
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
          <Route path="/operations/users" element={<UsersPage />} />
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
