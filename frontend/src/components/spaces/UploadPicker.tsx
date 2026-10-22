import { useEffect, useRef, useState } from 'react';
import { api, protectedResourceUrl } from '../../api/client';
import { sharedResourceApi, type UploadItem } from '../../api/shared-resource';
import type { UploadedFile } from '../../api/types';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../ui/dialog';
import { Button } from '../ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
export function UploadPicker({
  open,
  onOpenChange,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSelect: (file: UploadedFile) => Promise<void>;
}) {
  const { c } = useCatalogText();
  const [items, setItems] = useState<UploadItem[]>([]),
    [cursor, setCursor] = useState<string | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState('');
  const seq = useRef(0);
  const read = async (after?: string) => {
    const ticket = ++seq.current;
    setBusy(true);
    setError('');
    try {
      const v = await sharedResourceApi.uploads('all', after);
      if (ticket !== seq.current) return;
      setItems((old) =>
        after ? [...old, ...v.items.filter((f) => !old.some((x) => x.id === f.id))] : v.items,
      );
      setCursor(v.nextCursor);
    } catch (e) {
      if (ticket === seq.current) setError((e as Error).message);
    } finally {
      if (ticket === seq.current) setBusy(false);
    }
  };
  useEffect(() => {
    setItems([]);
    setCursor(null);
    if (open) void read();
    return () => {
      ++seq.current;
    };
  }, [open]);
  const choose = async (id: string) => {
    setBusy(true);
    setError('');
    try {
      const file = await api.getFile(id);
      if (!file.contentType.startsWith('image/') || !file.thumbnailUrl)
        throw new Error(c('请选择已有真实缩略图的图片。'));
      await onSelect(file);
      onOpenChange(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!busy) onOpenChange(v);
      }}
    >
      <DialogContent variant="picker" className="space-content-dialog" closeDisabled={busy}>
        <DialogTitle>{c('选择已上传图片')}</DialogTitle>
        <DialogDescription>{c('仅列出当前空间中可独立访问的上传文件。')}</DialogDescription>
        {error ? (
          <p role="alert" className="jm-inline-error">
            {error}
            <Button variant="link" onClick={() => void read()}>
              {c('重新读取')}
            </Button>
          </p>
        ) : null}
        <div className="space-upload-grid" aria-busy={busy}>
          {items
            .filter((f) => f.hasThumbnail && f.contentType.startsWith('image/'))
            .map((file) => (
              <button
                key={file.id}
                type="button"
                disabled={busy}
                onClick={() => void choose(file.id)}
              >
                <img
                  src={protectedResourceUrl(
                    '/api/files/' + encodeURIComponent(file.id) + '/thumbnail',
                  )}
                  alt=""
                />
                <strong>{file.filename}</strong>
                <small>{file.creatorName}</small>
                {file.sharedWithMe ? <span>{c('共享给我')}</span> : null}
              </button>
            ))}
        </div>
        {!busy && !items.some((f) => f.hasThumbnail && f.contentType.startsWith('image/')) ? (
          <p>{c('暂无可选择的图片')}</p>
        ) : null}
        {cursor ? (
          <Button disabled={busy} onClick={() => void read(cursor)}>
            {c('加载更多')}
          </Button>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
