import { useContext, useEffect, useRef } from 'react';
import { http } from '../../api/client';
import { shareInvalidations, watchInvalidations } from '../../api/invalidation-stream';
import { sessionBoundary } from '../../api/session-boundary';
import { sessionAudience } from '../../api/session-audience';
import { SessionOptionalContext } from '../../state/SessionContext';

// Project/media consumers share an authenticated invalidation stream, including
// across tabs. A slow reread covers interrupted streams without losing drafts.
export function useProjectUpdates(projectId: string | undefined, onChange: () => void | Promise<unknown>) {
  const session = useContext(SessionOptionalContext), callback = useRef(onChange);
  callback.current = onChange;
  useEffect(() => {
    if (!projectId) return;
    const controller = new AbortController(), epoch = sessionBoundary.epoch;
    let timer: ReturnType<typeof setTimeout> | undefined,
      batch: ReturnType<typeof setTimeout> | undefined,
      attempt = 0, reading = false, dirty = false;
    const current = () => !controller.signal.aborted && epoch === sessionBoundary.epoch;
    const deliver = async () => {
      if (document.hidden || !current()) return;
      if (reading) { dirty = true; return; }
      reading = true;
      try { await callback.current(); } catch { /* Consumers retain their read errors. */ }
      finally {
        reading = false;
        if (dirty) { dirty = false; changed(); }
      }
    };
    const changed = () => {
      if (document.hidden || !current()) return;
      if (reading) { dirty = true; return; }
      if (batch !== undefined) return;
      attempt = 0;
      batch = setTimeout(() => { batch = undefined; void deliver(); }, 100);
    };
    const connect = async () => {
      if (!session?.user) return;
      try {
        await shareInvalidations(
          JSON.stringify([sessionAudience(), session.user!.identity]), 'project:' + projectId,
          (change, signal) => watchInvalidations(http, '/api/projects/' + encodeURIComponent(projectId) + '/events', 'project', change, signal),
          changed, controller.signal,
        );
      } catch {
        if (!controller.signal.aborted && epoch === sessionBoundary.epoch)
          timer = setTimeout(() => void connect(), Math.min(30000, 1000 * 2 ** Math.min(attempt++, 5)));
      }
    };
    if (session?.user) void connect();
    const focused = () => {
      clearTimeout(batch); batch = undefined;
      void deliver();
    };
    const fallback = setInterval(focused, 30000);
    window.addEventListener('focus', focused);
    document.addEventListener('visibilitychange', changed);
    return () => { controller.abort(); clearTimeout(timer); clearTimeout(batch); clearInterval(fallback); window.removeEventListener('focus', focused); document.removeEventListener('visibilitychange', changed); };
  }, [projectId, session?.user?.identity, session?.epoch]);
}
