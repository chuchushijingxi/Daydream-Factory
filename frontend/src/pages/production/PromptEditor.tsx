import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Image as ImageIcon } from 'lucide-react';
import type { Asset, VisualTarget } from '../../api/types';
import type { PromptPart, ReadPart } from '../../api/storyboard-types';
import { assetLabel, cleanParts, refKey, sameRef } from '../../domain/storyboard';
import { compact, documentText, partLength, replaceParts } from './prompt-document';
import { protectedResourceUrl } from '../../api/client';
import { MentionImagePreview } from './MentionImagePreview';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useLanguage } from '../../contexts/LanguageContext';

type Bookmark = { start: number; end: number };
type Snapshot = { parts: PromptPart[]; selection: Bookmark };

function caretRect(editor: HTMLElement) {
  const selection = window.getSelection();
  if (!selection?.rangeCount || !selection.isCollapsed) return null;
  const range = selection.getRangeAt(0).cloneRange();
  if (!editor.contains(range.endContainer)) return null;
  range.collapse(false);
  const rect = range.getBoundingClientRect?.();
  if (rect?.height) return rect;
  // 文本节点边界可能没有折叠 Range 的矩形，使用相邻字符的右边缘。
  const node = range.startContainer,
    at = range.startOffset;
  if (node.nodeType === Node.TEXT_NODE && at > 0) {
    range.setStart(node, at - 1);
    const previous = range.getBoundingClientRect?.();
    if (previous?.height) {
      const newline = node.textContent?.[at - 1] === '\n';
      const editorRect = editor.getBoundingClientRect();
      return {
        left: newline ? editorRect.left : previous.right,
        top: newline ? previous.bottom : previous.top,
        bottom: newline ? previous.bottom + previous.height : previous.bottom,
      };
    }
  }
  return null;
}
export function PromptEditor({
  parts,
  selected,
  assets,
  disabled,
  onChange,
  savedParts = [],
  footer,
}: {
  parts: PromptPart[];
  selected: VisualTarget[];
  assets: Asset[];
  disabled: boolean;
  onChange: (parts: PromptPart[]) => void;
  savedParts?: ReadPart[];
  footer?: ReactNode;
}) {
  const { c } = useCatalogText();
  const { language } = useLanguage();
  const host = useRef<HTMLDivElement>(null),
    candidateList = useRef<HTMLDivElement>(null),
    current = useRef(parts),
    emitted = useRef(''),
    composing = useRef(false);
  const tokens = useRef(new WeakMap<Node, Extract<PromptPart, { type: 'asset' }>>());
  const bookmark = useRef<Bookmark>({ start: 0, end: 0 }),
    history = useRef<Snapshot[]>([{ parts, selection: bookmark.current }]),
    cursor = useRef(0);
  const [menu, setMenu] = useState<{ query: string; start: number; end: number } | null>(null),
    [active, setActive] = useState(0),
    [, redraw] = useState(0);
  const id = useId(),
    closeTimer = useRef<ReturnType<typeof setTimeout>>();
  const [closing, setClosing] = useState(false);
  const [hovered, setHovered] = useState<{ anchor: HTMLElement; target: VisualTarget } | null>(
    null,
  );
  const referenceImage = (ref: VisualTarget) => {
    const asset = assets.find((a) => a.id === ref.assetId);
    return ref.lookId ? asset?.looks.find((look) => look.id === ref.lookId)?.image : asset?.image;
  };
  const hoverImage = hovered ? referenceImage(hovered.target) : null;
  function previewReference(target: EventTarget | null) {
    const anchor = target instanceof Element ? target.closest<HTMLElement>('.sb-mention') : null;
    const ref = anchor && host.current?.contains(anchor) ? tokens.current.get(anchor) : null;
    setHovered((previous) =>
      anchor && ref && exists(ref) && referenceImage(ref)?.thumbnailUrl
        ? previous?.anchor === anchor
          ? previous
          : { anchor, target: ref }
        : null,
    );
  }
  useLayoutEffect(() => {
    if (hovered && !hovered.anchor.isConnected) setHovered(null);
  });
  const [menuPosition, setMenuPosition] = useState({
    left: 0,
    top: 0,
    width: 268,
    maxHeight: 228,
    visible: false,
    above: false,
  });
  function showMenu(value: typeof menu) {
    clearTimeout(closeTimer.current);
    setClosing(false);
    setMenu(value);
  }
  function closeMenu() {
    setClosing(true);
    clearTimeout(closeTimer.current);
    closeTimer.current = setTimeout(
      () => {
        setMenu(null);
        setClosing(false);
      },
      matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 200,
    );
  }
  useEffect(() => () => clearTimeout(closeTimer.current), []);
  useEffect(() => {
    if (disabled) closeMenu();
  }, [disabled]);
  const exists = (ref: VisualTarget) =>
    assets.some(
      (a) => a.id === ref.assetId && (!ref.lookId || a.looks.some((l) => l.id === ref.lookId)),
    );
  const label = (ref: VisualTarget) =>
    exists(ref)
      ? assetLabel(assets, ref, c)
      : savedParts.find((p) => p.type === 'asset' && sameRef(p, ref))?.label ||
        assetLabel(assets, ref, c);
  function updateToken(token: HTMLElement, ref: VisualTarget) {
    const name = label(ref),
      image = referenceImage(ref);
    token.dataset.available = String(exists(ref));
    token.setAttribute('aria-label', c('{name}，资产引用', { name }));
    let text = token.querySelector<HTMLElement>('.sb-mention-label');
    if (!text) {
      text = document.createElement('span');
      text.className = 'sb-mention-label';
      token.append(text);
    }
    if (text.textContent !== name) text.textContent = name;
    const thumbnail = image?.thumbnailUrl ? protectedResourceUrl(image.thumbnailUrl) : '';
    let img = token.querySelector<HTMLImageElement>('img');
    if (!thumbnail) {
      img?.remove();
      return;
    }
    if (!img) {
      img = document.createElement('img');
      img.alt = '';
      img.setAttribute('aria-hidden', 'true');
      img.draggable = false;
      img.loading = 'lazy';
      img.addEventListener('error', () => {
        img!.hidden = true;
      });
      token.prepend(img);
    }
    if (img.getAttribute('src') !== thumbnail) {
      img.hidden = false;
      img.src = thumbnail;
    }
  }
  useLayoutEffect(() => {
    host.current?.childNodes.forEach((node) => {
      const ref = tokens.current.get(node);
      if (ref && node instanceof HTMLElement) updateToken(node, ref);
    });
  }, [assets, savedParts, language]);
  function length(node: Node): number {
    if (tokens.current.has(node)) return 1;
    if (node.nodeType === 3) return node.textContent?.length || 0;
    return Array.from(node.childNodes).reduce((n, c) => n + length(c), 0);
  }
  function offset(node: Node, at: number): number {
    // Images and labels are presentation inside one atomic reference.
    for (
      let ancestor: Node | null = node;
      ancestor && ancestor !== host.current;
      ancestor = ancestor.parentNode
    )
      if (tokens.current.has(ancestor) && ancestor.parentNode) {
        const parent = ancestor.parentNode,
          index = Array.from(parent.childNodes).indexOf(ancestor as ChildNode);
        return offset(parent, index + (at > 0 ? 1 : 0));
      }
    let n =
      node.nodeType === 3
        ? at
        : Array.from(node.childNodes)
            .slice(0, at)
            .reduce((n, c) => n + length(c), 0);
    while (node !== host.current && node.parentNode) {
      const parent = node.parentNode;
      for (const c of Array.from(parent.childNodes)) {
        if (c === node) break;
        n += length(c);
      }
      node = parent;
    }
    return n;
  }
  function selection(): Bookmark {
    const s = window.getSelection();
    if (
      !s?.anchorNode ||
      !s.focusNode ||
      !host.current?.contains(s.anchorNode) ||
      !host.current.contains(s.focusNode)
    )
      return bookmark.current;
    const a = offset(s.anchorNode, s.anchorOffset),
      b = offset(s.focusNode, s.focusOffset);
    bookmark.current = { start: Math.min(a, b), end: Math.max(a, b) };
    return bookmark.current;
  }
  function place(b: Bookmark) {
    const root = host.current;
    if (!root) return;
    function point(at: number): [Node, number] {
      let rest = at;
      for (let i = 0; i < root!.childNodes.length; i++) {
        const n = root!.childNodes[i],
          size = length(n);
        if (tokens.current.has(n)) {
          if (rest === 0) return [root!, i];
          if (rest === 1) return [root!, i + 1];
        } else if (rest <= size && n.nodeType === 3) return [n, rest];
        rest -= size;
      }
      return [root!, root!.childNodes.length];
    }
    const r = document.createRange(),
      [a, ao] = point(b.start),
      [z, zo] = point(b.end);
    r.setStart(a, ao);
    r.setEnd(z, zo);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(r);
    bookmark.current = b;
  }
  function render(value: PromptPart[], b?: Bookmark) {
    const el = host.current;
    if (!el) return;
    el.replaceChildren();
    for (const p of value)
      if (p.type === 'text') el.append(document.createTextNode(p.text));
      else {
        const token = document.createElement('span');
        token.className = 'sb-mention';
        token.contentEditable = 'false';
        token.tabIndex = 0;
        updateToken(token, p);
        tokens.current.set(token, p);
        el.append(token);
      }
    if (b) place(b);
  }
  function read(): PromptPart[] {
    const out: PromptPart[] = [];
    function visit(n: Node) {
      const token = tokens.current.get(n);
      if (token) out.push(token);
      else if (n.nodeType === 3) out.push({ type: 'text', text: n.textContent || '' });
      else if (n.nodeName === 'BR') out.push({ type: 'text', text: '\n' });
      else {
        if ((n.nodeName === 'DIV' || n.nodeName === 'P') && out.length)
          out.push({ type: 'text', text: '\n' });
        n.childNodes.forEach(visit);
      }
    }
    host.current?.childNodes.forEach(visit);
    return compact(out);
  }
  function publish(value: PromptPart[], b: Bookmark, paint = false, record = true) {
    current.current = value;
    emitted.current = JSON.stringify(value);
    if (paint) render(value, b);
    if (record) {
      history.current = history.current.slice(0, cursor.current + 1);
      history.current.push({ parts: structuredClone(value), selection: b });
      cursor.current++;
    }
    redraw((n) => n + 1);
    onChange(value);
  }
  function mention(value: PromptPart[], b: Bookmark) {
    let pos = 0,
      found: typeof menu = null;
    if (b.start === b.end)
      for (const p of value) {
        const end = pos + partLength(p);
        if (p.type === 'text' && b.start >= pos && b.start <= end) {
          const text = p.text.slice(0, b.start - pos),
            at = text.lastIndexOf('@'),
            query = text.slice(at + 1);
          if (at >= 0 && !/[\s@]/.test(query)) found = { query, start: pos + at, end: b.start };
        }
        pos = end;
      }
    showMenu(found);
    setActive(0);
  }
  function insert(value: PromptPart[], range = selection()) {
    const next = replaceParts(current.current, range.start, range.end, value),
      at = range.start + value.reduce((n, p) => n + partLength(p), 0);
    host.current?.focus({ preventScroll: true });
    publish(next, { start: at, end: at }, true);
    closeMenu();
  }
  function undo(delta: number) {
    if (disabled) return;
    const next = cursor.current + delta;
    if (next < 0 || next >= history.current.length) return;
    cursor.current = next;
    const s = history.current[next];
    host.current?.focus({ preventScroll: true });
    publish(s.parts, s.selection, true, false);
    closeMenu();
  }
  useLayoutEffect(() => {
    const clean = cleanParts(parts),
      key = JSON.stringify(clean);
    if (key !== emitted.current) {
      current.current = clean;
      emitted.current = key;
      render(clean);
      history.current = [{ parts: clean, selection: { start: 0, end: 0 } }];
      cursor.current = 0;
    }
  }, [parts]);
  const candidates = selected.filter(exists).filter((ref) =>
    label(ref)
      .toLocaleLowerCase()
      .includes(menu?.query.toLocaleLowerCase() || ''),
  );
  useLayoutEffect(() => {
    if (!menu || closing) return;
    const position = () => {
      const editor = host.current,
        list = candidateList.current;
      if (!editor || !list) return;
      const caret = caretRect(editor),
        bounds = editor.getBoundingClientRect();
      const viewport = window.visualViewport;
      const leftEdge = viewport?.offsetLeft || 0,
        topEdge = viewport?.offsetTop || 0;
      const rightEdge = leftEdge + (viewport?.width || window.innerWidth);
      const bottomEdge = topEdge + (viewport?.height || window.innerHeight);
      if (
        !caret ||
        caret.bottom <= Math.max(bounds.top, topEdge) ||
        caret.top >= Math.min(bounds.bottom, bottomEdge)
      ) {
        setMenuPosition((current) => (current.visible ? { ...current, visible: false } : current));
        return;
      }
      const width = Math.min(268, rightEdge - leftEdge - 16);
      const height = Math.min(228, list.scrollHeight + 2);
      const below = Math.max(0, bottomEdge - caret.bottom - 16);
      const above = Math.max(0, caret.top - topEdge - 16);
      const placeAbove = below < height && above > below;
      const maxHeight = Math.min(228, placeAbove ? above : below);
      const next = {
        left: Math.max(leftEdge + 8, Math.min(caret.left, rightEdge - width - 8)),
        top: placeAbove ? caret.top - Math.min(height, maxHeight) - 8 : caret.bottom + 8,
        width,
        maxHeight,
        visible: maxHeight > 0,
        above: placeAbove,
      };
      setMenuPosition((current) =>
        Object.keys(next).every(
          (key) => current[key as keyof typeof next] === next[key as keyof typeof next],
        )
          ? current
          : next,
      );
    };
    position();
    const observer = new ResizeObserver(position);
    observer.observe(host.current!);
    observer.observe(candidateList.current!);
    window.addEventListener('resize', position);
    document.addEventListener('scroll', position, true);
    document.addEventListener('selectionchange', position);
    window.visualViewport?.addEventListener('resize', position);
    window.visualViewport?.addEventListener('scroll', position);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', position);
      document.removeEventListener('scroll', position, true);
      document.removeEventListener('selectionchange', position);
      window.visualViewport?.removeEventListener('resize', position);
      window.visualViewport?.removeEventListener('scroll', position);
    };
  }, [menu, closing, candidates.length]);
  function choose(ref: VisualTarget) {
    if (!menu || closing || disabled) return;
    insert([{ type: 'asset', ...ref }], { start: menu.start, end: menu.end });
  }
  return (
    <div className="sb-prompt-shell">
      <div
        ref={host}
        className="sb-prompt-editor"
        role="textbox"
        aria-label={c('视频提示词')}
        aria-multiline="true"
        aria-readonly={disabled}
        aria-expanded={!!menu && !closing && menuPosition.visible}
        aria-controls={menu ? id : undefined}
        aria-activedescendant={
          menu && !closing && menuPosition.visible && candidates.length
            ? `${id}-${active}`
            : undefined
        }
        contentEditable={!disabled}
        suppressContentEditableWarning
        onPointerOver={(event) => previewReference(event.target)}
        onPointerOut={(event) => {
          if (
            hovered &&
            (!(event.relatedTarget instanceof Node) ||
              !hovered.anchor.contains(event.relatedTarget))
          )
            setHovered(null);
        }}
        onFocusCapture={(event) => previewReference(event.target)}
        onBlurCapture={(event) => {
          if (
            hovered &&
            (!(event.relatedTarget instanceof Node) ||
              !hovered.anchor.contains(event.relatedTarget))
          )
            setHovered(null);
        }}
        onCompositionStart={() => {
          composing.current = true;
          closeMenu();
        }}
        onCompositionEnd={() => {
          composing.current = false;
          const value = read(),
            b = selection();
          publish(value, b);
          mention(value, b);
        }}
        onInput={() => {
          if (composing.current) return;
          const value = read(),
            b = selection();
          publish(value, b);
          mention(value, b);
        }}
        onBeforeInput={(e) => {
          const type = (e.nativeEvent as InputEvent).inputType;
          if (type === 'historyUndo' || type === 'historyRedo') {
            e.preventDefault();
            undo(type === 'historyUndo' ? -1 : 1);
          }
        }}
        onBlur={(e) => {
          if (!candidateList.current?.contains(e.relatedTarget as Node)) closeMenu();
        }}
        onMouseUp={() => {
          if (!disabled && !composing.current) mention(current.current, selection());
        }}
        onKeyUp={(e) => {
          if (disabled || composing.current) return;
          const b = selection();
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') mention(current.current, b);
        }}
        onKeyDown={(e) => {
          if (disabled || composing.current || e.nativeEvent.isComposing || e.keyCode === 229)
            return;
          if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z') {
            e.preventDefault();
            undo(e.shiftKey ? 1 : -1);
            return;
          }
          if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'y') {
            e.preventDefault();
            undo(1);
            return;
          }
          if (menu && !closing) {
            if (e.key === 'Escape') {
              e.preventDefault();
              closeMenu();
              return;
            }
            if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) {
              e.preventDefault();
              const next =
                e.key === 'Home'
                  ? 0
                  : e.key === 'End'
                    ? Math.max(0, candidates.length - 1)
                    : (active + (e.key === 'ArrowDown' ? 1 : -1) + Math.max(1, candidates.length)) %
                      Math.max(1, candidates.length);
              setActive(next);
              const item = document.getElementById(`${id}-${next}`),
                list = candidateList.current;
              if (item && list) {
                if (item.offsetTop < list.scrollTop) list.scrollTop = item.offsetTop;
                else if (item.offsetTop + item.offsetHeight > list.scrollTop + list.clientHeight)
                  list.scrollTop = item.offsetTop + item.offsetHeight - list.clientHeight;
              }
              return;
            }
            if (e.key === 'Enter') {
              e.preventDefault();
              if (candidates[active]) choose(candidates[active]);
              return;
            }
            if (e.key === 'Tab') closeMenu();
          }
          if (e.key === 'Enter') {
            e.preventDefault();
            insert([{ type: 'text', text: '\n' }]);
            return;
          }
          if (e.key === 'Backspace' || e.key === 'Delete') {
            const b = selection();
            if (b.start !== b.end) {
              e.preventDefault();
              insert([], b);
              return;
            }
            let at = 0;
            const target = e.key === 'Backspace' ? b.start - 1 : b.start;
            for (const p of current.current) {
              if (p.type === 'asset' && at === target) {
                e.preventDefault();
                insert([], { start: at, end: at + 1 });
                return;
              }
              at += partLength(p);
            }
          }
        }}
        onPaste={(e) => {
          e.preventDefault();
          if (!disabled) insert([{ type: 'text', text: e.clipboardData.getData('text/plain') }]);
        }}
        onDrop={(e) => e.preventDefault()}
        onCopy={(e) => {
          const b = selection();
          e.preventDefault();
          const cut = replaceParts(
            replaceParts(
              current.current,
              b.end,
              current.current.reduce((n, p) => n + partLength(p), 0),
              [],
            ),
            0,
            b.start,
            [],
          );
          e.clipboardData.setData(
            'text/plain',
            documentText(cut, (p) => label(p)),
          );
        }}
        onCut={(e) => {
          const b = selection();
          e.preventDefault();
          const cut = replaceParts(
            replaceParts(
              current.current,
              b.end,
              current.current.reduce((n, p) => n + partLength(p), 0),
              [],
            ),
            0,
            b.start,
            [],
          );
          e.clipboardData.setData(
            'text/plain',
            documentText(cut, (p) => label(p)),
          );
          if (!disabled) insert([], b);
        }}
      />
      {footer}
      {hovered && hoverImage?.thumbnailUrl ? (
        <MentionImagePreview
          key={hoverImage.fileId + ':' + refKey(hovered.target)}
          anchor={hovered.anchor}
          thumbnailUrl={hoverImage.thumbnailUrl}
          label={label(hovered.target)}
          onClose={() => setHovered(null)}
        />
      ) : null}
      {menu
        ? createPortal(
            <div
              ref={candidateList}
              id={id}
              className="sb-mention-list"
              data-state={closing ? 'closing' : 'open'}
              role="listbox"
              aria-label={c('本镜头已选资产')}
              style={{
                position: 'fixed',
                left: menuPosition.left,
                top: menuPosition.top,
                width: menuPosition.width,
                maxHeight: menuPosition.maxHeight,
                visibility: menuPosition.visible ? 'visible' : 'hidden',
                transformOrigin: menuPosition.above ? 'bottom left' : 'top left',
              }}
            >
              {candidates.length ? (
                candidates.map((ref, i) => {
                  const asset = assets.find((a) => a.id === ref.assetId);
                  const image = ref.lookId
                    ? asset?.looks.find((look) => look.id === ref.lookId)?.image
                    : asset?.image;
                  return (
                    <button
                      key={refKey(ref)}
                      id={`${id}-${i}`}
                      type="button"
                      role="option"
                      tabIndex={-1}
                      aria-selected={i === active}
                      onMouseEnter={() => setActive(i)}
                      onMouseDown={(e) => e.preventDefault()}
                      onClick={() => choose(ref)}
                    >
                      <span className="sb-mention-option-image" aria-hidden="true">
                        {image?.thumbnailUrl ? (
                          <img
                            src={protectedResourceUrl(image.thumbnailUrl)}
                            alt=""
                            loading="lazy"
                          />
                        ) : (
                          <ImageIcon />
                        )}
                      </span>
                      <span className="sb-mention-option-label">
                        <span>{label(ref)}</span>
                        <small>{c('已选参考素材')}</small>
                      </span>
                    </button>
                  );
                })
              ) : (
                <p>{c('没有匹配的已选资产，请先添加参考素材。')}</p>
              )}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
