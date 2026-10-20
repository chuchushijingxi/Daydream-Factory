import { downloadProtected } from '../../api/download';
import { toast } from '../../components/ui/sonner';
import { ChargeNotice } from '../../features/billing/useChargeQuote';
import { Fragment, useEffect, useMemo, useRef, useState } from 'react';
import { BookOpen, Download } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useWorkspace } from '../../state/WorkspaceContext';
import { useAssetPreparation } from '../../features/project/useAssetPreparation';
import { ProjectWorkflowFooter } from '../../features/project/components/ProjectWorkflowFooter';
import { Button } from '../../components/ui/button';
import { ScriptParseProgress } from './ScriptParseProgress';
import { scriptParsing } from '../../domain/script-parsing';
import { indexLabel, indexReady } from '../../domain/storyboard';
import { scriptFooterState } from '../../features/project/workflow-footer-state';

export function ScriptContentPage() {
  const { c } = useCatalogText();
  const {
    currentProject: playlet,
    source,
    status,
    storyboardIndex,
    storyboardError,
    refreshStoryboardIndex,
  } = useWorkspace();
  const task = useAssetPreparation(),
    navigate = useNavigate();
  const reader = useRef<HTMLDivElement>(null),
    anchors = useRef(new Map<string, HTMLSpanElement>());
  const [activeEpisode, setActiveEpisode] = useState('');
  useEffect(() => setActiveEpisode(''), [playlet?.id]);
  const episodes =
    source?.version === playlet?.source.version && indexReady(storyboardIndex, source?.version)
      ? storyboardIndex!.index!.episodes
      : [];
  // 只在服务端给出的 Unicode 来源起点插入阅读锚点，原文及集外材料全部保留。
  const paragraphs = useMemo(
    () =>
      source?.spans.map((span) => {
        const chars = Array.from(span.text);
        const starts = episodes
          .flatMap((episode) => {
            const point = episode.sourceRanges[0]?.start;
            return point?.spanId === span.id && point.offset >= 0 && point.offset <= chars.length
              ? [{ id: episode.id, offset: point.offset }]
              : [];
          })
          .sort((a, b) => a.offset - b.offset);
        let offset = 0;
        return (
          <Fragment key={span.id}>
            {starts.map((start) => {
              const text = chars.slice(offset, start.offset).join('');
              offset = start.offset;
              return (
                <Fragment key={start.id}>
                  {text}
                  <span
                    className="editor-script-anchor"
                    ref={(node) => {
                      if (node) anchors.current.set(start.id, node);
                      else anchors.current.delete(start.id);
                    }}
                    data-episode-id={start.id}
                  />
                </Fragment>
              );
            })}
            {chars.slice(offset).join('')}
          </Fragment>
        );
      }) || [],
    [source, storyboardIndex?.index],
  );
  if (!playlet) return null;
  const progress = scriptParsing(playlet, storyboardIndex, !!status?.capabilities.storyboard);
  const reading =
    !status || (!!status.capabilities.storyboard && !storyboardIndex && !storyboardError);
  const footer = scriptFooterState(
    task,
    progress,
    reading,
    !storyboardIndex ? storyboardError : '',
  );
  const selectedId = activeEpisode || episodes[0]?.id || '';
  const jump = (id: string) => {
    const host = reader.current,
      target = anchors.current.get(id);
    if (!host || !target) return;
    setActiveEpisode(id);
    host.scrollTo({
      top:
        host.scrollTop + target.getBoundingClientRect().top - host.getBoundingClientRect().top - 24,
      behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
    });
  };
  return (
    <div className="editor-script-page">
      {task.error || storyboardError || (progress.stopped && progress.run?.error) ? (
        <p role="alert" className="script-parse-inline-alert">
          {task.error || storyboardError || progress.run?.error}
          {storyboardError ? (
            <Button
              variant="link"
              size="sm"
              onClick={() => void refreshStoryboardIndex().catch(() => {})}
            >
              {c('回读解析结果')}
            </Button>
          ) : null}
          {task.readback ? (
            <Button
              variant="link"
              size="sm"
              disabled={task.sending}
              onClick={() => void task.reload()}
            >
              {c('读取最新版本')}
            </Button>
          ) : null}
        </p>
      ) : null}
      {!reading &&
      !progress.complete &&
      !progress.active &&
      (task.selectionMissing || task.modelMissing) ? (
        <p className="script-parse-inline-alert">
          {c(
            task.selectionMissing
              ? '请先选择项目的分析模型。'
              : '暂无可用分析模型，请在模型配置中启用。',
          )}
        </p>
      ) : null}
      <div className="editor-reading-layout jm-outline-layout">
        <nav className="editor-script-outline" aria-label={c('分集目录')}>
          <div className="editor-outline-heading">
            <h2>{c('分集目录')}</h2>
            {episodes.length ? <small>{indexLabel(storyboardIndex, source?.version, c)}</small> : null}
          </div>
          {episodes.length ? (
            episodes.map((episode, i) => (
              <button
                type="button"
                key={episode.id}
                aria-label={episode.title}
                aria-current={selectedId === episode.id ? 'location' : undefined}
                onClick={() => jump(episode.id)}
              >
                <span className="editor-outline-number" aria-hidden>
                  {String(i + 1).padStart(2, '0')}
                </span>
                <span>{episode.title}</span>
              </button>
            ))
          ) : (
            <>
              <button
                type="button"
                aria-current="location"
                onClick={() => reader.current?.scrollTo({ top: 0, behavior: 'auto' })}
              >
                <BookOpen aria-hidden />
                <span>{c('完整剧本')}</span>
              </button>
              <p className="editor-outline-note">{c('当前按完整原文阅读')}</p>
            </>
          )}
        </nav>
        <section className="editor-script-document" aria-labelledby="script-content-title">
          <header className="editor-page-heading">
            <h2 id="script-content-title">{c('剧本')}</h2>
            <Button
              variant="ghost"
              size="sm"
              onClick={() =>
                void downloadProtected(
                  '/api/projects/' + encodeURIComponent(playlet.id) + '/source/file',
                  playlet.source.filename,
                ).catch((error) => toast.error(error.message))
              }
            >
              <Download aria-hidden />
              {c('下载原剧本')}
            </Button>
          </header>
          <div
            ref={reader}
            className="jm-script-body editor-script-reader"
            tabIndex={0}
            role="region"
            aria-label={c('完整剧本正文')}
            onScroll={() => {
              const top = reader.current!.getBoundingClientRect().top + 64;
              let current = episodes[0]?.id || '';
              for (const episode of episodes) {
                const anchor = anchors.current.get(episode.id);
                if (anchor && anchor.getBoundingClientRect().top <= top) current = episode.id;
              }
              if (current !== activeEpisode) setActiveEpisode(current);
            }}
          >
            <div className="editor-script-column">
              <pre>
                {paragraphs.map((paragraph, i) => (
                  <span className="editor-source-paragraph" key={source!.spans[i].id}>
                    {paragraph}
                    {i < paragraphs.length - 1 ? '\n' : ''}
                  </span>
                ))}
              </pre>
              {!source ? <p role="status">{c('正在读取完整剧本…')}</p> : null}
            </div>
          </div>
        </section>
      </div>
      <ProjectWorkflowFooter
        className="script-parse-dock"
        leftContent={
          <>
            <ScriptParseProgress task={task} notice={footer} />
            {!progress.active && !progress.complete ? <ChargeNotice quote={task.quote} /> : null}
          </>
        }
        nextBusy={task.sending}
        nextAmount={!progress.active && !progress.complete ? task.quote.value?.amount : undefined}
        onNext={() => {
          if (progress.complete) navigate(`/playlet/${playlet.id}/characters`);
          else void task.start();
        }}
        nextDisabled={footer.disabled}
      />
    </div>
  );
}
