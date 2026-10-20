import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type PointerEvent } from 'react';
import type { StoryShot } from '../../api/storyboard-types';

type Preview = { ids: string[]; original: string[]; slots: number[]; revision: number };
export type FloatingShot = {
  shot: StoryShot;
  number: number;
  x: number;
  y: number;
  width: number;
  height: number;
  angle: number;
  settling: boolean;
};
type Session = Preview & {
  id: string;
  pointerId: number;
  startX: number;
  startY: number;
  x: number;
  y: number;
  lastX: number;
  offsetX: number;
  offsetY: number;
  active: boolean;
  floating: FloatingShot;
};

export function useShotDrag({ shots, revision, disabled, onReorder }: {
  shots: StoryShot[];
  revision: number;
  disabled: boolean;
  onReorder: (ids: string[], revision: number) => Promise<boolean>;
}) {
  const track = useRef<HTMLDivElement>(null), cards = useRef(new Map<string, HTMLDivElement>());
  const latest = useRef({ shots, revision, disabled, onReorder });
  latest.current = { shots, revision, disabled, onReorder };
  const session = useRef<Session | null>(null), cleanup = useRef<() => void>();
  const scrollFrame = useRef<number>(), settleTimer = useRef<ReturnType<typeof setTimeout>>();
  const saving = useRef(false), mounted = useRef(true), suppressClick = useRef(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [floating, setFloating] = useState<FloatingShot | null>(null);
  const renderedRevision = useRef(revision);
  const revisionChanged = renderedRevision.current !== revision;
  useLayoutEffect(() => { renderedRevision.current = revision; }, [revision]);
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

  function releaseListeners() {
    cleanup.current?.();
    cleanup.current = undefined;
    if (scrollFrame.current !== undefined) cancelAnimationFrame(scrollFrame.current);
    scrollFrame.current = undefined;
    session.current = null;
  }
  function settle(s: Session, order: string[]) {
    const node = track.current;
    if (!node || reduced()) { setFloating(null); return; }
    const rect = node.getBoundingClientRect(), slot = s.slots[order.indexOf(s.id)];
    setFloating({ ...s.floating, number: order.indexOf(s.id) + 1,
      x: rect.left + slot - node.scrollLeft, y: rect.top, angle: 0, settling: true });
    clearTimeout(settleTimer.current);
    settleTimer.current = setTimeout(() => { if (mounted.current) setFloating(null); }, 160);
  }
  function cancel(animate = true) {
    const current = session.current;
    releaseListeners();
    setPreview(null);
    if (current?.active) {
      suppressClick.current = true;
      if (animate) settle(current, current.original);
      else setFloating(null);
    }
  }
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      releaseListeners();
      clearTimeout(settleTimer.current);
    };
  }, []);
  useEffect(() => { if (session.current) cancel(false); }, [revision, disabled]);

  function snapshot(): Preview | null {
    const node = track.current;
    if (!node) return null;
    const rect = node.getBoundingClientRect(), ids = latest.current.shots.map(shot => shot.id);
    return { ids, original: ids, revision: latest.current.revision,
      slots: ids.map(id => (cards.current.get(id)?.getBoundingClientRect().left ?? rect.left) - rect.left + node.scrollLeft) };
  }
  async function commit(next: Preview) {
    if (saving.current) return;
    if (latest.current.disabled || next.revision !== latest.current.revision || next.ids.every((id, i) => id === next.original[i])) {
      setPreview(null);
      return;
    }
    saving.current = true;
    setPreview(next);
    try {
      // 只有松手后的完整顺序进入原CAS；指针移动不能成为项目事实。
      await latest.current.onReorder(next.ids, next.revision);
    } finally {
      saving.current = false;
      if (mounted.current) setPreview(null);
    }
  }
  function move(from: string, to: string) {
    const next = snapshot();
    if (!next) return;
    const start = next.ids.indexOf(from), end = next.ids.indexOf(to);
    if (start < 0 || end < 0 || start === end) return;
    next.ids = [...next.ids];
    next.ids.splice(start, 1);
    next.ids.splice(end, 0, from);
    void commit(next);
  }
  function project(s: Session) {
    const node = track.current;
    if (!node) return;
    const rect = node.getBoundingClientRect();
    if (s.x >= rect.left && s.x <= rect.right && s.y >= rect.top && s.y <= rect.bottom) {
      const x = s.x - rect.left + node.scrollLeft;
      // 固定槽位计算目标，避免卡片让位时再次命中移动中的卡片而来回跳动。
      const at = s.slots.reduce((best, left, i) => Math.abs(left + s.floating.width / 2 - x) < Math.abs(s.slots[best] + s.floating.width / 2 - x) ? i : best, 0);
      const ids = [...s.original], from = ids.indexOf(s.id);
      ids.splice(from, 1);
      ids.splice(at, 0, s.id);
      if (ids.some((id, i) => id !== s.ids[i])) {
        s.ids = ids;
        setPreview({ ids, original: s.original, slots: s.slots, revision: s.revision });
      }
      s.floating.number = at + 1;
    }
    setFloating({ ...s.floating, x: s.x - s.offsetX, y: s.y - s.offsetY });
  }
  function autoScroll() {
    scrollFrame.current = undefined;
    const s = session.current, node = track.current;
    if (!s?.active || !node) return;
    const rect = node.getBoundingClientRect();
    if (s.y < rect.top || s.y > rect.bottom || s.x < rect.left || s.x > rect.right) return;
    const delta = s.x < rect.left + 28 ? -10 : s.x > rect.right - 28 ? 10 : 0;
    const before = node.scrollLeft;
    node.scrollLeft = Math.max(0, Math.min(node.scrollWidth - node.clientWidth, before + delta));
    if (node.scrollLeft !== before) {
      project(s);
      scrollFrame.current = requestAnimationFrame(autoScroll);
    }
  }
  function pointerDown(event: PointerEvent<HTMLButtonElement>, shot: StoryShot, index: number) {
    suppressClick.current = false;
    if (event.button !== 0 || event.isPrimary === false || disabled || saving.current || floating?.settling || shots.length < 2) return;
    const initial = snapshot();
    if (!initial) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const s: Session = { ...initial, id: shot.id, pointerId: event.pointerId,
      startX: event.clientX, startY: event.clientY, x: event.clientX, y: event.clientY, lastX: event.clientX,
      offsetX: event.clientX - rect.left, offsetY: event.clientY - rect.top, active: false,
      floating: { shot, number: index + 1, x: rect.left, y: rect.top, width: rect.width, height: rect.height, angle: 0, settling: false } };
    releaseListeners();
    session.current = s;
    const moving = (e: globalThis.PointerEvent) => {
      if (e.pointerId !== s.pointerId || session.current !== s) return;
      if (latest.current.disabled || latest.current.revision !== s.revision) { cancel(false); return; }
      s.x = e.clientX; s.y = e.clientY;
      if (!s.active && Math.max(Math.abs(s.x - s.startX), Math.abs(s.y - s.startY)) <= 4) return;
      s.active = true;
      if (e.cancelable) e.preventDefault();
      if (Math.abs(s.x - s.lastX) > .5) {
        s.floating.angle = reduced() ? 0 : s.x > s.lastX ? 1.5 : -1.5;
        s.lastX = s.x;
      }
      project(s);
      if (scrollFrame.current === undefined) scrollFrame.current = requestAnimationFrame(autoScroll);
    };
    const up = (e: globalThis.PointerEvent) => {
      if (e.pointerId !== s.pointerId || session.current !== s) return;
      const bounds = track.current?.getBoundingClientRect();
      if (!s.active) { releaseListeners(); return; }
      if (!bounds || e.clientX < bounds.left || e.clientX > bounds.right || e.clientY < bounds.top || e.clientY > bounds.bottom || latest.current.disabled || latest.current.revision !== s.revision) { cancel(); return; }
      s.x = e.clientX;
      s.y = e.clientY;
      project(s);
      releaseListeners();
      suppressClick.current = true;
      settle(s, s.ids);
      void commit(s);
    };
    const cancelled = () => cancel();
    const pointerCancelled = (e: globalThis.PointerEvent) => { if (e.pointerId === s.pointerId) cancel(); };
    const escaped = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.preventDefault(); cancel(); } };
    const scrolled = () => { if (session.current?.active) project(s); };
    window.addEventListener('pointermove', moving, { passive: false });
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', pointerCancelled);
    window.addEventListener('blur', cancelled);
    window.addEventListener('resize', cancelled);
    window.addEventListener('keydown', escaped);
    track.current?.addEventListener('scroll', scrolled);
    const scrollNode = track.current;
    cleanup.current = () => {
      window.removeEventListener('pointermove', moving);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', pointerCancelled);
      window.removeEventListener('blur', cancelled);
      window.removeEventListener('resize', cancelled);
      window.removeEventListener('keydown', escaped);
      scrollNode?.removeEventListener('scroll', scrolled);
    };
  }
  function style(id: string): CSSProperties | undefined {
    // 正式顺序回读后DOM已经落到预览位置，此时清平移不再播放第二次让位。
    if (revisionChanged || (preview && preview.revision !== revision)) return { transition: 'none' };
    if (!preview) return;
    const from = preview.original.indexOf(id), to = preview.ids.indexOf(id);
    return from < 0 || to < 0 ? undefined : { transform: `translate3d(${preview.slots[to] - preview.slots[from]}px,0,0)` };
  }
  return { track, cards, floating, pointerDown, move, style,
    number: (id: string, index: number) => preview?.revision === revision ? preview.ids.indexOf(id) + 1 : index + 1,
    resetClick: () => { suppressClick.current = false; },
    consumeClick: () => { const ignored = suppressClick.current; suppressClick.current = false; return ignored; },
  };
}
