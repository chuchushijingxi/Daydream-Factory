import { useCatalogText } from '../../contexts/useCatalogText';
import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { CheckCircle2, CircleAlert, Info, TriangleAlert, X } from 'lucide-react';
import { Button } from './button';
type Notice = {
  id: string;
  message: string;
  kind: 'success' | 'info' | 'warning' | 'error';
};
let notices: Notice[] = [],
  sequence = 0;
const subscribers = new Set<() => void>();
const publish = () => subscribers.forEach((fn) => fn());
const subscribe = (fn: () => void) => {
  subscribers.add(fn);
  return () => {
    subscribers.delete(fn);
  };
};
const snapshot = () => notices;
function notify(
  kind: Notice['kind'],
  message: string,
  options?: {
    id: string;
  },
) {
  const id = options?.id || 'notice-' + ++sequence;
  notices = [
    ...notices.filter((n) => n.id !== id),
    {
      id,
      kind,
      message,
    },
  ];
  publish();
  return id;
}
export const toast = {
  dismiss: () => {
    notices = [];
    publish();
  },
  info: (message: string, options?: { id: string }) => notify('info', message, options),
  warning: (message: string, options?: { id: string }) => notify('warning', message, options),
  success: (
    message: string,
    options?: {
      id: string;
    },
  ) => notify('success', message, options),
  error: (
    message: string,
    options?: {
      id: string;
    },
  ) => notify('error', message, options),
};
function NoticeItem({ notice }: { notice: Notice }) {
  const { c } = useCatalogText();
  const [closing, setClosing] = useState(false);
  const deadline = useRef(3000),
    started = useRef(0),
    timer = useRef<ReturnType<typeof setTimeout>>(),
    removal = useRef<ReturnType<typeof setTimeout>>();
  const paused = useRef({
    hover: false,
    focus: false,
  });
  const remove = () => {
    notices = notices.filter((n) => n.id !== notice.id);
    publish();
  };
  const close = () => {
    if (closing || removal.current) return;
    clearTimeout(timer.current);
    setClosing(true);
    removal.current = setTimeout(
      remove,
      matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 300,
    );
  };
  const resume = () => {
    if (notice.kind === 'error' || removal.current || paused.current.hover || paused.current.focus)
      return;
    started.current = Date.now();
    timer.current = setTimeout(close, deadline.current);
  };
  const pause = (key: 'hover' | 'focus', value: boolean) => {
    if (value && !paused.current.hover && !paused.current.focus) {
      clearTimeout(timer.current);
      deadline.current = Math.max(0, deadline.current - (Date.now() - started.current));
    }
    paused.current[key] = value;
    if (!value) resume();
  };
  useEffect(() => {
    removal.current = undefined;
    setClosing(false);
    deadline.current = 3000;
    resume();
    return () => {
      clearTimeout(timer.current);
      clearTimeout(removal.current);
    };
  }, [notice]);
  return (
    <div
      className="jm-toast-slot"
      data-state={closing ? 'closing' : 'open'}
      aria-hidden={closing || undefined}
      onMouseEnter={() => pause('hover', true)}
      onMouseLeave={() => pause('hover', false)}
      onFocus={() => pause('focus', true)}
      onBlur={() => pause('focus', false)}
    >
      <div className="jm-toast-clip">
        <div
          className="jm-toast"
          role={notice.kind === 'error' || notice.kind === 'warning' ? 'alert' : 'status'}
          data-kind={notice.kind}
        >
          {notice.kind === 'error' ? (
            <CircleAlert className="jm-toast-icon" aria-hidden />
          ) : notice.kind === 'warning' ? (
            <TriangleAlert className="jm-toast-icon" aria-hidden />
          ) : notice.kind === 'info' ? (
            <Info className="jm-toast-icon" aria-hidden />
          ) : (
            <CheckCircle2 className="jm-toast-icon" aria-hidden />
          )}
          <span className="jm-toast-title">{notice.message}</span>
          {notice.kind === 'error' ? (
            <Button
              variant="ghost"
              size="icon"
              className="jm-toast-close"
              aria-label={c('关闭通知')}
              disabled={closing}
              onClick={close}
            >
              <X />
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  );
}
export function Toaster() {
  const { c } = useCatalogText();
  const items = useSyncExternalStore(subscribe, snapshot);
  return (
    <div
      className="jm-toaster"
      hidden={!items.length}
      aria-label={c('操作反馈')}
      aria-live="polite"
      aria-relevant="additions"
    >
      {items.map((notice) => (
        <NoticeItem key={notice.id} notice={notice} />
      ))}
    </div>
  );
}
