import { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown, Search } from 'lucide-react';
import { useCatalogText } from '../../contexts/useCatalogText';
export function Choice({
  label,
  value,
  items,
  onChange,
  disabled = false,
}: {
  label: string;
  value: string;
  items: { id: string; label: string; detail?: string }[];
  onChange: (id: string) => void;
  disabled?: boolean;
}) {
  const { c } = useCatalogText();
  const trigger = useRef<HTMLButtonElement>(null),
    panel = useRef<HTMLDivElement>(null),
    search = useRef<HTMLInputElement>(null),
    list = useRef<HTMLDivElement>(null),
    timer = useRef<ReturnType<typeof setTimeout>>();
  const [phase, setPhase] = useState<'closed' | 'open' | 'closing'>('closed'),
    [query, setQuery] = useState(''),
    [active, setActive] = useState(0),
    [pos, setPos] = useState({ left: 0, top: 0, width: 320, maxHeight: 320 });
  const id = useId();
  const filtered = items.filter((i) =>
    (i.label + ' ' + (i.detail || '')).toLocaleLowerCase().includes(query.toLocaleLowerCase()),
  );
  const close = (focus = true) => {
    setPhase('closing');
    clearTimeout(timer.current);
    timer.current = setTimeout(
      () => {
        setPhase('closed');
        if (focus) trigger.current?.focus({ preventScroll: true });
      },
      matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 200,
    );
  };
  useEffect(() => () => clearTimeout(timer.current), []);
  useEffect(() => {
    if (phase !== 'open') return;
    const position = () => {
      const r = trigger.current?.getBoundingClientRect();
      if (!r) return;
      const width = Math.min(380, innerWidth - 24),
        below = innerHeight - r.bottom - 16,
        above = r.top - 16,
        height = Math.min(360, Math.max(below, above));
      setPos({
        left: Math.max(12, Math.min(r.left, innerWidth - width - 12)),
        top: below >= Math.min(240, above) ? r.bottom + 8 : Math.max(8, r.top - height - 8),
        width,
        maxHeight: height,
      });
    };
    position();
    search.current?.focus({ preventScroll: true });
    const outside = (e: PointerEvent) => {
      if (
        !panel.current?.contains(e.target as Node) &&
        !trigger.current?.contains(e.target as Node)
      )
        close(false);
    };
    document.addEventListener('pointerdown', outside);
    window.addEventListener('resize', position);
    document.addEventListener('scroll', position, true);
    return () => {
      document.removeEventListener('pointerdown', outside);
      window.removeEventListener('resize', position);
      document.removeEventListener('scroll', position, true);
    };
  }, [phase]);
  const move = (next: number) => {
    setActive(next);
    const child = list.current?.children[next] as HTMLElement | undefined;
    if (child && list.current) {
      const parent = list.current;
      if (child.offsetTop < parent.scrollTop) parent.scrollTop = child.offsetTop;
      else if (child.offsetTop + child.offsetHeight > parent.scrollTop + parent.clientHeight)
        parent.scrollTop = child.offsetTop + child.offsetHeight - parent.clientHeight;
    }
  };
  return (
    <>
      <button
        ref={trigger}
        className="sb-choice"
        type="button"
        role="combobox"
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={phase === 'open'}
        aria-controls={phase !== 'closed' ? id : undefined}
        disabled={disabled}
        onKeyDown={(e) => {
          if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && phase !== 'open') {
            e.preventDefault();
            e.currentTarget.click();
          }
        }}
        onClick={() => {
          if (phase === 'open') close();
          else {
            clearTimeout(timer.current);
            setQuery('');
            setActive(
              Math.max(
                0,
                items.findIndex((i) => i.id === value),
              ),
            );
            setPhase('open');
          }
        }}
      >
        <span>{items.find((i) => i.id === value)?.label || label}</span>
        <ChevronDown size={12} />
      </button>
      {phase !== 'closed'
        ? createPortal(
            <div
              ref={panel}
              className="sb-choice-panel"
              data-state={phase}
              style={{ ...pos, position: 'fixed', height: undefined }}
            >
              <div className="sb-choice-search">
                <Search size={16} />
                <input
                  ref={search}
                  type="search"
                  aria-label={c('搜索{label}', { label })}
                  role="combobox"
                  aria-controls={id}
                  aria-expanded={phase === 'open'}
                  aria-activedescendant={filtered[active] ? `${id}-${active}` : undefined}
                  value={query}
                  onChange={(e) => {
                    setQuery(e.target.value);
                    setActive(0);
                  }}
                  onKeyDown={(e) => {
                    if (e.nativeEvent.isComposing) return;
                    if (e.key === 'Escape') {
                      e.preventDefault();
                      close();
                    } else if (e.key === 'Tab') close(false);
                    else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) {
                      e.preventDefault();
                      move(
                        e.key === 'Home'
                          ? 0
                          : e.key === 'End'
                            ? filtered.length - 1
                            : Math.max(
                                0,
                                Math.min(
                                  filtered.length - 1,
                                  active + (e.key === 'ArrowDown' ? 1 : -1),
                                ),
                              ),
                      );
                    } else if (e.key === 'Enter' && filtered[active]) {
                      e.preventDefault();
                      onChange(filtered[active].id);
                      close();
                    }
                  }}
                />
              </div>
              <div ref={list} id={id} role="listbox" aria-label={label} className="sb-choice-items">
                {filtered.map((i, n) => (
                  <button
                    id={`${id}-${n}`}
                    key={i.id}
                    type="button"
                    role="option"
                    aria-selected={i.id === value}
                    data-active={n === active}
                    tabIndex={-1}
                    onClick={() => {
                      onChange(i.id);
                      close();
                    }}
                  >
                    <span>
                      {i.label}
                      {i.detail ? <small>{i.detail}</small> : null}
                    </span>
                    {i.id === value ? <Check size={14} /> : null}
                  </button>
                ))}
                {!filtered.length ? <p>{c('无匹配结果')}</p> : null}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
