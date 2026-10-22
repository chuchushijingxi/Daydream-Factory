import { useRef } from 'react';
import { teamClient } from '../../api/team-client';
import { sessionBoundary } from '../../api/session-boundary';
import { useOptionalSpace } from '../../state/SpaceContext';
import type { TeamMember } from '../../api/spaces-types';
import { Feedback, useRead } from '../operations/controls';
import { TransferPanel } from './TransferPanel';

// 仅供已核验owner/admin的设置区组合；成员读取不会赋予额外管理权限。
export function TeamResourceTransferPanel({ id, onSaved }: { id: string; onSaved: () => void }) {
  const space = useOptionalSpace();
  const key = JSON.stringify([id, sessionBoundary.epoch, space?.epoch, space?.current?.role]);
  const saved = useRef<{ key: string; members: TeamMember[] } | null>(null);
  const read = useRead((signal) => teamClient.members(id, signal), [key]);
  if (read.value) saved.current = { key, members: read.value };
  const members = read.value || (saved.current?.key === key ? saved.current.members : null);
  return (
    <div className="team-stack team-transfer-panel">
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {members && (
        <TransferPanel
          key={key}
          id={id}
          members={members}
          disabled={read.busy || !!read.error}
          onSaved={() => {
            read.refresh();
            onSaved();
          }}
        />
      )}
    </div>
  );
}
