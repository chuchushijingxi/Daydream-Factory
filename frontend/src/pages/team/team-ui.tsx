import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Button } from '../../components/ui/button';
import { Ellipsis } from 'lucide-react';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '../../components/ui/dropdown-menu';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '../../components/ui/dialog';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { sessionBoundary } from '../../api/session-boundary';
import { writeResultUnknown } from '../../api/errors';
import type { SpaceRole, SpaceSummary, TeamView } from '../../api/spaces-types';
import { StatusBadge } from '../operations/controls';
import { dateTime } from '../credits/credit-format';

export function roleText(role: SpaceRole, c: CatalogText) {
  return role === 'owner' ? c('所有者') : role === 'admin' ? c('管理员') : c('成员');
}
export function RoleBadge({ role }: { role: SpaceRole }) {
  const { c } = useCatalogText();
  return (
    <span className="team-role-badge" data-role={role}>
      {roleText(role, c)}
    </span>
  );
}

// 头像只取名称首字，作为成员与团队的视觉锚点；不伪造上传头像。
export function InitialAvatar({ name, kind = 'member' }: { name: string; kind?: 'member' | 'team' }) {
  const initial = Array.from(name.trim())[0]?.toUpperCase() || '·';
  return (
    <span className="team-avatar" data-kind={kind} aria-hidden="true">
      {initial}
    </span>
  );
}
const shanghaiDate = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
});
// 摘要处只显示日期；流水与审计等需要精确时刻的位置仍使用 dateTime。
export function dateOnly(value?: string | null) {
  return value ? shanghaiDate.format(new Date(value)) : '—';
}
// 名额、额度类进度按占用比例着色：<70% 常规，70%–<90% 提醒，≥90% 告急（含已满）。
export type UsageTone = 'normal' | 'warning' | 'danger';
export function usageTone(used: number, limit: number): UsageTone {
  const ratio = limit > 0 ? used / limit : 1;
  return ratio >= 0.9 ? 'danger' : ratio >= 0.7 ? 'warning' : 'normal';
}
export function daysUntil(value?: string | null, now = Date.now()) {
  if (!value) return null;
  return Math.ceil((new Date(value).getTime() - now) / 86400000);
}

