import { useState } from 'react';
import * as Popover from '@radix-ui/react-popover';
import { Check, ChevronDown, Search } from 'lucide-react';
import { useSession } from '../../state/SessionContext';
import { Button } from '../../components/ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Feedback, PageNavigation, useRead } from './controls';

export function NotificationRecipientPicker({
  value,
  name,
  disabled,
  onChange,
}: {
  value: string;
  name: string;
  disabled: boolean;
  onChange: (id: string, name: string) => void;
}) {
  const { service, epoch } = useSession();
  const { c } = useCatalogText();
  const [open, setOpen] = useState(false),
    [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [page, setPage] = useState('');
  const users = useRead(() => service.users(query, page), [service, query, page, epoch], open);
  const searchUsers = () => {
    setQuery(search.trim());
    setPage('');
    users.refresh();
  };
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          className="notification-recipient-trigger"
          aria-label={c('选择接收用户')}
          disabled={disabled}
        >
          <span>{name || c('请选择用户')}</span>
          <ChevronDown size={14} />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className="notification-recipient-menu"
          align="start"
          sideOffset={6}
          collisionPadding={12}
          aria-label={c('选择接收用户')}
        >
          <div className="notification-recipient-search">
            <Search size={14} />
            <input
              aria-label={c('搜索接收人邮箱')}
              placeholder={c('搜索邮箱')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault();
                  searchUsers();
                }
              }}
            />
            <Button size="sm" variant="ghost" onClick={searchUsers}>
              {c('查询')}
            </Button>
          </div>
          <Feedback
            busy={users.busy}
            error={users.error}
            empty={users.value?.items.length === 0}
            retry={users.refresh}
          />
          {users.value ? (
            <>
              <div className="notification-recipient-options" role="group" aria-label={c('接收用户')}>
                {users.value.items.map((account) => (
                  <button
                    type="button"
                    key={account.identity}
                    disabled={!account.enabled}
                    aria-pressed={value === account.identity}
                    onClick={() => {
                      onChange(account.identity, account.username);
                      setOpen(false);
                    }}
                  >
                    <span>
                      <strong>{account.username}</strong>
                      <small>
                        {account.enabled
                          ? account.email
                          : c('{email} · 已禁用', { email: account.email })}
                      </small>
                    </span>
                    {value === account.identity ? <Check size={14} /> : null}
                  </button>
                ))}
              </div>
              <PageNavigation page={users.value} busy={users.busy} onPage={setPage} />
            </>
          ) : null}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
