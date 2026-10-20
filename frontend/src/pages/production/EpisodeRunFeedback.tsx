import { useEffect, useRef } from 'react';
import { Check, CircleAlert, CirclePause, Clapperboard, Timer, X } from 'lucide-react';
import { protectedResourceUrl } from '../../api/client';
import type { Asset } from '../../api/types';
import type { EpisodeSourceView, EpisodeView, StoryboardRun } from '../../api/storyboard-types';
import { Button } from '../../components/ui/button';
import { Spinner } from '../../components/ui/spinner';
import { toast } from '../../components/ui/sonner';
import { breakdownModes } from './breakdown-modes';
import { useCatalogText } from '../../contexts/useCatalogText';

// activeRun 属于整个项目；不能把其他集或上游任务当作本集正在生成。
export function currentEpisodeRun(view: EpisodeView | null): StoryboardRun | null {
  if (!view) return null;
  const matches = (run: StoryboardRun | null) =>
    run?.contract === 'storyboard-agent-v1' &&
    run.sourceVersion === view.sourceVersion &&
    run.storyboardScope?.episodeId === view.episode.id;
  return matches(view.activeRun) ? view.activeRun : matches(view.latestRun) ? view.latestRun : null;
}

// 只列出资产引用的原文段落落在本集原文内的对象，不推测模型会选哪些。
function episodeAssets(source: EpisodeSourceView | null | undefined, assets: Asset[]) {
  if (!source?.fragments.length) return [];
  const spans = new Set(source.fragments.map((fragment) => fragment.spanId));
  return assets.filter((asset) => asset.sourceIds.some((id) => spans.has(id)));
}

function assetThumbnail(asset: Asset) {
  return asset.image?.thumbnailUrl || asset.looks.find((look) => look.image)?.image?.thumbnailUrl || '';
}

