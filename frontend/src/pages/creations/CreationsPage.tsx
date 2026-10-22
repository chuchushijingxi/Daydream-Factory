import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Download, Image as ImageIcon, Video, Play, Search, X } from 'lucide-react';
import { mediaLibraryApi, type Creation } from '../../api/media-library';
import { protectedResourceUrl } from '../../api/client';
import { downloadProtected } from '../../api/download';
import { Button } from '../../components/ui/button';
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '../../components/ui/dialog';
import { Spinner } from '../../components/ui/spinner';
import { ViewTabs } from '../../components/ui/view-tabs';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useLanguage } from '../../contexts/LanguageContext';
import { useWorkspace } from '../../state/WorkspaceContext';
import { toast } from '../../components/ui/sonner';
import { useRead } from '../operations/controls';

const keyOf = (v: Creation) => v.taskId + ':' + v.outputId;
export function creationDay(value: string) {
  const d = new Date(value);
  return (
    d.getFullYear() +
    '-' +
    String(d.getMonth() + 1).padStart(2, '0') +
    '-' +
    String(d.getDate()).padStart(2, '0')
  );
}
function download(v: Creation) {
  const ext =
    v.kind === 'video'
      ? 'mp4'
      : v.contentType === 'image/png'
        ? 'png'
        : v.contentType === 'image/webp'
          ? 'webp'
          : 'jpg';
  void downloadProtected(v.contentUrl, 'Daydream-' + v.outputId + '.' + ext).catch((e) =>
    toast.error(e.message),
  );
}
function CreationThumbnail({ item }: { item: Creation }) {
  const { c } = useCatalogText();
  const [failed, setFailed] = useState(false);
  return item.thumbnailUrl && !failed ? (
    <img
      src={protectedResourceUrl(item.thumbnailUrl)}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
    />
  ) : (
    <span className="creation-no-preview">{c('暂无预览')}</span>
  );
}
export function CreationsPage() {
  const { c } = useCatalogText(),
    { language } = useLanguage();
  const locale = language === 'en' ? 'en-US' : 'zh-CN';
  const [params, setParams] = useSearchParams();
  const [kind, setKind] = useState('image'),
    [draft, setDraft] = useState(''),
    [search, setSearch] = useState('');
  const scope = JSON.stringify([kind, search]),
    sequence = useRef(0);
  const [page, setPage] = useState<{
    scope: string;
    items: Creation[];
    cursor: string | null;
    busy: boolean;
    error: string;
  }>({ scope, items: [], cursor: null, busy: true, error: '' });
  useEffect(() => {
    const timer = setTimeout(() => setSearch(draft.trim()), 300);
    return () => clearTimeout(timer);
  }, [draft]);
  async function read(cursor?: string) {
    const ticket = ++sequence.current;
    setPage((old) => ({
      scope,
      items: cursor && old.scope === scope ? old.items : [],
      cursor: cursor || null,
      busy: true,
      error: '',
    }));
    try {
      const v = await mediaLibraryApi.list(kind, search, cursor, 'all');
      if (sequence.current !== ticket) return;
      setPage((old) => ({
        scope,
        items: cursor
          ? [
              ...old.items,
              ...v.items.filter((item) => !old.items.some((p) => keyOf(p) === keyOf(item))),
            ]
          : v.items,
        cursor: v.nextCursor,
        busy: false,
        error: '',
      }));
    } catch (e) {
      if (sequence.current === ticket)
        setPage((old) => ({
          ...old,
          busy: false,
          error: (e as Error).message,
        }));
    }
  }
  useEffect(() => {
    void read();
    return () => {
      sequence.current++;
    };
  }, [scope]);
  const current =
    page.scope === scope ? page : { scope, items: [], cursor: null, busy: true, error: '' };
  const groups = new Map<string, Creation[]>();
  for (const item of current.items) {
    const day = creationDay(item.createdAt);
    groups.set(day, [...(groups.get(day) || []), item]);
  }
  const open = (v: Creation) => {
    const next = new URLSearchParams(params);
    next.set('task', v.taskId);
    next.set('output', v.outputId);
    setParams(next);
  };
  return (
    <section className="creations-page" aria-label={c('创作资产')}>
      <h1>{c('创作资产')}</h1>
      <div className="creations-toolbar">
        <ViewTabs
          items={[
            { value: 'image', label: c('图片') },
            { value: 'video', label: c('视频') },
          ]}
          value={kind}
          onChange={setKind}
          label={c('媒体类型')}
          prefix="creation-kind-"
        />
        <div className="creations-search-tools">
          <label className="creations-search">
            <Search aria-hidden />
            <input
              aria-label={c('搜索提示词或项目名称')}
              maxLength={200}
              placeholder={c('搜索提示词或项目名称')}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            {draft ? (
              <button
                aria-label={c('清空搜索')}
                onClick={() => {
                  setDraft('');
                  setSearch('');
                }}
              >
                <X />
              </button>
            ) : null}
          </label>
        </div>
      </div>
      <div className="creations-scroll" aria-busy={current.busy}>
        {[...groups].map(([day, items]) => (
          <section className="creation-day" key={day} aria-label={day}>
            <header>
              <h2>
                {new Date(items[0].createdAt).toLocaleDateString(locale, {
                  month: 'long',
                  day: 'numeric',
                  weekday: 'long',
                })}
              </h2>
              <time dateTime={day}>{day}</time>
            </header>
            <div className="creations-grid">
              {items.map((item) => (
                <article className="creation-card" key={keyOf(item)}>
                  <button
                    className="creation-card-open"
                    onClick={() => open(item)}
                    aria-label={c('查看{name}', {
                      name: item.promptPreview || item.projectTitle,
                    })}
                  >
                    <div className="creation-card-image">
                      <CreationThumbnail item={item} />
                      <span
                        className="creation-kind"
                        aria-label={c(item.kind === 'image' ? '图片' : '视频')}
                      >
                        {item.kind === 'image' ? <ImageIcon /> : <Video />}
                      </span>
                      {item.kind === 'video' ? (
                        <span className="creation-play">
                          <Play />
                        </span>
                      ) : null}
                    </div>
                    <strong>{item.promptPreview || item.projectTitle}</strong>
                    <span className="space-resource-meta">
                      {c('创建人：{name}', { name: item.creatorName })}
                      {item.sharedWithMe ? <span>{c('共享给我')}</span> : null}
                    </span>
                  </button>
                  <footer>
                    <time dateTime={item.createdAt}>
                      {new Date(item.createdAt).toLocaleTimeString(locale, {
                        hour: '2-digit',
                        minute: '2-digit',
                      })}
                    </time>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={c('下载生成结果')}
                      onClick={() => download(item)}
                    >
                      <Download />
                    </Button>
                  </footer>
                </article>
              ))}
            </div>
          </section>
        ))}
        {current.error ? (
          <div role="alert" className="creations-feedback jm-inline-error">
            {current.error}
            <Button variant="link" onClick={() => void read(current.cursor || undefined)}>
              {c('重新读取')}
            </Button>
          </div>
        ) : null}
        {current.busy ? (
          <div role="status" className="creations-feedback">
            <Spinner />
            {c('正在读取…')}
          </div>
        ) : !current.items.length && !current.error ? (
          <p className="creations-feedback">{c(search ? '没有匹配的资产' : '暂无创作资产')}</p>
        ) : null}
        {current.cursor && !current.busy && !current.error ? (
          <Button
            className="creations-more"
            variant="outline"
            onClick={() => void read(current.cursor!)}
          >
            {c('加载更多')}
          </Button>
        ) : null}
      </div>
      <CreationDetail
        task={params.get('task') || ''}
        output={params.get('output') || ''}
        onClose={() => {
          const next = new URLSearchParams(params);
          next.delete('task');
          next.delete('output');
          setParams(next, { replace: true });
        }}
      />
    </section>
  );
}
function CreationDetail({
  task,
  output,
  onClose,
}: {
  task: string;
  output: string;
  onClose: () => void;
}) {
  const { c } = useCatalogText(),
    { language } = useLanguage(),
    { models } = useWorkspace();
  const detail = useRead(
      () => mediaLibraryApi.detail(task, output),
      [task, output],
      !!task && !!output,
    ),
    item = detail.value;
  const [failed, setFailed] = useState(false),
    [retry, setRetry] = useState(0);
  const content = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setFailed(false);
    setRetry(0);
  }, [task, output]);
  const model = item
    ? [...models.image, ...models.video, ...(models.catalog || [])].find(
        (m) => m.id === item.modelId,
      )?.name || item.modelId
    : '';
  return (
    <Dialog
      open={!!task && !!output}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        ref={content}
        variant="preview"
        className="creation-detail"
        tabIndex={-1}
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          content.current?.focus();
        }}
      >
        <DialogTitle className="sr-only">{c('资产详情')}</DialogTitle>
        <DialogDescription className="sr-only">{c('图片、视频及生成信息')}</DialogDescription>
        {detail.busy ? (
          <div role="status" className="creations-feedback">
            <Spinner />
            {c('正在读取…')}
          </div>
        ) : detail.error ? (
          <div role="alert" className="creations-feedback jm-inline-error">
            {detail.error}
            <Button variant="outline" onClick={detail.refresh}>
              {c('重新读取')}
            </Button>
          </div>
        ) : item ? (
          <div className="creation-detail-body">
            <div className="creation-detail-stage">
              {failed ? (
                <div role="alert" className="creations-feedback">
                  {c('媒体读取失败')}
                  <Button
                    variant="outline"
                    onClick={() => {
                      setFailed(false);
                      setRetry((v) => v + 1);
                    }}
                  >
                    {c('重新读取')}
                  </Button>
                </div>
              ) : item.kind === 'image' ? (
                <img
                  key={keyOf(item) + retry}
                  src={protectedResourceUrl(item.contentUrl)}
                  alt={item.projectTitle}
                  onError={() => setFailed(true)}
                />
              ) : (
                <video
                  key={keyOf(item) + retry}
                  src={protectedResourceUrl(item.contentUrl)}
                  poster={item.thumbnailUrl ? protectedResourceUrl(item.thumbnailUrl) : undefined}
                  controls
                  playsInline
                  preload="metadata"
                  onError={() => setFailed(true)}
                />
              )}
            </div>
            <aside className="creation-detail-info">
              <div className="creation-detail-actions">
                <Button variant="outline" onClick={() => download(item)}>
                  <Download />
                  {c('下载')}
                </Button>
              </div>
              <h2>{c(item.kind === 'image' ? '图片提示词' : '视频提示词')}</h2>
              <p className="creation-detail-prompt">{item.prompt || '—'}</p>
              <dl>
                {[
                  [c('模型'), model],
                  [c('模型标识'), item.requestModelId],
                  [c('比例'), item.aspectRatio],
                  [c('分辨率'), item.resolution],
                  [c('尺寸'), item.width + ' × ' + item.height],
                  ...(item.kind === 'video'
                    ? [[c('时长'), c('{duration} 秒', { duration: item.durationSeconds })]]
                    : []),
                  [c('项目'), item.projectTitle],
                  [
                    c('生成时间'),
                    new Date(item.createdAt).toLocaleString(language === 'en' ? 'en-US' : 'zh-CN'),
                  ],
                ].map(([label, value]) => (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>{value || '—'}</dd>
                  </div>
                ))}
              </dl>
            </aside>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
