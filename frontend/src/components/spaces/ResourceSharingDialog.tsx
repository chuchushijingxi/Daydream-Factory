import { useEffect, useRef, useState } from 'react';
import { sharedResourceApi, type Sharing } from '../../api/shared-resource';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../ui/dialog';
import { Button } from '../ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
export function ResourceSharingDialog({
  open,
  onOpenChange,
  spaceId,
  projectId,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  spaceId: string;
  projectId: string;
}) {
  const { c } = useCatalogText();
  const [value, setValue] = useState<Sharing | null>(null),
    [members, setMembers] = useState<{ userId: string; name: string }[]>([]),
    [grantee, setGrantee] = useState('*'),
    [permission, setPermission] = useState<1 | 2>(1),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [uncertain, setUncertain] = useState(false);
  const sequence = useRef(0);
  const read = async () => {
    const ticket = ++sequence.current;
    setBusy(true);
    setError('');
    try {
      const [v, m] = await Promise.all([
        sharedResourceApi.sharing(spaceId, projectId),
        sharedResourceApi.members(spaceId),
      ]);
      if (ticket !== sequence.current) return;
      setValue(v);
      setMembers(m);
      setUncertain(false);
    } catch (e) {
      if (ticket === sequence.current) setError((e as Error).message);
    } finally {
      if (ticket === sequence.current) setBusy(false);
    }
  };
  useEffect(() => {
    if (open) {
      setValue(null);
      void read();
    }
    return () => {
      ++sequence.current;
    };
  }, [open, spaceId, projectId]);
  const change = async (action: () => Promise<Sharing>) => {
    const ticket = ++sequence.current;
    setBusy(true);
    setError('');
    try {
      const result = await action();
      if (ticket === sequence.current) setValue(result);
    } catch (e) {
      if (ticket === sequence.current) {
        setUncertain(true);
        setError((e as Error).message);
      }
    } finally {
      if (ticket === sequence.current) setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!busy) onOpenChange(v);
      }}
    >
      <DialogContent className="space-content-dialog" closeDisabled={busy}>
        <DialogTitle>{c('项目共享')}</DialogTitle>
        <DialogDescription>
          {c('仅向本团队成员共享；共享可以放宽团队档位，不能转授共享管理权。')}
        </DialogDescription>
        {error ? (
          <p role="alert" className="jm-inline-error">
            {error}
          </p>
        ) : null}
        <Button variant="outline" size="sm" disabled={busy} onClick={() => void read()}>
          {c('重新读取')}
        </Button>
        {busy && !value ? <p role="status">{c('正在读取…')}</p> : null}
        {value?.canManage ? (
          <>
            <ul className="space-share-list">
              {value.shares.map((item) => (
                <li key={item.grantee}>
                  <span>
                    {item.grantee === '*'
                      ? c('全体成员')
                      : members.find((m) => m.userId === item.grantee)?.name || item.grantee}
                  </span>
                  <span>{item.permission === 2 ? c('可编辑') : c('只读')}</span>
                  <Button
                    variant="ghost"
                    disabled={busy || uncertain}
                    onClick={() =>
                      void change(() =>
                        sharedResourceApi.deleteShare(spaceId, projectId, item.grantee),
                      )
                    }
                  >
                    {c('取消共享')}
                  </Button>
                </li>
              ))}
            </ul>
            <label>
              {c('共享给')}
              <select
                value={grantee}
                disabled={busy || uncertain}
                onChange={(e) => setGrantee(e.target.value)}
              >
                <option value="*">{c('全体成员')}</option>
                {members.map((member) => (
                  <option key={member.userId} value={member.userId}>
                    {member.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {c('共享权限')}
              <select
                value={permission}
                disabled={busy || uncertain}
                onChange={(e) => setPermission(Number(e.target.value) as 1 | 2)}
              >
                <option value="1">{c('只读')}</option>
                <option value="2">{c('可编辑')}</option>
              </select>
            </label>
            <Button
              disabled={busy || uncertain}
              onClick={() =>
                void change(() =>
                  sharedResourceApi.putShare(spaceId, projectId, grantee, permission),
                )
              }
            >
              {c('保存共享')}
            </Button>
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
