import { useEffect, useRef, useState } from 'react';
import { storyboardApi } from '../api/client';
import type { ShotInput, StoryShot } from '../api/storyboard-types';
import type { ServiceStatus } from '../api/types';
import { shotInput, shotPatch, sameRef } from '../domain/storyboard';
import { translate } from '../contexts/translate';

export type ShotDraft = {
  projectId: string;
  episodeId: string;
  sourceVersion: number;
  revision: number;
  base: ShotInput;
  value: ShotInput;
  conflict: boolean;
  message: string;
};
export const draftChanged = (d: ShotDraft) => Object.keys(shotPatch(d.base, d.value)).length > 0;
const complete = (d: ShotDraft) =>
  !d.value.videoPrompt ||
  d.value.selectedAssets.every((ref) =>
    d.value.videoPrompt!.parts.some((part) => part.type === 'asset' && sameRef(part, ref)),
  );

// Lives with the workspace so switching episodes or pages cannot abandon a
// pending save. One queue serializes project revisions, including edits made
// while a previous request is in flight.
export function useStoryboardAutosave(
  status: ServiceStatus | null,
  refresh: (id: string) => Promise<unknown>,
  canWrite: (projectId: string) => boolean,
) {
  const drafts = useRef(new Map<string, ShotDraft>()),
    [version, update] = useState(0),
    [saving, setSaving] = useState('');
  const running = useRef(false),
    refreshRef = useRef(refresh),
    statusRef = useRef(status),
    writeRef = useRef(canWrite);
  refreshRef.current = refresh;
  statusRef.current = status;
  writeRef.current = canWrite;
  const changed = () => update((n) => n + 1);
  useEffect(() => {
    if (running.current || !status?.capabilities.storyboard || status.readOnly) return;
    const entry = [...drafts.current].find(
      ([, d]) => draftChanged(d) && !d.conflict && complete(d) && writeRef.current(d.projectId),
    );
    if (!entry) return;
    const timer = setTimeout(() => void persist(entry[0]), 600);
    return () => clearTimeout(timer);
  }, [version, status, saving]);
  async function persist(key: string) {
    const current = drafts.current.get(key);
    if (
      running.current ||
      !current ||
      current.conflict ||
      !draftChanged(current) ||
      !complete(current) ||
      !statusRef.current ||
      statusRef.current.readOnly ||
      !writeRef.current(current.projectId)
    )
      return;
    const submitted = structuredClone(current),
      patch = shotPatch(submitted.base, submitted.value);
    running.current = true;
    setSaving(key);
    try {
      const receipt = await storyboardApi.changes(submitted.projectId, submitted.revision, [
        { op: 'updateShot', shotId: key.slice(submitted.projectId.length + 1), patch },
      ]);
      const after = receipt.changes.find(
        (c) =>
          c.entityType === 'shot' &&
          c.id === key.slice(submitted.projectId.length + 1) &&
          c.after !== null,
      )?.after as StoryShot | undefined;
      if (!after || receipt.revision !== submitted.revision + 1)
        throw new Error(translate('自动保存回执不完整，结果未确认。'));
      const adopted = shotInput(after);
      if (
        (Object.keys(patch) as (keyof ShotInput)[]).some(
          (k) => JSON.stringify(adopted[k]) !== JSON.stringify(patch[k]),
        )
      )
        throw new Error(translate('自动保存回执与提交内容不一致，结果未确认。'));
      for (const [k, d] of drafts.current) {
        if (d.projectId === submitted.projectId && d.revision === submitted.revision && !d.conflict)
          drafts.current.set(k, { ...d, revision: receipt.revision });
      }
      const latest = drafts.current.get(key);
      if (latest) {
        const remaining = shotPatch(submitted.value, latest.value);
        if (!Object.keys(remaining).length)
          drafts.current.set(key, {
            ...latest,
            revision: receipt.revision,
            base: adopted,
            value: adopted,
            message: '',
          });
        else
          drafts.current.set(key, {
            ...latest,
            revision: receipt.revision,
            base: adopted,
            value: { ...adopted, ...remaining },
            message: '',
          });
      }
    } catch (e) {
      const latest = drafts.current.get(key) || submitted;
      drafts.current.set(key, {
        ...latest,
        conflict: true,
        message: translate('自动保存未确认：{reason}。内容已保留，请核对最新版本；不会自动重发。', {
          reason: (e as Error).message,
        }),
      });
    } finally {
      // A failed read does not turn a confirmed save into an uncertain write.
      try {
        await refreshRef.current(submitted.projectId);
      } catch {
        /* Workspace reports its read error. */
      }
      running.current = false;
      setSaving('');
      changed();
    }
  }
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => {
      if ([...drafts.current.values()].some(draftChanged)) {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, []);
  return { drafts, changed, saving, version };
}
