import * as Menu from '@radix-ui/react-dropdown-menu';
import { ChevronUp, Languages, SunMoon, Sun, Moon, UserRound, LogOut } from 'lucide-react';
import { useLanguage } from '../../contexts/LanguageContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useContext, useRef, useState } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useWorkspace } from '../../state/WorkspaceContext';
import { ProfileDialog } from '../../pages/accounts/ProfileDialog';
import { AccountAvatar } from '../../pages/accounts/AccountAvatar';
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
  const trigger = useRef<HTMLButtonElement>(null),
    profileOpening = useRef(false);
  const [profileOpen, setProfileOpen] = useState(false),
    [menuOpen, setMenuOpen] = useState(false);
  // Presence keeps the closing panel mounted. Deactivate its actions immediately.
  const stopClosedInput = (event: { preventDefault(): void; stopPropagation(): void }) => {
    if (!menuOpen) {
      event.preventDefault();
      event.stopPropagation();
    }
  };
  const username = session?.user?.username || c('短剧 Agent');
  return (
    <div className="xy-sidebar-preferences">
      <Menu.Root modal={false} open={menuOpen} onOpenChange={setMenuOpen}>
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
                <span className="xy-sidebar-menu-name" title={username}>
                  {username}
                </span>
              </>
            ) : null}
          </button>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Content
            side="top"
            align="start"
            sideOffset={6}
            collisionPadding={12}
            className="xy-dropdown xy-preferences-menu"
            onPointerDownCapture={stopClosedInput}
            onClickCapture={stopClosedInput}
            onKeyDownCapture={stopClosedInput}
            onEscapeKeyDown={(event) => event.stopPropagation()}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (!profileOpening.current) trigger.current?.focus({ preventScroll: true });
            }}
          >
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
          </Menu.Content>
        </Menu.Portal>
      </Menu.Root>
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
