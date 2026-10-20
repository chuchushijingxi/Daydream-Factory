import { useState } from 'react';
import type { BreakdownMode, Episode, StoryboardIndexView } from '../../api/storyboard-types';
import type { ModelOption } from '../../api/types';
import { billingApi } from '../../api/billing-client';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { ChargeNotice, QuoteAmount, useChargeQuote } from '../../features/billing/useChargeQuote';
import { breakdownModes } from './breakdown-modes';
import { useCatalogText } from '../../contexts/useCatalogText';

export function BreakdownDialog({
  episode,
  index,
  videoModelId,
  videoModel,
  busy,
  canStart,
  priceSequence,
  error,
  onClose,
  onStart,
}: {
  episode: Episode;
  index: StoryboardIndexView;
  videoModelId?: string;
  videoModel?: ModelOption;
  busy: boolean;
  canStart: boolean;
  priceSequence: number;
  error: string;
  onClose: () => void;
  onStart: (mode: BreakdownMode, basis?: string) => void;
}) {
  const { c } = useCatalogText();
  const [mode, setMode] = useState<BreakdownMode>('general');
  const quote = useChargeQuote(
    () =>
      billingApi.project(index.projectId, {
        revision: index.revision,
        operation: 'breakdown',
        episodeId: episode.id,
        breakdownMode: mode,
      }),
    JSON.stringify([
      index.projectId,
      index.revision,
      episode.id,
      mode,
      priceSequence,
      videoModelId,
      videoModel?.mediaProfile?.durations,
      videoModel?.mediaProfile?.retired,
    ]),
    canStart,
  );
  const durations =
    videoModel?.mediaProfile?.durations?.filter((n) => Number.isFinite(n) && n > 0) || [];
  const longest = durations.length ? Math.max(...durations) : 0;
  const missingVideo = !!videoModelId && (!longest || videoModel?.mediaProfile?.retired);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="sb-breakdown-dialog" closeDisabled={busy}>
        <DialogTitle>{c('拆解{title}', { title: episode.title })}</DialogTitle>
        <DialogDescription>{c('选择这集的表达侧重点。场景数和分镜数由内容决定。')}</DialogDescription>
        <fieldset className="sb-breakdown-modes" disabled={busy}>
          <legend className="sb-breakdown-legend">{c('拆解模式')}</legend>
          {breakdownModes.map((item) => (
            <label key={item.id} className="sb-breakdown-mode" data-selected={mode === item.id}>
              <input
                type="radio"
                name="breakdown-mode"
                value={item.id}
                checked={mode === item.id}
                onChange={() => setMode(item.id)}
              />
              <span className="sb-breakdown-mode-copy">
                <span className="sb-breakdown-mode-heading">
                  <strong>{c(item.label)}</strong>
                  {item.id === 'general' ? <small>{c('默认')}</small> : null}
                  <span>{c(item.description)}</span>
                </span>
                <span className="sb-breakdown-mode-result">{c(item.result)}</span>
              </span>
            </label>
          ))}
        </fieldset>
        {missingVideo ? (
          <p className="sb-error" role="alert">
            {c('模型不可用或缺少时长信息，请检查模型配置后再拆解。')}
          </p>
        ) : null}
        {error ? (
          <p className="sb-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="sb-breakdown-footer">
          <ChargeNotice quote={quote} />
          <Button
            disabled={!canStart || quote.blocked || missingVideo}
            busy={busy}
            onClick={() => onStart(mode, quote.basis)}
          >
            {c('开始拆解')} <QuoteAmount quote={quote} />
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