export function MemberActionMenu({
  name,
  disabled,
  items,
}: {
  name: string;
  disabled: boolean;
  items: { label: string; run: () => void; destructive?: boolean }[];
}) {
  const { c } = useCatalogText();
  const trigger = useRef<HTMLButtonElement>(null);
  const pending = useRef<(() => void) | null>(null);
  if (!items.length) return <span className="op-muted">—</span>;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          ref={trigger}
          variant="ghost"
          size="icon"
          className="team-member-more"
          aria-label={c('管理成员：{name}', { name })}
          disabled={disabled}
        >
          <Ellipsis aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="team-member-menu"
        align="end"
        sideOffset={6}
        onCloseAutoFocus={(event) => {
          if (!pending.current) return;
          event.preventDefault();
          const run = pending.current;
          pending.current = null;
          // 先回到稳定触发器再打开确认窗，关闭后才能可靠恢复焦点。
          trigger.current?.focus({ preventScroll: true });
          run();
        }}
      >
        {items.map((item, index) => (
          <span key={item.label} className="team-menu-group">
            {item.destructive && index > 0 && <DropdownMenuSeparator />}
            <DropdownMenuItem
              variant={item.destructive ? 'destructive' : 'default'}
              onSelect={() => {
                pending.current = item.run;
              }}
            >
              {item.label}
            </DropdownMenuItem>
          </span>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
export const numberText = (value: number) => new Intl.NumberFormat().format(value);
export function numberInput(value: string, minimum = 0) {
  const n = Number(value);
  if (!/^(0|[1-9]\d*)$/.test(value) || !Number.isSafeInteger(n) || n < minimum)
    throw new Error('INVALID_NUMBER');
  return n;
}
export function useTeamMutation() {
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [notice, setNotice] = useState(''),
    [needsRead, setNeedsRead] = useState(false);
  const guard = useRef(false),
    active = useRef(true);
  const { c } = useCatalogText();
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  async function execute<T>(
    action: () => Promise<T>,
    done?: (value: T) => void | Promise<void>,
    reading = false,
  ) {
    if (guard.current || (needsRead && !reading)) return false;
    guard.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    const epoch = sessionBoundary.epoch;
    try {
      const value = await action();
      sessionBoundary.assert(epoch);
      if (!active.current) return false;
      await done?.(value);
      if (active.current) {
        if (reading) setNeedsRead(false);
        else setNotice(c('操作已完成'));
      }
      return true;
    } catch (e) {
      if (active.current && epoch === sessionBoundary.epoch) {
        const unknown =
          writeResultUnknown(e) || (e as { code?: string }).code === 'SPACE_COMMIT_UNKNOWN';
        if (!reading) setNeedsRead(unknown || (e as { status?: number }).status === 409);
        setError(
          (e as Error).message === 'INVALID_NUMBER'
            ? c('请输入有效的安全整数。')
            : (e as Error).message || c('请求失败，请稍后重试。'),
        );
      }
      return false;
    } finally {
      guard.current = false;
      if (active.current && epoch === sessionBoundary.epoch) setBusy(false);
    }
  }
  return {
    busy,
    error,
    notice,
    needsRead,
    run: <T,>(action: () => Promise<T>, done?: (value: T) => void | Promise<void>) =>
      execute(action, done),
    readback: <T,>(action: () => Promise<T>, done?: (value: T) => void | Promise<void>) =>
      execute(action, done, true),
    reconciled: () => {
      setNeedsRead(false);
      setError('');
      setNotice('');
    },
  };
}
export type TeamMutation = ReturnType<typeof useTeamMutation>;
export function MutationFeedback({ mutation }: { mutation: TeamMutation }) {
  const { c } = useCatalogText();
  return (
    <>
      {mutation.error && (
        <p className="op-inline-error" role="alert">
          {mutation.error}
        </p>
      )}
      {mutation.needsRead && (
        <p className="team-callout" role="status">
          {c('提交结果或版本未确认，请先读取最新状态核对，不要重复提交。')}
        </p>
      )}
      {mutation.notice && (
        <p className="team-success" role="status">
          {mutation.notice}
        </p>
      )}
    </>
  );
}
export function ConfirmAction({
  open,
  onOpenChange,
  title,
  description,
  confirm,
  busy,
  confirmDisabled = false,
  children,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  title: string;
  description: string;
  confirm: () => void;
  busy: boolean;
  confirmDisabled?: boolean;
  children?: ReactNode;
}) {
  const { c } = useCatalogText();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent variant="confirm" closeDisabled={busy}>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
        {children}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
            {c('取消')}
          </Button>
          <Button variant="destructive" busy={busy} disabled={confirmDisabled} onClick={confirm}>
            {c('确认')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
export function TeamState({ team }: { team: SpaceSummary | TeamView }) {
  const { c } = useCatalogText();
  const days = daysUntil(team.expiresAt);
  const expiresSoon = days !== null && days > 0 && days <= 7;
  return (
    <>
      {team.status !== 'active' && (
        <StatusBadge tone="warning">
          {team.status === 'expired' ? c('已到期') : c('已停用')}
        </StatusBadge>
      )}
      {team.memberCount > team.memberLimit && (
        <p className="team-callout" role="status">
          {c('当前人数超过上限，已有成员不受影响，暂不能邀请或批准加入。')}
        </p>
      )}
      {expiresSoon && (
        <p className="team-callout" role="status">
          {c('团队将在 7 天内到期：{time}', { time: dateTime(team.expiresAt || null) })}
        </p>
      )}
      {team.status === 'expired' && (
        <p className="team-callout">{c('团队已到期，已发积分仍可使用，暂不能邀请新成员。')}</p>
      )}
      {'readOnly' in team && team.readOnly && (
        <p className="team-callout">{c('团队积分已用完，当前内容只读。')}</p>
      )}
    </>
  );
}
