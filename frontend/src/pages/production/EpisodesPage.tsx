import { useContext, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Clapperboard, Pencil, Play } from 'lucide-react';
import { storyboardApi } from '../../api/client';
import type { BreakdownMode, Episode, StoryboardIndexView } from '../../api/storyboard-types';
import { Button } from '../../components/ui/button';
import { Spinner } from '../../components/ui/spinner';
import { FreeBadge } from '../../components/ui/free-badge';
import { toast } from '../../components/ui/sonner';
import { accessMessage, indexLabel, indexReady } from '../../domain/storyboard';
import { BreakdownDialog } from './BreakdownDialog';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useWorkspace } from '../../state/WorkspaceContext';
import { draftChanged } from '../../state/useStoryboardAutosave';
import { ProjectWorkflowFooter } from '../../features/project/components/ProjectWorkflowFooter';
import { formatInteger } from '../credits/credit-format';
import { useCatalogText } from '../../contexts/useCatalogText';
import { workflowNotice } from '../../features/project/workflow-footer-state';

function episodeRun(index: StoryboardIndexView, episode: Episode) {
  const matches = (run: typeof index.activeRun) =>
    run?.contract === 'storyboard-agent-v1' &&
    run.sourceVersion === index.sourceVersion &&
    run.storyboardScope?.episodeId === episode.id;
  return matches(index.activeRun)
    ? index.activeRun
    : matches(episode.latestRun || null)
      ? episode.latestRun!
      : null;
}

function EpisodeCard({
  episode,
  index,
  canStart,
  starting,
  onStart,
}: {
  episode: Episode;
  index: StoryboardIndexView;
  canStart: boolean;
  starting: boolean;
  onStart: (episode: Episode) => void;
}) {
  const { c } = useCatalogText();
  const run = episodeRun(index, episode),
    running = run?.status === 'running',
    unknown = run?.errorCode === 'MODEL_RESULT_UNKNOWN',
    hasContent =
      (episode.sceneCount ?? episode.sceneIds.length) > 0 || (episode.shotCount ?? 0) > 0,
    retry = run?.status === 'failed' || run?.status === 'cancelled';
  const href = `/playlet/${index.projectId}/production?episodeId=${encodeURIComponent(episode.id)}`;
  const label = running
    ? c('拆解中')
    : unknown
      ? c('结果待核查')
      : run?.status === 'failed'
        ? c('拆解未完成')
        : run?.status === 'cancelled'
          ? c('已取消')
          : hasContent
            ? episode.pendingPromptCount
              ? c('提示词待补齐')
              : c('已拆解')
            : c('待拆解');
  const titleId = 'episode-title-' + episode.id;
  return (
    <article className="sb-episode-card" aria-labelledby={titleId} data-running={running}>
      <Link
        className="sb-episode-open"
        to={href}
        aria-label={c('进入分集：{title}', { title: episode.title })}
      />
      <div className="sb-episode-cover" aria-hidden="true">
        <Play />
      </div>
      <div className="sb-episode-body">
        <span
          className="sb-episode-state"
          data-tone={
            running
              ? 'running'
              : unknown ||
                  run?.status === 'cancelled' ||
                  (hasContent && !!episode.pendingPromptCount)
                ? 'warning'
                : run?.status === 'failed'
                  ? 'error'
                  : hasContent
                    ? episode.pendingPromptCount
                      ? 'warning'
                      : 'complete'
                    : 'pending'
          }
          role={running ? 'status' : undefined}
        >
          {running ? <Spinner /> : null}
          {label}
        </span>
        <h2 id={titleId}>{episode.title}</h2>
        <p className="sb-episode-meta">
          <span>{c('{count} 角色', { count: episode.characterCount ?? 0 })}</span>
          <span aria-hidden="true">·</span>
          <span>{c('{count} 场景', { count: episode.sceneCount ?? episode.sceneIds.length })}</span>
          <span aria-hidden="true">·</span>
          <span>{c('{count} 分镜', { count: episode.shotCount ?? 0 })}</span>
        </p>
        <div className="sb-episode-actions">
          {hasContent || running ? (
            <span className="sb-episode-enter" aria-hidden="true">
              {running ? <Clapperboard /> : <Pencil />}
              {running ? c('查看进度') : c('编辑')}
            </span>
          ) : unknown && run ? (
            <span className="sb-episode-enter">{c('结果待确认')}</span>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              disabled={!canStart}
              busy={starting}
              onClick={() => onStart(episode)}
            >
              <Clapperboard />
              {starting ? c('正在启动') : retry ? c('重新拆解') : c('拆解分镜')}
            </Button>
          )}
        </div>
      </div>
    </article>
  );
}

