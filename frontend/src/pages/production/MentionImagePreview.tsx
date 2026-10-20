import { useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { protectedResourceUrl } from '../../api/client';

// 浮层放到编辑器之外，不能让预览图片进入 parts、选区或复制内容。
export function MentionImagePreview({
  anchor,
  thumbnailUrl,
  label,
  onClose,
}: {
  anchor: HTMLElement;
  thumbnailUrl: string;
  label: string;
  onClose: () => void;
}) {
  const panel = useRef<HTMLDivElement>(null),
    id = useId();
  const [position, setPosition] = useState({ left: 0, top: 0, visible: false });
  const close = useRef(onClose);
  close.current = onClose;
  function place() {
    if (!anchor.isConnected || !panel.current) return;
    const rect = anchor.getBoundingClientRect(),
      preview = panel.current.getBoundingClientRect();
    const viewport = window.visualViewport;
    const left = viewport?.offsetLeft || 0,
      top = viewport?.offsetTop || 0;
    const right = left + (viewport?.width || window.innerWidth),
      bottom = top + (viewport?.height || window.innerHeight);
    const above = rect.top - preview.height - 8;
    setPosition({
      left: Math.max(
        left + 12,
        Math.min(rect.left + rect.width / 2 - preview.width / 2, right - preview.width - 12),
      ),
      top: Math.max(
        top + 12,
        Math.min(above >= top + 12 ? above : rect.bottom + 8, bottom - preview.height - 12),
      ),
      visible: true,
    });
  }
  useLayoutEffect(() => {
    const previous = anchor.getAttribute('aria-describedby');
    anchor.setAttribute('aria-describedby', [previous, id].filter(Boolean).join(' '));
    const dismiss = () => close.current();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') dismiss();
    };
    document.addEventListener('scroll', dismiss, true);
    window.addEventListener('resize', dismiss);
    window.addEventListener('blur', dismiss);
    document.addEventListener('keydown', keydown);
    window.visualViewport?.addEventListener('resize', dismiss);
    window.visualViewport?.addEventListener('scroll', dismiss);
    return () => {
      if (previous === null) anchor.removeAttribute('aria-describedby');
      else anchor.setAttribute('aria-describedby', previous);
      document.removeEventListener('scroll', dismiss, true);
      window.removeEventListener('resize', dismiss);
      window.removeEventListener('blur', dismiss);
      document.removeEventListener('keydown', keydown);
      window.visualViewport?.removeEventListener('resize', dismiss);
      window.visualViewport?.removeEventListener('scroll', dismiss);
    };
  }, [anchor, id]);
  return createPortal(
    <div
      ref={panel}
      id={id}
      role="tooltip"
      className="sb-mention-image-preview"
      style={{
        left: position.left,
        top: position.top,
        visibility: position.visible ? 'visible' : 'hidden',
      }}
    >
      <img
        src={protectedResourceUrl(thumbnailUrl)}
        alt={label}
        draggable={false}
        onLoad={place}
        onError={() => close.current()}
      />
    </div>,
    document.body,
  );
}
