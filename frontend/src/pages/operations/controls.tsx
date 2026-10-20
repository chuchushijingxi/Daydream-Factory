import { useEffect, useRef, useState, type ReactNode } from 'react';
import * as Menu from '@radix-ui/react-dropdown-menu';
import { ChevronDown, ChevronLeft, ChevronRight } from 'lucide-react';
import { Button } from '../../components/ui/button';
import { Spinner } from '../../components/ui/spinner';
import { sessionBoundary } from '../../api/session-boundary';
import { useCatalogText } from '../../contexts/useCatalogText';
import type { FilterOption, PageView } from '../../api/administration';

export function StatusBadge({
  tone = 'neutral',
  children,
}: {
  tone?: 'neutral' | 'info' | 'success' | 'warning' | 'danger';
  children: ReactNode;
}) {
  return (
    <span className="op-status-badge" data-tone={tone}>
      {children}
    </span>
  );
}

export function useRead<T>(load: (signal: AbortSignal) => Promise<T>, dependencies: unknown[], enabled = true) {
  const [retry, setRetry] = useState(0);
  const [snapshot, setSnapshot] = useState<{
    dependencies: unknown[];
    retry: number;
    enabled: boolean;
    value: T | null;
    error: string;
    busy: boolean;
  }>(() => ({ dependencies, retry, enabled, value: null, error: '', busy: enabled }));
  const sequence = useRef(0);
  useEffect(() => {
    const ticket = ++sequence.current,
      epoch = sessionBoundary.epoch,
      controller = new AbortController();
    const base = { dependencies, retry, enabled };
    setSnapshot({ ...base, value: null, error: '', busy: enabled });
    if (enabled)
      void load(controller.signal)
        .then((value) => {
          if (ticket === sequence.current && epoch === sessionBoundary.epoch)
            setSnapshot({ ...base, value, error: '', busy: false });
        })
        .catch((error) => {
          if (ticket === sequence.current && epoch === sessionBoundary.epoch)
            setSnapshot({ ...base, value: null, error: error.message, busy: false });
        });
    return () => {
      controller.abort();
      ++sequence.current;
    };
  }, [...dependencies, retry, enabled]);
  // Hide the previous scope synchronously, before effects start the next request.
  const current =
    snapshot.retry === retry &&
    snapshot.enabled === enabled &&
    snapshot.dependencies.length === dependencies.length &&
    snapshot.dependencies.every((value, index) => Object.is(value, dependencies[index]));
  return {
    value: current ? snapshot.value : null,
    error: current ? snapshot.error : '',
    busy: enabled && (!current || snapshot.busy),
    refresh: () => setRetry((x) => x + 1),
  };
}
export function Feedback({
  busy,
  error,
  empty,
  retry,
}: {
  busy: boolean;
  error: string;
  empty?: boolean;
  retry: () => void;
}) {
  const { c } = useCatalogText();
  if (busy)
    return (
      <div className="op-feedback" role="status">
        <Spinner />
        {c('正在读取…')}
      </div>
    );
  if (error)
    return (
      <div className="op-feedback op-error" role="alert">
        <span>{error}</span>
        <Button size="sm" variant="outline" onClick={retry}>
          {c('重新读取')}
        </Button>
      </div>
    );
  if (empty)
    return (
      <div className="op-feedback" role="status">
        {c('当前条件下暂无记录')}
      </div>
    );
  return null;
}
export function PageNavigation({
  page,
  busy,
  onPage,
}: {
  page: PageView<unknown>;
  busy: boolean;
  onPage: (page: string) => void;
}) {
  const { c } = useCatalogText();
  return (
    <nav className="op-pagination" aria-label={c('分页')}>
      <span>{page.caption}</span>
      <Button
        size="icon"
        variant="ghost"
        aria-label={c('上一页')}
        disabled={busy || page.previous === null}
        onClick={() => page.previous !== null && onPage(page.previous)}
      >
        <ChevronLeft />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        aria-label={c('下一页')}
        disabled={busy || page.next === null}
        onClick={() => page.next !== null && onPage(page.next)}
      >
        <ChevronRight />
      </Button>
    </nav>
  );
}
export function Choice({
  label,
  value,
  options,
  onChange,
  disabled = false,
  className = '',
}: {
  label: string;
  value: string;
  options: FilterOption[];
  onChange: (v: string) => void;
  disabled?: boolean;
  className?: string;
}) {
  const selectedLabel = options.find((x) => x.value === value)?.label || label;
  return (
    <Menu.Root>
      <Menu.Trigger asChild>
        <button className={'op-choice ' + className} disabled={disabled} aria-label={label}>
          <span className="op-choice-label" title={selectedLabel}>
            {selectedLabel}
          </span>
          <ChevronDown />
        </button>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content
          className="jm-select-content op-choice-menu"
          align="start"
          sideOffset={6}
          collisionPadding={12}
        >
          <Menu.RadioGroup value={value} onValueChange={onChange}>
            {options.map((x) => (
              <Menu.RadioItem className="xy-menu-row" key={x.value} value={x.value}>
                {x.label}
              </Menu.RadioItem>
            ))}
          </Menu.RadioGroup>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}
export function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  return (
    <label className="op-field">
      <span>{label}</span>
      {children}
      {hint ? <small>{hint}</small> : null}
    </label>
  );
}
