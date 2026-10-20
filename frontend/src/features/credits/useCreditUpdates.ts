import { useContext, useEffect, useRef, useState } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { shareInvalidations } from '../../api/invalidation-stream';
import { sessionAudience } from '../../api/session-audience';
import { translate } from '../../contexts/translate';

// One authenticated stream per mounted scope; GET rereads remain authoritative.
export function useCreditUpdates(enabled: boolean, target: string | null, onChange?: () => void) {
  const session = useContext(SessionOptionalContext),
    callback = useRef(onChange);
  callback.current = onChange;
  const [revision, setRevision] = useState(0),
    [error, setError] = useState('');
  useEffect(() => {
    setError('');
    if (!enabled || !session?.user) return;
    const controller = new AbortController(),
      epoch = sessionBoundary.epoch;
    let stopped = false,
      attempt = 0,
      timer: ReturnType<typeof setTimeout>,
      batch: ReturnType<typeof setTimeout> | undefined;
    const changed = () => {
      if (stopped || epoch !== sessionBoundary.epoch) return;
      attempt = 0;
      setError('');
      if (batch !== undefined) return;
      batch = setTimeout(() => {
        batch = undefined;
        if (stopped || epoch !== sessionBoundary.epoch) return;
        setRevision((value) => value + 1);
        callback.current?.();
      }, 100);
    };
    const connect = async () => {
      try {
        await shareInvalidations(
          JSON.stringify([sessionAudience(), session.user!.identity]),
          'credits:' + JSON.stringify(target),
          (change, signal) => session.service.watchCredits(target, change, signal),
          changed,
          controller.signal,
        );
      } catch {
        if (stopped || epoch !== sessionBoundary.epoch || controller.signal.aborted) return;
        setError(translate('积分实时同步暂不可用，正在重连。'));
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
  }, [enabled, target, session?.service, session?.user?.identity, session?.epoch]);
  return { revision, error };
}