export function EpisodeRunFeedback({
  view,
  expanded,
  source,
  assets = [],
  onCancel,
  cancelDisabled = false,
}: {
  view: EpisodeView | null;
  expanded: boolean;
  source?: EpisodeSourceView | null;
  assets?: Asset[];
  onCancel?: () => void;
  cancelDisabled?: boolean;
}) {
  const { c } = useCatalogText();
  const run = currentEpisodeRun(view);
  const previous = useRef<{
    projectId: string;
    episodeId: string;
    id: string;
    status: string;
  } | null>(null);
  useEffect(() => {
    const last = previous.current;
    if (
      view &&
      run &&
      last?.projectId === view.projectId &&
      last.episodeId === view.episode.id &&
      last.id === run.id &&
      last.status === 'running' &&
      run.status === 'completed'
    )
      toast.success(
        c('{title}生成完成，已保存 {scenes} 个场景、{shots} 个镜头。', {
          title: view.episode.title,
          scenes: view.scenes.length,
          shots: view.shots.length,
        }),
        { id: 'episode-complete-' + run.id },
      );
    previous.current =
      view && run
        ? { projectId: view.projectId, episodeId: view.episode.id, id: run.id, status: run.status }
        : null;
  }, [view?.projectId, view?.episode.id, run?.id, run?.status]);
  if (!view || !run || !['running', 'failed', 'cancelled'].includes(run.status)) return null;
  const running = run.status === 'running',
    reviewing = run.stage === 'storyboardReview';
  const promptsOnly = run.storyboardScope?.operation !== 'breakdown';
  const mode = breakdownModes.find((item) => item.id === run.storyboardScope?.breakdownMode);
  const constraint = run.storyboardScope?.videoConstraint;
  const title = !running
    ? c(
        run.status === 'cancelled' ? '{title}生成已取消' : '{title}生成未完成',
        { title: view.episode.title },
      )
    : reviewing
      ? c('正在独立复核{title}', { title: view.episode.title })
      : run.storyboardScope?.operation === 'fillPrompts'
        ? c('正在补齐{title}的视频提示词', { title: view.episode.title })
        : run.storyboardScope?.operation === 'regeneratePrompts'
          ? c('正在重生成{title}的视频提示词', { title: view.episode.title })
          : c('正在生成{title}', { title: view.episode.title });
  const description =
    run.errorCode === 'MODEL_RESULT_UNKNOWN'
      ? c('存在结果未明的请求，已保留原任务，不会自动重发。')
      : c('已保存的内容会保留，可继续查看；重新生成需要你明确启动。');
  const promptCount = view.shots.filter((shot) =>
    shot.videoPrompt?.parts.some((part) => part.type === 'asset' || part.text.trim()),
  ).length;
  const phases = ['storyboard', 'storyboardReview'].includes(run.stage)
    ? [
        {
          key: 'storyboard',
          label: promptsOnly ? c('编写与核对提示词') : c('生成与核对'),
          detail: promptsOnly
            ? c('保留原有镜头动作、对白与资产选择，编写并核对视频提示词')
            : c('拆解场景、编排连续镜头，选择资产并编写、核对视频提示词'),
          state: reviewing ? 'done' : running ? 'active' : 'stopped',
        },
        {
          key: 'review',
          label: c('独立复核'),
          detail: promptsOnly
            ? c('复核本次视频提示词，发现问题继续修正')
            : c('复核剧情、对白、镜头衔接和资产引用，发现问题继续修正'),
          state: reviewing ? (running ? 'active' : 'stopped') : 'waiting',
        },
      ]
    : [];
  const phaseStatus = (state: string) =>
    state === 'done'
      ? c('已完成')
      : state === 'active'
        ? c('进行中')
        : state === 'stopped'
          ? c(run.status === 'cancelled' ? '已取消' : '未完成')
          : c('待处理');
  const script = source?.fragments.map((fragment) => fragment.text.trim()).filter(Boolean) || [];
  const scriptLength = script.reduce((total, text) => total + Array.from(text).length, 0);
  const involved = episodeAssets(source, assets);
  const kinds = (
    [
      ['character', '角色'],
      ['location', '场景'],
      ['prop', '道具'],
    ] as const
  ).map(([kind, label]) => ({ kind, label, items: involved.filter((asset) => asset.kind === kind) }));
  const showScript = expanded && script.length > 0;
  return (
    <section
      className="sb-run-feedback"
      data-expanded={expanded}
      data-state={run.status}
      data-script={showScript}
      aria-label={c('本集生成进度')}
      aria-busy={running}
    >
      {showScript ? (
        <article className="sb-run-script" aria-label={c('本集原文')}>
          <header>
            <h3>{c('本集原文')}</h3>
            <span>{c('{count} 字', { count: scriptLength.toLocaleString() })}</span>
          </header>
          <div className="sb-run-script-text" tabIndex={0}>
            {script.map((text, i) => (
              <p key={i}>{text}</p>
            ))}
          </div>
        </article>
      ) : null}
      <div className="sb-run-body">
        <div className="sb-run-heading">
          {running ? null : (
            <span className="sb-run-icon" aria-hidden>
              {run.status === 'cancelled' ? <CirclePause /> : <CircleAlert />}
            </span>
          )}
          <div className="sb-run-copy" role="status" aria-live="polite">
            <h2>{title}</h2>
            {mode ? (
              <div className="sb-run-mode">
                <span>
                  <Clapperboard aria-hidden />
                  {c(mode.label)}
                </span>
                <span>
                  <Timer aria-hidden />
                  {constraint
                    ? c('每镜最长 {seconds} 秒', { seconds: constraint.maxDurationSeconds })
                    : c('时长按内容估算')}
                </span>
              </div>
            ) : null}
          </div>
        </div>
        {mode && expanded ? <p className="sb-run-mode-note">{c(mode.result)}</p> : null}
        {phases.length ? (
          <ol className="sb-run-phases" aria-label={c('本集生成阶段')}>
            {phases.map((phase, i) => (
              <li
                key={phase.key}
                data-state={phase.state}
                aria-current={phase.state === 'active' ? 'step' : undefined}
              >
                <span className="sb-run-step-icon" aria-hidden>
                  {phase.state === 'done' ? (
                    <Check />
                  ) : phase.state === 'active' ? (
                    <Spinner />
                  ) : (
                    i + 1
                  )}
                </span>
                <div>
                  <span className="sb-run-step-title">
                    <strong>{phase.label}</strong>
                    <small data-state={phase.state}>{phaseStatus(phase.state)}</small>
                  </span>
                  <p>{phase.detail}</p>
                </div>
              </li>
            ))}
          </ol>
        ) : null}
        {running ? null : <p className="sb-run-description">{description}</p>}
        {running ? null : (
          <p className="sb-run-results">
            {!view.scenes.length && !view.shots.length
              ? c('本次尚未保存场景或镜头。')
              : promptCount
                ? c('当前已保存 {scenes} 个场景 · {shots} 个镜头 · {prompts} 个镜头的视频提示词', {
                    scenes: view.scenes.length,
                    shots: view.shots.length,
                    prompts: promptCount,
                  })
                : c('当前已保存 {scenes} 个场景 · {shots} 个镜头', {
                    scenes: view.scenes.length,
                    shots: view.shots.length,
                  })}
          </p>
        )}
        {!running && (run.error || run.errorCode) ? (
          <p role="alert" className="sb-run-error">
            {run.error || run.errorCode}
          </p>
        ) : null}
        {expanded && involved.length ? (
          <div className="sb-run-assets">
            <h3>{c('原文中出现')}</h3>
            {kinds.map(({ kind, label, items }) =>
              items.length ? (
                <div key={kind} className="sb-run-asset-row" data-kind={kind}>
                  <span className="sb-run-asset-kind">
                    {c(label)}
                    <small>{items.length}</small>
                  </span>
                  <ul>
                    {items.slice(0, 6).map((asset) => {
                      const thumbnail = assetThumbnail(asset);
                      return (
                        <li key={asset.id}>
                          <span className="sb-mention" title={asset.name}>
                            {thumbnail ? (
                              <img src={protectedResourceUrl(thumbnail)} alt="" loading="lazy" />
                            ) : null}
                            <span className="sb-mention-label">{asset.name}</span>
                          </span>
                        </li>
                      );
                    })}
                    {items.length > 6 ? (
                      <li className="sb-run-asset-more">+{items.length - 6}</li>
                    ) : null}
                  </ul>
                </div>
              ) : null,
            )}
          </div>
        ) : null}
        {running && onCancel ? (
          <div className="sb-run-actions">
            <Button variant="outline" size="sm" disabled={cancelDisabled} onClick={onCancel}>
              <X aria-hidden />
              {c('取消生成')}
            </Button>
            <small>{c('已保存的内容会保留，已发出的请求可能仍计费')}</small>
          </div>
        ) : null}
      </div>
    </section>
  );
}
