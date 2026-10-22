import { useOptionalSpace } from '../../state/SpaceContext';
import { useContext, useEffect, useState } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import { sessionBoundary } from '../../api/session-boundary';
import { useRead } from '../../pages/operations/controls';

// Keep confirmed presentation during same-account refreshes. Transport, errors
// and late-response rejection remain in the shared reader/session boundary.
export function useCreditRead<T>(load: () => Promise<T>, dependencies: unknown[], enabled = true) {
  const space = useOptionalSpace();
  const session = useContext(SessionOptionalContext),
    identity = session?.user?.identity,
    service = session?.service,
    epoch =
      sessionBoundary.epoch +
      ':' +
      (space?.epoch ?? 0) +
      ':' +
      (space?.spaceId ?? '') +
      ':' +
      (space?.current?.role ?? '');
  const active = enabled && !!identity;
  const read = useRead(load, [...dependencies, identity, service, epoch], active);
  const [last, setLast] = useState<{
    identity: string;
    service: unknown;
    epoch: string;
    value: T;
  } | null>(null);
  useEffect(() => {
    if (active && identity && read.value !== null)
      setLast({ identity, service, epoch, value: read.value });
    else
      setLast((old) =>
        !active || old?.identity !== identity || old?.service !== service || old?.epoch !== epoch
          ? null
          : old,
      );
  }, [read.value, active, identity, service, epoch]);
  const previous =
    active && last?.identity === identity && last.service === service && last.epoch === epoch
      ? last.value
      : null;
  return { ...read, value: read.value ?? previous };
}