export function EpisodesPage() {
  const { c } = useCatalogText();
  const {
    currentProject: p,
    status,
    storyboardIndex: index,
    storyboardError,
    refreshStoryboardIndex,
    storyboardDrafts,
    storyboardSaving,
    models,
  } = useWorkspace();
  const session = useContext(SessionOptionalContext),
    navigate = useNavigate();
  const [starting, setStarting] = useState(''),
    [error, setError] = useState(''),
    [selectedEpisode, setSelectedEpisode] = useState<Episode | null>(null),
    [uncertain, setUncertain] = useState(''),
    [priceSequence, setPriceSequence] = useState(0);
  const submitting = useRef(false),
    alive = useRef(true),
    projectId = useRef(p?.id);
  projectId.current = p?.id;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    setError('');
    setUncertain('');
    setSelectedEpisode(null);
  }, [p?.id, p?.source.version]);
  if (!p) return null;
  const ready = indexReady(index, p.source.version),
    episodes = index?.index?.episodes || [];
  const analysis = models.analysis.find((model) => model.id === p.settings?.llmModel);
  // 限免只展示活动标签，不拼出“0 积分（限免）”，避免被理解为真实扣费。
  const freeAnalysis =
    analysis?.free === true && !!analysis.creditPrice && analysis.billingUnit === 'characters-100';
  const priceHint = freeAnalysis
    ? ''
    : analysis?.creditPrice && analysis.billingUnit === 'characters-100'
      ? c('分镜脚本生成100字符消耗 {price} 积分，以实际生成为准', {
          price: formatInteger(String(analysis.creditPrice.unitPrice)),
        })
      : c('分镜脚本生成积分价格待确认');
  const active = index?.activeRun || p.runs.find((run) => run.status === 'running');
  const access = accessMessage(index?.access.reason, c);
  const reason = uncertain
    ? c('拆解请求的受理结果尚未确认，请重新读取，不会重复提交。')
    : !ready
        ? ''
        : active
          ? c('项目有任务正在运行，可查看进度和已有内容。')
          : access
            ? c(access)
            : '';
  const canStart =
    ready &&
    !!status?.capabilities.storyboard &&
    !status.readOnly &&
    !!index?.access.canStart &&
    !active &&
    !starting &&
    !uncertain &&
    !storyboardSaving;
  const hasDraft = (episodeId: string) =>
    [...storyboardDrafts.current.values()].some(
      (draft) => draft.projectId === p.id && draft.episodeId === episodeId && draftChanged(draft),
    );
  let footer = workflowNotice(c('选择分集卡片，查看或拆解分镜'), 'success');
  if (!status) footer = workflowNotice(c('正在读取分集目录'), 'info');
  else if (!status.capabilities.storyboard)
    footer = workflowNotice(c('分集制作暂不可用，请检查服务配置'), 'warning');
  else if (uncertain || index?.access.reason === 'MODEL_RESULT_UNKNOWN')
    footer = workflowNotice(c('拆解结果待确认，请重新读取'), 'warning');
  else if (storyboardError)
    footer = workflowNotice(c('分集目录读取失败，请重新读取'), 'danger');
  else if (!index) footer = workflowNotice(c('正在读取分集目录'), 'info');
  else if (!ready)
    footer = workflowNotice(c('分集目录未就绪，请返回剧本完成解析'), 'warning');
  else if (starting) footer = workflowNotice(c('正在启动分镜拆解，请稍候'), 'info');
  else if (active) footer = workflowNotice(c('项目任务进行中，可查看已有分集'), 'info');
  else if (storyboardSaving)
    footer = workflowNotice(c('正在自动保存分镜，请稍候'), 'info');
  else if (episodes.some((episode) => hasDraft(episode.id)))
    footer = workflowNotice(c('部分分集有未保存草稿，请先检查'), 'warning');
  else if (status.readOnly) footer = workflowNotice(c('只读模式，可选择分集查看'));
  else if (!p.settings?.llmModel)
    footer = workflowNotice(c('请先选择分析模型，再拆解分镜'), 'warning');
  else if (!index.access.canStart)
    footer = workflowNotice(
      index.access.reason === 'ASSETS_NOT_READY' ||
        index.access.reason === 'VISUAL_PROMPTS_REQUIRED'
        ? c('资产内容未就绪，请返回上一步完善')
        : c('暂不能拆解，可查看已有分集和过程'),
      'warning',
    );

  async function start(episode: Episode, mode: BreakdownMode, billingBasis?: string) {
    if (
      !p ||
      !index ||
      !canStart ||
      hasDraft(episode.id) ||
      submitting.current ||
      (session?.user && !billingBasis)
    )
      return;
    const id = p.id;
    submitting.current = true;
    setStarting(episode.id);
    setError('');
    try {
      // 报价和启动共用目录的冻结 revision；并发变化由现有 CAS 拒绝，不重放任务。
      const accepted = await storyboardApi.start(id, episode.id, {
        operation: 'breakdown',
        revision: index.revision,
        breakdownMode: mode,
        ...(billingBasis ? { billingBasis } : {}),
      });
      if (!alive.current || projectId.current !== id) return;
      if (accepted.sourceVersion !== p.source.version)
        throw new Error(c('拆解回执与当前原文版本不一致，请回读核查。'));
      // 先刷新共用目录，立即返回分集页时也能看到已受理任务，不能再点一次启动。
      try {
        await refreshStoryboardIndex();
      } catch (readError) {
        toast.error(
          c('拆解已受理，但分集目录回读失败：{reason}', {
            reason: (readError as Error).message,
          }),
        );
      }
      if (!alive.current || projectId.current !== id) return;
      setSelectedEpisode(null);
      navigate(`/playlet/${id}/production?episodeId=${encodeURIComponent(episode.id)}`, {
        state: { episodeStart: accepted },
      });
    } catch (e) {
      if (!alive.current || projectId.current !== id) return;
      const failure = e as Error & { code?: string; status?: number };
      setError(failure.message);
      if (failure.code === 'BILLING_BASIS_CHANGED') setPriceSequence((value) => value + 1);
      if (
        failure.code === 'MODEL_RESULT_UNKNOWN' ||
        !failure.status ||
        failure.status >= 500 ||
        failure.code === 'PROGRAM_FAILED'
      )
        setUncertain(episode.id);
      try {
        await refreshStoryboardIndex();
      } catch (readError) {
        setError(
          c('{message}；回读失败：{reason}', {
            message: failure.message,
            reason: (readError as Error).message,
          }),
        );
      }
    } finally {
      submitting.current = false;
      if (alive.current && projectId.current === id) setStarting('');
    }
  }
  return (
    <main className="sb-page sb-production sb-episodes-page" aria-label={c('分集视频')}>
      <div className="sb-episodes-content">
        <header className="sb-episodes-heading">
          <div>
            <h1>{c('分集视频')}</h1>
            <p className="sb-episodes-subtitle">
              <strong>
                {ready && index?.episodeCount != null
                  ? c('共 {count} 集', { count: index.episodeCount })
                  : indexLabel(index, p.source.version, c)}
              </strong>
              <span className="sb-episodes-price">
                {freeAnalysis ? <FreeBadge /> : priceHint}
              </span>
            </p>
          </div>
        </header>
        {error || storyboardError ? (
          <div className="sb-episodes-notice" role="alert">
            {error || storyboardError}
            <Button
              variant="link"
              size="sm"
              onClick={() => void refreshStoryboardIndex().catch((e) => setError(e.message))}
            >
              {c('重新读取')}
            </Button>
          </div>
        ) : null}
        {reason ? (
          <p className="sb-episodes-notice" role="status">
            {reason}
          </p>
        ) : null}
        {!status || (!index && !storyboardError && status.capabilities.storyboard) ? (
          <div className="sb-episodes-empty" role="status">
            <Spinner />
            {c('正在读取分集目录…')}
          </div>
        ) : !status.capabilities.storyboard ? (
          <div className="sb-episodes-empty">
            <Clapperboard />
            <h2>{c('分集制作尚未接通')}</h2>
            <p>{c('已有剧本和资产可继续查看。')}</p>
          </div>
        ) : !ready ? (
          <div className="sb-episodes-empty">
            <Clapperboard />
            <h2>
              {!index
                ? c('无法读取分集目录')
                : index.index || index.latestIndexRun
                  ? c('分集目录尚未就绪')
                  : c('这个项目没有分集目录')}
            </h2>
            <p>
              {!index
                ? c('请重新读取分集目录，已有项目内容会保留。')
                : index.index || index.latestIndexRun
                  ? c('请先完成当前剧本的分集解析，再选择单集拆解。')
                  : c('保留已有剧本和资产；新项目上传剧本后会自动识别分集。')}
            </p>
            <Button asChild variant="outline">
              <Link to={`/playlet/${p.id}/outline`}>{c('查看剧本解析')}</Link>
            </Button>
          </div>
        ) : index ? (
          <div className="sb-episodes-grid">
            {episodes.map((episode) => (
              <EpisodeCard
                key={episode.id}
                episode={episode}
                index={index}
                canStart={canStart && !hasDraft(episode.id)}
                starting={starting === episode.id}
                onStart={(target) => {
                  setError('');
                  setSelectedEpisode(target);
                }}
              />
            ))}
          </div>
        ) : null}
      </div>
      {selectedEpisode && index ? (
        <BreakdownDialog
          key={p.id + ':' + selectedEpisode.id}
          episode={selectedEpisode}
          index={index}
          videoModelId={p.settings?.videoModel}
          videoModel={models.video.find((model) => model.id === p.settings?.videoModel)}
          busy={!!starting}
          canStart={canStart && !hasDraft(selectedEpisode.id)}
          priceSequence={priceSequence}
          error={error || reason}
          onClose={() => setSelectedEpisode(null)}
          onStart={(mode, basis) => void start(selectedEpisode, mode, basis)}
        />
      ) : null}
      <ProjectWorkflowFooter
        tip={c(footer.tip, footer.values)}
        tipTone={footer.tone}
        onPrevious={() => navigate(`/playlet/${p.id}/characters`)}
      />
    </main>
  );
}
