import { createContext, useContext, useEffect, useRef, useState } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { shareInvalidations } from '../../api/invalidation-stream';
import { sessionAudience } from '../../api/session-audience';
import { teamClient } from '../../api/team-client';

// 团队页订阅成员、邀请与加入申请的失效提示，返回递增修订号；断线按退避重连，
// 每个 ready 帧（含重连）都触发一次静默重读，补上断线期间可能错过的变化。
export function useTeamUpdates(spaceId: string, enabled: boolean) {
  const session = useContext(SessionOptionalContext);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!enabled || !spaceId || !session?.user) return;
    const controller = new AbortController(),
      epoch = sessionBoundary.epoch;
    let stopped = false,
      attempt = 0,
      timer: ReturnType<typeof setTimeout>,
      batch: ReturnType<typeof setTimeout> | undefined;
    const changed = () => {
      if (stopped || epoch !== sessionBoundary.epoch) return;
      attempt = 0;
      if (batch !== undefined) return;
      batch = setTimeout(() => {
        batch = undefined;
        if (!stopped && epoch === sessionBoundary.epoch) setRevision((value) => value + 1);
      }, 100);
    };
    const connect = async () => {
      try {
        await shareInvalidations(
          JSON.stringify([sessionAudience(), session.user!.identity]),
          'team:' + spaceId,
          (change, signal) => teamClient.watch(spaceId, change, signal),
          changed,
          controller.signal,
        );
      } catch {
        if (stopped || epoch !== sessionBoundary.epoch || controller.signal.aborted) return;
        timer = setTimeout(
          () => void connect(),
          Math.min(30000, 1000 * 2 ** Math.min(attempt++, 5)),
        );
      }
    };
    void connect();
    return () => {
      stopped = true;
      controller.abort();
      clearTimeout(timer);
      clearTimeout(batch);
    };
  }, [spaceId, enabled, session?.user?.identity, session?.epoch]);
  return revision;
}

export const TeamRevisionContext = createContext(0);

// 面板在团队修订号变化时静默重读；挂载时不触发，初次读取由面板自身完成。
export function useTeamRevision(reload: () => void) {
  const revision = useContext(TeamRevisionContext),
    seen = useRef(revision),
    callback = useRef(reload);
  callback.current = reload;
  useEffect(() => {
    if (seen.current === revision) return;
    seen.current = revision;
    callback.current();
  }, [revision]);
}
