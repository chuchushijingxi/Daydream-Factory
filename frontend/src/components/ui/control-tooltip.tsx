import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useId } from 'react';

/** Small control tooltip: keyboard focus is immediate; hover waits 150 ms. */
export function ControlTooltip({
  label,
  children,
  disabled = false,
}: {
  label: string;
  children: ReactNode;
  disabled?: boolean;
}) {
  const id = useId(),
    anchor = useRef<HTMLSpanElement>(null),
    tip = useRef<HTMLDivElement>(null);
  const opening = useRef<ReturnType<typeof setTimeout>>(),
    closing = useRef<ReturnType<typeof setTimeout>>();
  const suppressed = useRef(false);
  const [phase, setPhase] = useState<'closed' | 'open' | 'closing'>('closed');
  const [position, setPosition] = useState({
    left: 0,
    top: 0,
    origin: 'center',
  });
  const clear = () => {
    clearTimeout(opening.current);
    clearTimeout(closing.current);
  };
  const show = (immediate = false) => {
    clear();
    if (suppressed.current) return;
    if (immediate) setPhase('open');
    else opening.current = setTimeout(() => setPhase('open'), 150);
  };
  const hide = () => {
    clear();
    setPhase((p) => (p === 'closed' ? p : 'closing'));
    closing.current = setTimeout(
      () => setPhase('closed'),
      matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 200,
    );
  };
  useEffect(() => clear, []);
  useLayoutEffect(() => {
    if (phase === 'closed') return;
    const place = () => {
      if (!anchor.current || !tip.current) return;
      const a = anchor.current.getBoundingClientRect(),
        t = {
          width: tip.current.offsetWidth,
          height: tip.current.offsetHeight,
        };
      const above = a.top >= t.height + 12;
      setPosition({
        left: Math.max(8, Math.min(innerWidth - t.width - 8, a.left + (a.width - t.width) / 2)),
        top: above ? a.top - t.height - 4 : Math.min(innerHeight - t.height - 8, a.bottom + 4),
        origin: above ? 'bottom center' : 'top center',
      });
    };
    place();
    addEventListener('resize', place);
    addEventListener('scroll', place, true);
    const escape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        suppressed.current = true;
        hide();
      }
    };
    document.addEventListener('keydown', escape);
    return () => {
      removeEventListener('resize', place);
      removeEventListener('scroll', place, true);
      document.removeEventListener('keydown', escape);
    };
  }, [phase, label]);
  return (
    <span
      className="jm-tooltip-anchor"
      ref={anchor}
      tabIndex={disabled ? 0 : undefined}
      aria-label={disabled ? label : undefined}
      aria-describedby={phase !== 'closed' ? id : undefined}
      onMouseEnter={() => show()}
      onMouseLeave={() => {
        suppressed.current = false;
        hide();
      }}
      onFocus={() => {
        suppressed.current = false;
        show(true);
      }}
      onBlur={hide}
    >
      {children}
      {phase !== 'closed' &&
        createPortal(
          <div
            ref={tip}
            id={id}
            role="tooltip"
            className="jm-tooltip"
            data-state={phase}
            style={{
              left: position.left,
              top: position.top,
              transformOrigin: position.origin,
            }}
          >
            {label}
          </div>,
          document.body,
        )}
    </span>
  );
}
