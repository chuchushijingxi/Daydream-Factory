import * as Menu from '@radix-ui/react-dropdown-menu';
import {
  ChevronUp,
  ChevronRight,
  ChevronLeft,
  Check,
  Users,
  UserPlus,
  Mail,
  Languages,
  SunMoon,
  Sun,
  Moon,
  UserRound,
  LogOut,
  Ellipsis,
  ReceiptText,
  FileText,
  ShieldCheck,
} from 'lucide-react';
import { useLanguage } from '../../contexts/LanguageContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useContext, useEffect, useRef, useState } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useWorkspace } from '../../state/WorkspaceContext';
import { ProfileDialog } from '../../pages/accounts/ProfileDialog';
import { AccountAvatar } from '../../pages/accounts/AccountAvatar';
import { useOptionalSpace } from '../../state/SpaceContext';
import { useHref, useNavigate } from 'react-router-dom';
import { legalPaths } from '../../pages/legal/LegalDocumentPage';
import { JoinTeamDialog } from '../../pages/team/JoinTeamDialog';
export function SidebarAccountMenu({
  compact = false,
  label,
  showLanguage = false,
}: {
  compact?: boolean;
  label: string;
  showLanguage?: boolean;
}) {
  const { c } = useCatalogText(),
    { language, setLanguage } = useLanguage(),
    { theme, toggleTheme, status } = useWorkspace();
  const session = useContext(SessionOptionalContext);
  const space = useOptionalSpace();
  const navigate = useNavigate(),
    termsHref = useHref(legalPaths.terms),
    privacyHref = useHref(legalPaths.privacy);
  const hasTeams = space?.spaces.some((item) => item.kind === 'team') === true;
  const trigger = useRef<HTMLButtonElement>(null),
    profileOpening = useRef(false),
    content = useRef<HTMLDivElement>(null),
    spaceTrigger = useRef<HTMLDivElement>(null),
    returningToSpace = useRef(false);
  const [profileOpen, setProfileOpen] = useState(false),
    [menuOpen, setMenuOpen] = useState(false),
    [joinOpen, setJoinOpen] = useState(false),
    [submenuOpen, setSubmenuOpen] = useState(false),
    [spaceListOpen, setSpaceListOpen] = useState(false),
    [moreOpen, setMoreOpen] = useState(false),
    [moreListOpen, setMoreListOpen] = useState(false),
    [narrow, setNarrow] = useState(
      () => typeof matchMedia === 'function' && matchMedia('(max-width: 640px)').matches,
    );
  useEffect(() => {
    if (typeof matchMedia !== 'function') return;
    const media = matchMedia('(max-width: 640px)');
    const resized = () => {
      setNarrow(media.matches);
      // A change between stacked and flyout navigation closes the old layer,
      // rather than leaving its focused item in a different menu hierarchy.
      setMenuOpen(false);
      setSubmenuOpen(false);
      setMoreOpen(false);
    };
    media.addEventListener('change', resized);
    return () => media.removeEventListener('change', resized);
  }, []);
  useEffect(() => {
    if (!menuOpen || !narrow) return;
    const focus = spaceListOpen
      ? content.current?.querySelector<HTMLElement>('[data-space-option][data-current="true"]') ||
        content.current?.querySelector<HTMLElement>('[data-space-option]')
      : returningToSpace.current
        ? spaceTrigger.current
        : null;
    returningToSpace.current = spaceListOpen;
    if (!focus) return;
    const frame = requestAnimationFrame(() => focus.focus({ preventScroll: true }));
    return () => cancelAnimationFrame(frame);
  }, [menuOpen, narrow, spaceListOpen]);
  const changeMenuOpen = (open: boolean) => {
    setMenuOpen(open);
    setSubmenuOpen(false);
    setMoreOpen(false);
    if (open) {
      setSpaceListOpen(false);
      setMoreListOpen(false);
      returningToSpace.current = false;
    }
  };
  // Presence keeps the closing panel mounted. Deactivate its actions immediately.
  const stopClosedInput = (event: { preventDefault(): void; stopPropagation(): void }) => {
    if (!menuOpen) {
      event.preventDefault();
      event.stopPropagation();
    }
  };
  const username = session?.user?.username || c('短剧 Agent');
  const stopClosedSubInput = (event: { preventDefault(): void; stopPropagation(): void }) => {
    if (!menuOpen || !submenuOpen) {
      event.preventDefault();
      event.stopPropagation();
    }
  };
  const stopClosedMoreInput = (event: { preventDefault(): void; stopPropagation(): void }) => {
    if (!menuOpen || !moreOpen) {
      event.preventDefault();
      event.stopPropagation();
    }
  };
  // 充值记录只在账号菜单“更多”下提供，收银页与积分浮层不再放记录入口。
  // 协议与政策是独立文档页，始终在新标签页打开，不替换当前工作台。
  const moreChoices = (
    <>
      <Menu.Item
        className="xy-menu-row xy-preferences-row"
        onSelect={() => navigate('/payments/orders')}
      >
        <ReceiptText aria-hidden />
        <span>{c('充值记录')}</span>
      </Menu.Item>
      <Menu.Item className="xy-menu-row xy-preferences-row" asChild>
        <a href={termsHref} target="_blank" rel="noopener noreferrer">
          <FileText aria-hidden />
          <span>{c('用户服务协议')}</span>
        </a>
      </Menu.Item>
      <Menu.Item className="xy-menu-row xy-preferences-row" asChild>
        <a href={privacyHref} target="_blank" rel="noopener noreferrer">
          <ShieldCheck aria-hidden />
          <span>{c('隐私政策')}</span>
        </a>
      </Menu.Item>
    </>
  );
  const spaceChoices = space ? (
    <>
      <Menu.RadioGroup value={space.spaceId} onValueChange={(id) => void space.switchSpace(id)}>
        {space.spaces.map((item, index) => (
          <span key={item.id} className="space-menu-entry">
            {index > 0 && space.spaces[index - 1].kind !== item.kind ? (
              <Menu.Separator className="xy-preferences-separator" />
            ) : null}
            <span className="space-menu-line" data-kind={item.kind}>
              <Menu.RadioItem
                value={item.id}
                textValue={item.kind === 'personal' ? c('个人空间') : item.name}
                data-space-option
                data-current={item.id === space.spaceId}
                className="xy-menu-row xy-preferences-row space-menu-row"
              >
                <span className="space-menu-check" aria-hidden>
                  {item.id === space.spaceId ? <Check /> : null}
                </span>
                <span className="space-menu-copy">
                  <span className="space-menu-title">
                    <span className="space-menu-name">
                      {item.kind === 'personal' ? c('个人空间') : item.name}
                    </span>
                    {item.kind === 'team' ? (
                      // 与团队页同一枚角色标签，紧跟团队名称。
                      <span className="team-role-badge space-role-badge" data-role={item.role}>
                        {c(
                          item.role === 'owner'
                            ? '所有者'
                            : item.role === 'admin'
                              ? '管理员'
                              : '成员',
                        )}
                      </span>
                    ) : null}
                  </span>
                  {item.kind === 'team' ? (
                    <span className="space-menu-detail">
                      <span>{c('{count}人', { count: item.memberCount })}</span>
                      {item.status === 'expired' ? (
                        <span className="space-expired-label">{c('已到期')}</span>
                      ) : null}
                    </span>
                  ) : null}
                </span>
              </Menu.RadioItem>
              {item.kind === 'team' ? (
                // 团队行直接进入团队页；目标页自行核验并切换空间，菜单不另做第二套切换。
                <Menu.Item
                  className="space-menu-enter"
                  aria-label={c('进入团队：{name}', { name: item.name })}
                  onSelect={() => navigate('/team/' + encodeURIComponent(item.id))}
                >
                  <span>{c('进入')}</span>
                  <ChevronRight aria-hidden />
                </Menu.Item>
              ) : null}
            </span>
          </span>
        ))}
      </Menu.RadioGroup>
      <Menu.Separator className="xy-preferences-separator" />
      <Menu.Item className="xy-menu-row xy-preferences-row" onSelect={() => navigate('/invites')}>
        <Mail aria-hidden />
        <span>{c('待处理邀请（{count}）', { count: space.pendingInvites })}</span>
      </Menu.Item>
      <Menu.Item className="xy-menu-row xy-preferences-row" onSelect={() => setJoinOpen(true)}>
        <UserPlus aria-hidden />
        <span>{c('加入团队')}</span>
      </Menu.Item>
    </>
  ) : null;
  return (
    <div className="xy-sidebar-preferences">
      <Menu.Root modal={false} open={menuOpen} onOpenChange={changeMenuOpen}>
        <Menu.Trigger asChild>
          <button
            ref={trigger}
            type="button"
            className="xy-sidebar-menu-trigger"
            data-compact={compact}
            aria-label={label}
          >
            <AccountAvatar user={session?.user} />
            {!compact ? (
              <>
                <span className="space-account-copy">
                  <span className="xy-sidebar-menu-name" title={username}>
                    {username}
                  </span>
                  {space?.current ? (
                    <small className="space-account-subtitle">
                      {space.current.kind === 'personal' ? c('个人空间') : space.current.name}
                    </small>
                  ) : null}
                </span>
              </>
            ) : null}
          </button>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Content
            ref={content}
            data-space-panel={spaceListOpen || moreListOpen}
            side="top"
            align="start"
            sideOffset={6}
            collisionPadding={12}
            className="xy-dropdown xy-preferences-menu"
            onPointerDownCapture={stopClosedInput}
            onClickCapture={stopClosedInput}
            onKeyDownCapture={(event) => {
              stopClosedInput(event);
              if (menuOpen && (spaceListOpen || moreListOpen) && event.key === 'ArrowLeft') {
                event.preventDefault();
                event.stopPropagation();
                setSpaceListOpen(false);
                setMoreListOpen(false);
              }
            }}
            onEscapeKeyDown={(event) => {
              event.stopPropagation();
              if (spaceListOpen || moreListOpen) {
                event.preventDefault();
                setSpaceListOpen(false);
                setMoreListOpen(false);
              }
            }}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (!profileOpening.current) trigger.current?.focus({ preventScroll: true });
            }}
          >
            {spaceListOpen ? (
              <>
                <Menu.Item
                  className="xy-menu-row xy-preferences-row space-menu-back"
                  aria-label={c('返回账号菜单')}
                  onSelect={(event) => {
                    event.preventDefault();
                    setSpaceListOpen(false);
                  }}
                >
                  <ChevronLeft aria-hidden />
                  <span>{c('当前空间')}</span>
                </Menu.Item>
                <Menu.Separator className="xy-preferences-separator" />
                {spaceChoices}
              </>
            ) : moreListOpen ? (
              <>
                <Menu.Item
                  className="xy-menu-row xy-preferences-row space-menu-back"
                  aria-label={c('返回账号菜单')}
                  onSelect={(event) => {
                    event.preventDefault();
                    setMoreListOpen(false);
                  }}
                >
                  <ChevronLeft aria-hidden />
                  <span>{c('更多')}</span>
                </Menu.Item>
                <Menu.Separator className="xy-preferences-separator" />
                {moreChoices}
              </>
            ) : (
              <>
                <div className="xy-preferences-identity">
                  <AccountAvatar user={session?.user} />
                  <div>
                    <strong>{username}</strong>
                    {session?.user && session.user.email !== username ? (
                      <p>{session.user.email}</p>
                    ) : null}
                    {status?.readOnly ? <p>{c('只读浏览')}</p> : null}
                  </div>
                </div>
                {space && hasTeams ? (
                  narrow ? (
                    <Menu.Item
                      ref={spaceTrigger}
                      className="xy-menu-row xy-preferences-row space-menu-trigger"
                      onSelect={(event) => {
                        event.preventDefault();
                        setSpaceListOpen(true);
                      }}
                    >
                      <Users aria-hidden />
                      <span>{c('当前空间')}</span>
                      <span className="space-menu-current">
                        {space.current?.kind === 'personal' ? c('个人空间') : space.current?.name}
                      </span>
                      <ChevronRight aria-hidden />
                    </Menu.Item>
                  ) : (
                    <Menu.Sub open={submenuOpen} onOpenChange={setSubmenuOpen}>
                      <Menu.SubTrigger
                        ref={spaceTrigger}
                        className="xy-menu-row xy-preferences-row space-menu-trigger"
                      >
                        <Users aria-hidden />
                        <span>{c('当前空间')}</span>
                        <span className="space-menu-current">
                          {space.current?.kind === 'personal' ? c('个人空间') : space.current?.name}
                        </span>
                        <ChevronRight aria-hidden />
                      </Menu.SubTrigger>
                      <Menu.Portal>
                        <Menu.SubContent
                          className="xy-dropdown xy-preferences-menu space-submenu"
                          sideOffset={16}
                          collisionPadding={12}
                          onPointerDownCapture={stopClosedSubInput}
                          onClickCapture={stopClosedSubInput}
                          onKeyDownCapture={stopClosedSubInput}
                          onEscapeKeyDown={(event) => {
                            event.preventDefault();
                            event.stopPropagation();
                            setSubmenuOpen(false);
                            spaceTrigger.current?.focus({ preventScroll: true });
                          }}
                        >
                          {spaceChoices}
                        </Menu.SubContent>
                      </Menu.Portal>
                    </Menu.Sub>
                  )
                ) : null}
                {space && !hasTeams ? (
                  <Menu.Item
                    className="xy-menu-row xy-preferences-row"
                    onSelect={() => setJoinOpen(true)}
                  >
                    <UserPlus aria-hidden />
                    <span>{c('加入团队')}</span>
                  </Menu.Item>
                ) : null}
                {session?.user ? (
                  <Menu.Item
                    className="xy-menu-row xy-preferences-row"
                    onSelect={() => {
                      profileOpening.current = true;
                      setProfileOpen(true);
                    }}
                  >
                    <UserRound aria-hidden />
                    <span>{c('个人资料')}</span>
                  </Menu.Item>
                ) : null}
                {showLanguage ? (
                  <Menu.CheckboxItem
                    data-preference="language"
                    className="xy-menu-row xy-preferences-row xy-preference-toggle"
                    onSelect={(event) => event.preventDefault()}
                    checked={language === 'en'}
                    onCheckedChange={(checked) => void setLanguage(checked === true ? 'en' : 'zh')}
                    aria-label={c('{label}：{name}', {
                      label: c('语言'),
                      name: language === 'en' ? 'English' : '简体中文',
                    })}
                  >
                    <Languages aria-hidden />
                    <span>{c('语言')}</span>
                    <span className="xy-language-switch" aria-hidden>
                      <span data-active={language !== 'en'}>中</span>
                      <span data-active={language === 'en'}>EN</span>
                    </span>
                  </Menu.CheckboxItem>
                ) : null}
                <Menu.CheckboxItem
                  data-preference="theme"
                  className="xy-menu-row xy-preferences-row xy-preference-toggle"
                  onSelect={(event) => event.preventDefault()}
                  checked={theme === 'dark'}
                  onCheckedChange={(checked) => {
                    if ((checked === true) !== (theme === 'dark')) toggleTheme();
                  }}
                  aria-label={c('{label}：{name}', {
                    label: c('外观'),
                    name: c(theme === 'dark' ? '深色' : '浅色'),
                  })}
                >
                  <SunMoon aria-hidden />
                  <span>{c('外观')}</span>
                  <span className="xy-theme-switch" data-checked={theme === 'dark'} aria-hidden>
                    <span className="xy-theme-switch-thumb">
                      {theme === 'dark' ? <Moon /> : <Sun />}
                    </span>
                  </span>
                </Menu.CheckboxItem>
                {session?.user && space ? (
                  narrow ? (
                    <Menu.Item
                      className="xy-menu-row xy-preferences-row space-menu-trigger"
                      onSelect={(event) => {
                        event.preventDefault();
                        setMoreListOpen(true);
                      }}
                    >
                      <Ellipsis aria-hidden />
                      <span>{c('更多')}</span>
                      <span className="space-menu-current" />
                      <ChevronRight aria-hidden />
                    </Menu.Item>
                  ) : (
                    <Menu.Sub open={moreOpen} onOpenChange={setMoreOpen}>
                      <Menu.SubTrigger className="xy-menu-row xy-preferences-row space-menu-trigger">
                        <Ellipsis aria-hidden />
                        <span>{c('更多')}</span>
                        <span className="space-menu-current" />
                        <ChevronRight aria-hidden />
                      </Menu.SubTrigger>
                      <Menu.Portal>
                        <Menu.SubContent
                          className="xy-dropdown xy-preferences-menu space-submenu"
                          sideOffset={16}
                          collisionPadding={12}
                          onPointerDownCapture={stopClosedMoreInput}
                          onClickCapture={stopClosedMoreInput}
                          onKeyDownCapture={stopClosedMoreInput}
                          onEscapeKeyDown={(event) => {
                            event.preventDefault();
                            event.stopPropagation();
                            setMoreOpen(false);
                          }}
                        >
                          {moreChoices}
                        </Menu.SubContent>
                      </Menu.Portal>
                    </Menu.Sub>
                  )
                ) : null}
                {session?.user ? (
                  <>
                    <Menu.Separator className="xy-preferences-separator" />
                    <Menu.Item
                      className="xy-menu-row xy-preferences-row account-logout-item"
                      onSelect={(event) => {
                        if (!menuOpen) {
                          event.preventDefault();
                          return;
                        }
                        void session.logout();
                      }}
                    >
                      <LogOut aria-hidden />
                      <span>{c('退出登录')}</span>
                    </Menu.Item>
                  </>
                ) : (
                  <Menu.Item className="xy-preferences-close" aria-label={c('收起菜单')}>
                    <ChevronUp aria-hidden />
                  </Menu.Item>
                )}
              </>
            )}
          </Menu.Content>
        </Menu.Portal>
      </Menu.Root>
      {space ? <JoinTeamDialog open={joinOpen} onOpenChange={setJoinOpen} /> : null}
      {session?.user ? (
        <ProfileDialog
          open={profileOpen}
          returnFocus={trigger}
          onOpenChange={(open) => {
            profileOpening.current = open;
            setProfileOpen(open);
          }}
        />
      ) : null}
    </div>
  );
}
