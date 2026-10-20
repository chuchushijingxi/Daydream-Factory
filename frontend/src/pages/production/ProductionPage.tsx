import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Link, useLocation, useSearchParams } from 'react-router-dom';
import { BookOpen, ChevronRight, Clapperboard, Layers, Plus, PanelRight } from 'lucide-react';
import { useWorkspace } from '../../state/WorkspaceContext';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { ReferenceDialog } from './ReferenceDialog';
import { ShotStrip } from './ShotStrip';
import { accessMessage, indexReady, sourceSlice } from '../../domain/storyboard';
import type { EpisodeView, SourceRange, StoryboardOperation, StoryScene } from '../../api/storyboard-types';
import { SceneDialog } from './SceneDialog';
import { ShotWorkspace } from './ShotWorkspace';
import { useProduction } from './useProduction';
import { useCatalogText } from '../../contexts/useCatalogText';
import { currentEpisodeRun, EpisodeRunFeedback } from './EpisodeRunFeedback';

export function ProductionPage() {
  const { c } = useCatalogText();
  const {
    currentProject: p,
    status,
    storyboardIndex: index,
    storyboardError,
    refreshStoryboardIndex,
    source: fullSource,
  } = useWorkspace();
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const scroll = useRef<HTMLDivElement>(null),
    positions = useRef(new Map<string, number>());
  const episodeId = params.get('episodeId') || '';
  const controller = useProduction(episodeId, location.state?.episodeStart as EpisodeView | undefined),
    { view, source, error, busy, loading, editable, hasDrafts } = controller;
  const [showContext, setShowContext] = useState(false),
    [picker, setPicker] = useState(false),
    [showScenes, setShowScenes] = useState(false),
    [sceneDialog, setSceneDialog] = useState<{
      sourceRanges: SourceRange[];
      revision: number;
    } | null>(null),
    [confirm, setConfirm] = useState<{
      label: string;
      detail: string;
      operations: StoryboardOperation[];
      revision: number;
    } | null>(null);
  useEffect(() => {
    setSceneDialog(null);
    setConfirm(null);
    setPicker(false);
  }, [p?.id, episodeId]);
  useLayoutEffect(() => {
    const node = scroll.current,
      key = (p?.id || '') + ':' + episodeId + ':' + (params.get('shotId') || 'first');
    if (node) node.scrollTop = positions.current.get(key) || 0;
    return () => {
      if (node) positions.current.set(key, node.scrollTop);
    };
  }, [p?.id, episodeId, params.get('shotId')]);
  if (!p) return null;
  if (!status)
    return (
      <div className="sb-unavailable" role="status">
        {c('正在读取服务能力…')}
      </div>
    );
  if (!status.capabilities.storyboard)
    return (
      <div className="sb-unavailable">
        <Clapperboard />
        <h2>{c('分集制作尚未接通')}</h2>
        <p>{c('当前服务未提供分集与分镜能力。已有资产和原文可继续查看。')}</p>
        <Button asChild variant="outline">
          <Link to={`/playlet/${p.id}/characters`}>{c('返回资产库')}</Link>
        </Button>
      </div>
    );
  if (index && !index.index && !index.latestIndexRun)
    return (
      <div className="sb-unavailable">
        <BookOpen />
        <h2>{c('这个项目没有分集目录')}</h2>
        <p>{c('该项目由旧流程创建，保留已有剧本和资产。请新建项目，上传后会自动识别完整分集。')}</p>
        <div className="sb-actions">
          <Button asChild variant="outline">
            <Link to={`/playlet/${p.id}/characters`}>{c('查看已有资产')}</Link>
          </Button>
          <Button asChild>
            <Link to="/projects">{c('新建项目')}</Link>
          </Button>
        </div>
      </div>
    );
  const episodeRun = currentEpisodeRun(view),
    generating = episodeRun?.status === 'running';
  // 本集仍在生成或复核时，已保存内容只是中间结果，只展示进度，完成后再呈现场景与分镜。
  const scenes =
    (!generating &&
      view?.episode.sceneIds
      .map((id) => view.scenes.find((s) => s.id === id))
        .filter((s): s is StoryScene => !!s)) ||
    [];
  const scene = scenes.find((s) => s.id === params.get('sceneId')) || scenes[0],
    shots =
      scene?.shotIds.map((id) => view?.shots.find((s) => s.id === id)).filter((s) => !!s) || [];
  const shot = shots.find((s) => s.id === params.get('shotId')) || shots[0];
  const ready = indexReady(index, p.source.version);
  const showingRunFeedback =
    !!episodeRun && ['running', 'failed', 'cancelled'].includes(episodeRun.status);
  const browse = (patch: Record<string, string>) => {
    const next = new URLSearchParams(params);
    Object.entries(patch).forEach(([k, v]) => {
      if (v) next.set(k, v);
      else next.delete(k);
    });
    setParams(next, { replace: true });
  };
  const access = accessMessage(view?.access.reason, c);
  const startReason = view?.activeRun
      ? view.activeRun.storyboardScope?.episodeId === episodeId
        ? c('任务进行中，只读浏览。')
        : c('项目有其他任务正在运行，只读浏览。')
      : status.readOnly
        ? c('当前服务只读。')
        : access
          ? c(access)
          : '';
  let currentSource: string[] = [];
  try {
    if (shot && fullSource && fullSource.version === view?.sourceVersion)
      currentSource = sourceSlice(fullSource, shot.sourceRanges);
  } catch (e) {
    currentSource = [(e as Error).message];
  }
  const selected = shot ? controller.draft(shot).value.selectedAssets : [];
  const insertShot = (afterShotId?: string) => {
    if (!scene || !view) return;
    const before = new Set(scene.shotIds);
    void controller
      .changes([
        {
          op: 'createShot',
          sceneId: scene.id,
          ...(afterShotId ? { afterShotId } : {}),
          value: {
            sourceRanges: scene.sourceRanges,
            action: '',
            dialogues: [],
            camera: { size: '', angle: '', movement: '' },
            durationSeconds: null,
            designNotes: '',
            selectedAssets: scene.locationAssetId ? [{ assetId: scene.locationAssetId }] : [],
            videoPrompt: null,
          },
        },
      ])
      .then(async (ok) => {
        if (!ok) return;
        const latest = await controller.read().catch(() => undefined),
          created = latest?.scenes
            .find((s) => s.id === scene.id)
            ?.shotIds.find((id) => !before.has(id));
        if (created) browse({ shotId: created });
      });
  };
  return (
    <div className="sb-page sb-production">
      {startReason && !generating ? <p className="sb-notice">{startReason}</p> : null}
      {error || storyboardError ? (
        <div role="alert" className="sb-error">
          {error || storyboardError}
          <Button
            variant="link"
            size="sm"
            disabled={loading}
            onClick={() => {
              void controller.read().catch(() => {});
              void refreshStoryboardIndex().catch(() => {});
            }}
          >
            {c('重新读取')}
          </Button>
        </div>
      ) : null}
      {!ready ? (
        <p className="sb-notice">
          {c('分集解析尚未完成，候选可查看；请在')}
          <Link to={`/playlet/${p.id}/outline`}>{c('剧本解析')}</Link>
          {c('中查看真实运行。')}
        </p>
      ) : null}
      <div className="sb-mobile-tools">
        <Button
          variant="outline"
          size="sm"
          aria-expanded={showScenes}
          onClick={() => setShowScenes((v) => !v)}
        >
          <Layers />
          {c('场景')}
        </Button>
      </div>
      <div className="sb-layout">
        <aside className="sb-scenes" data-open={showScenes} aria-label={c('场景列表')}>
          <div className="sb-nav-heading">
            <h2>{c('场景')}</h2>
            <Button variant="ghost" size="sm" className="sb-reference-toggle"
              aria-label={c('参考资料')} title={c('参考资料')} aria-haspopup="dialog"
              aria-expanded={showContext} onClick={() => setShowContext(v => !v)}>
              <PanelRight />
            </Button>
          </div>
          <nav className="sb-scene-list" aria-label={c('本集场景')}>
            {scenes.map((item, index) => (
              <button
                type="button"
                key={item.id}
                className="sb-scene-item"
                aria-current={item.id === scene?.id ? 'true' : undefined}
                onClick={() => {
                  browse({ sceneId: item.id, shotId: '' });
                  setShowScenes(false);
                }}
              >
                <span className="sb-scene-number">{String(index + 1).padStart(2, '0')}</span>
                <span>
                  <strong>{item.title}</strong>
                  {item.summary ? <p className="sb-scene-summary" title={item.summary}>{item.summary}</p> : null}
                </span>
                <ChevronRight aria-hidden="true" />
              </button>
            ))}
            {generating ? (
              <div className="sb-scene-pending" role="status">
                {[0, 1, 2].map((i) => (
                  <span key={i} className="sb-scene-skeleton" aria-hidden>
                    <span />
                    <span />
                    <span />
                  </span>
                ))}
                <p className="sb-muted">{c('本集生成完成后显示场景')}</p>
              </div>
            ) : !scenes.length ? (
              <p className="sb-muted">
                {loading ? c('读取场景…') : c('本集暂无已保存场景')}
              </p>
            ) : null}
          </nav>
          <Button
            className="sb-add-scene"
            aria-label={c('新增场景')}
            title={c('新增场景')}
            variant="ghost"
            size="sm"
            disabled={!editable || hasDrafts || !ready}
            onClick={() => {
              if (view)
                setSceneDialog({
                  sourceRanges: view.episode.sourceRanges,
                  revision: view.revision,
                });
            }}
          >
            <Plus />
            {c('新增场景')}
          </Button>
        </aside>
        <main className="sb-main">
          <div ref={scroll} className="sb-main-scroll">
            <EpisodeRunFeedback
              view={view} expanded={!scene} source={source} assets={p.assets}
              onCancel={view?.activeRun ? () => void controller.cancel() : undefined}
              cancelDisabled={status.readOnly || busy}
            />
            {scene ? (
              <>
                {shot ? (
                  <ShotWorkspace
                    key={p.id + ':' + shot.id}
                    picker={picker}
                    onPickerChange={setPicker}
                    shot={shot}
                    number={shots.indexOf(shot) + 1}
                    controller={controller}
                  />
                ) : (
                  <div className="sb-empty">
                    <h2>{c('此场景暂无镜头')}</h2>
                    <p>{c('可返回分集页拆解，或新增一个连续镜头。')}</p>
                  </div>
                )}
              </>
            ) : !showingRunFeedback ? (
              <div className="sb-empty">
                <Clapperboard />
                <h2>
                  {loading
                    ? c('读取本集内容…')
                    : episodeRun
                      ? c('本集暂无已保存场景')
                      : c('本集尚未拆解')}
                </h2>
                <p>{c('从分集页选择这一集，开始拆解场景、分镜和视频提示词。')}</p>
                <Button asChild variant="outline"><Link to={`/playlet/${p.id}/episodes`}>{c('前往分集页')}</Link></Button>
              </div>
            ) : null}
          </div>
          {generating ? (
            <section className="sb-filmstrip sb-strip-placeholder" aria-label={c('本场景分镜')}>
              <div className="sb-strip-track">
                {Array.from({ length: 8 }, (_, i) => (
                  <span key={i} className="sb-strip-skeleton" aria-hidden />
                ))}
              </div>
            </section>
          ) : view ? (
            <ShotStrip
              key={scene?.id || 'empty'}
              shots={shots}
              currentId={shot?.id}
              revision={view.revision}
              disabled={!scene || !editable || hasDrafts || !!controller.saving}
              emptyMessage={scene ? undefined : c('暂无分镜，先新增场景。')}
              onSelect={(id) => {
                browse({ shotId: id });
                setShowScenes(false);
              }}
              onReorder={(shotIds, revision) =>
                scene ? controller.changes(
                  [{ op: 'setShotOrder', sceneId: scene.id, shotIds }],
                  revision,
                ) : Promise.resolve(false)
              }
              onInsert={insertShot}
              onDelete={(target) =>
                setConfirm({
                  label: c('删除镜头'),
                  revision: view.revision,
                  detail: c('删除后无法恢复，确定删除这个镜头？'),
                  operations: [{ op: 'deleteShot', shotId: target.id }],
                })
              }
            />
          ) : null}
        </main>
      </div>
      <ReferenceDialog
        open={showContext}
        onOpenChange={setShowContext}
        project={p}
        selected={selected}
        currentSource={currentSource}
        source={source}
      />

      {sceneDialog ? (
        <SceneDialog
          disabled={busy || !view?.access.editable || status.readOnly || !!view.activeRun}
          onClose={() => setSceneDialog(null)}
          onSave={(value) =>
            controller.changes(
              [{ op: 'createScene', episodeId, value: {
                ...value, sourceRanges: sceneDialog.sourceRanges, time: '', locationAssetId: null,
              } }],
              sceneDialog.revision,
            )
          }
        />
      ) : null}
      <Dialog
        open={!!confirm}
        onOpenChange={(v) => {
          if (!v && !busy) setConfirm(null);
        }}
      >
        <DialogContent variant="confirm" className="sb-confirm-dialog" closeDisabled={busy}>
          <DialogTitle>{confirm?.label}</DialogTitle>
          <DialogDescription>{confirm?.detail}</DialogDescription>
          {error ? (
            <p role="alert" className="jm-inline-error">
              {error}
            </p>
          ) : null}
          <footer className="sb-actions">
            <Button variant="outline" disabled={busy} onClick={() => setConfirm(null)}>
              {c('取消')}
            </Button>
            <Button
              variant="destructive"
              disabled={busy || hasDrafts || !!view?.activeRun || status.readOnly}
              busy={busy}
              onClick={() => {
                if (confirm?.operations)
                  void controller.changes(confirm.operations, confirm.revision).then((ok) => {
                    if (ok) setConfirm(null);
                  });
              }}
            >
              {c('删除镜头')}
            </Button>
          </footer>
        </DialogContent>
      </Dialog>
    </div>
  );
}
