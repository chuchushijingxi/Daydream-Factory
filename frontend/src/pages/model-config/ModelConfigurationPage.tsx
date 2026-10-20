import { useModelConfiguration } from './useModelConfiguration';
import { categories, modelCategoryOptions } from './model-config-data';
import {
  Image,
  AlertCircle,
  Settings2,
  ShieldCheck,
  Video,
  Plus,
  Server,
  Trash2,
  Search,
} from 'lucide-react';

import { Button } from '../../components/ui/button';
import { Spinner } from '../../components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { ModelMark, ModelCategoryTabs } from './ModelConfigurationControls';
import { labels, defaultLogo } from './model-config-data';

import { ModelConnectionEditor } from './ModelConnectionEditor';
export function ModelConfigurationPage() {
  const { listing, editor, gateway } = useModelConfiguration();
  const {
    c,
    status,
    registry,
    category,
    selectCategory,
    source,
    selectSource,
    search,
    searchConnections,
    loading,
    error,
    busy,
    deleting,
    requestDelete,
    cancelDelete,
    refresh,
    editable,
    unsupported,
    edit,
    remove,
  } = listing;
  const capabilityOptions = modelCategoryOptions(c);
  const directoryCategories =
    source !== 'gateway'
      ? capabilityOptions.filter((o) =>
          registry?.connections.some(
            (conn) => conn.kind === source && conn.models.some((m) => m.category === o.value),
          ),
        )
      : capabilityOptions;
  const connections = (registry?.connections || []).filter(
    (conn) =>
      conn.kind === source &&
      (conn.models.some((m) => m.category === category) ||
        (conn.models.length === 0 && category === 'analysis')) &&
      (!search ||
        [conn.name, conn.baseUrl, ...conn.models.flatMap((m) => [m.name, m.modelId])].some((t) =>
          t.toLowerCase().includes(search.toLowerCase()),
        )),
  );
  return (
    <section className="mc-page" aria-labelledby="mc-title">
      <header className="mc-header mc-directory-header">
        <div className="mc-directory-heading">
          <h1 id="mc-title">{c('模型配置')}</h1>
          <Button
            id="mc-new-gateway"
            disabled={!editable}
            onClick={(event) => {
              selectSource('gateway');
              selectCategory('analysis');
              edit(undefined, event.currentTarget);
            }}
          >
            <Plus />
            {c('新增中转站')}
          </Button>
        </div>
        <p>{c('管理官方服务与中转站，配置凭证并选择要启用的模型。')}</p>
      </header>
      <div className="mc-directory-controls">
        <div className="mc-source-tabs">
          <ModelCategoryTabs
            items={[
              {
                value: 'official',
                label: c('官方模型'),
              },
              {
                value: 'gateway',
                label: c('中转站'),
              },
              { value: 'builtin', label: c('本地测试') },
            ]}
            value={source}
            onChange={(value) => {
              selectSource(value);
              selectCategory(
                value !== 'gateway'
                  ? registry?.connections.find((conn) => conn.kind === value)?.models[0]
                      ?.category || 'analysis'
                  : 'analysis',
              );
            }}
            label={c('连接来源')}
            prefix="mc-source-"
            variant="line"
          />
        </div>
        <label className="mc-search">
          <Search aria-hidden />
          <input
            value={search}
            onChange={(e) => searchConnections(e.target.value)}
            aria-label={c('搜索连接或模型')}
            placeholder={c('搜索连接或模型')}
          />
        </label>
      </div>
      {source === 'gateway' || directoryCategories.length > 1 ? (
        <div className="mc-toolbar">
          <ModelCategoryTabs
            items={directoryCategories}
            value={category}
            onChange={(v) => selectCategory(v as typeof category)}
            label={c('模型分类')}
            prefix="mc-tab-"
            variant="pill"
          />
        </div>
      ) : null}
      {status?.readOnly ? (
        <p className="mc-notice">
          <ShieldCheck aria-hidden />
          {c('当前服务只读，模型配置仅可查看。')}
        </p>
      ) : null}
      {unsupported ? (
        <p className="mc-notice">{c('当前服务尚未接入模型配置，请更新后端服务。')}</p>
      ) : null}
      {error ? (
        <div role="alert" className="mc-error">
          {error}
          <Button
            variant="outline"
            onClick={() => void refresh().catch(() => {})}
            disabled={loading}
          >
            {c('重新读取')}
          </Button>
        </div>
      ) : null}
      <div id="mc-models" role="tabpanel" aria-labelledby={'mc-source-' + source}>
        {loading ? (
          <div className="mc-loading">
            <Spinner />
            {c('正在读取模型配置…')}
          </div>
        ) : !error && !unsupported && connections.length ? (
          <div className="mc-grid">
            {connections.map((conn) => (
              <article
                key={conn.id}
                className="mc-card mc-supplier-card"
                data-connection-id={conn.id}
                data-configured={conn.credentialConfigured}
                aria-label={conn.name}
              >
                <div className="mc-card-head">
                  <div className="mc-model-mark">
                    {conn.kind !== 'gateway' ? (
                      <ModelMark
                        url={
                          conn.models[0]?.defaultLogoUrl ||
                          (conn.models[0] ? defaultLogo(conn.models[0]) : '')
                        }
                        category="analysis"
                      />
                    ) : (
                      <Server aria-hidden />
                    )}
                  </div>
                  <div className="mc-supplier-name">
                    <div className="mc-supplier-title">
                      <h2>{conn.name}</h2>
                      {!conn.credentialConfigured && conn.kind !== 'builtin' ? (
                        <span className="mc-setup-badge">
                          <AlertCircle aria-hidden />
                          {c('待配置')}
                        </span>
                      ) : null}
                    </div>
                    <p className="mc-endpoint" title={conn.baseUrl}>
                      {conn.kind === 'builtin' ? c('本地运行 · 无需 API Key') : conn.baseUrl}
                    </p>
                  </div>
                </div>
                <p className="mc-catalog-description">
                  {c(
                    conn.kind === 'builtin'
                      ? '用于工作台的本地图片与视频模拟，可配置返回延迟和失败概率。'
                      : conn.kind === 'gateway'
                        ? '通过同一服务连接，管理不同厂商的语言、图片和视频模型。'
                        : conn.protocol === 'volcengine-ark'
                          ? '豆包语言、Seedream 图片与 Seedance 视频，共用一套方舟凭证。'
                          : conn.protocol === 'kling-media'
                            ? '可灵图片与视频模型，共用一套 API Key。'
                            : conn.protocol === 'dashscope-media'
                              ? 'Wan Image 与 HappyHorse，共用一套百炼 API Key。'
                              : conn.protocol === 'gemini'
                                ? 'Gemini 语言与 Nano Banana 图片，共用一套 Google API Key。'
                                : conn.protocol === 'openai-image'
                                  ? 'GPT Image 图片生成与编辑，共用一套 OpenAI API Key。'
                                  : '理解完整剧本，提取与纠错资产，编写资产视觉正文。',
                  )}
                </p>
                <div className="mc-catalog-tags">
                  {categories
                    .filter((kind) => conn.models.some((model) => model.category === kind))
                    .map((kind) => (
                      <span key={kind}>{c(labels[kind])}</span>
                    ))}
                  {conn.kind === 'official' &&
                  conn.models.some((m) => m.category === 'analysis') ? (
                    <>
                      <span>{c('思考模式')}</span>
                      <span>{c('工具调用')}</span>
                    </>
                  ) : null}
                </div>
                {conn.credentialConfigured && conn.reason ? (
                  <p className="mc-reason">{c(conn.reason)}</p>
                ) : null}
                <footer className="mc-card-footer">
                  <span className="mc-enabled-count">
                    {c('{enabled}/{total} 已启用', {
                      enabled: conn.models.filter((m) => m.enabled).length,
                      total: conn.models.length,
                    })}
                  </span>
                  <div className="mc-inline-actions">
                    {conn.kind === 'gateway' ? (
                      <Button
                        variant="destructive"
                        size="sm"
                        aria-label={c('删除连接 {name}', {
                          name: conn.name,
                        })}
                        onClick={() => requestDelete(conn)}
                        disabled={!conn.editable || busy || conn.models.some((m) => m.referenced)}
                      >
                        <Trash2 aria-hidden />
                        {c('删除')}
                      </Button>
                    ) : null}
                    <Button
                      id={'mc-config-' + conn.id}
                      className="mc-config-action"
                      variant={
                        conn.credentialConfigured || conn.kind === 'builtin' ? 'outline' : 'default'
                      }
                      onClick={(event) => {
                        edit(conn, event.currentTarget);
                      }}
                      disabled={!conn.editable || busy}
                    >
                      <Settings2 />
                      {c(
                        !conn.credentialConfigured && conn.kind !== 'builtin' ? '立即配置' : '配置',
                      )}
                    </Button>
                  </div>
                </footer>
              </article>
            ))}
          </div>
        ) : !error && !unsupported ? (
          <div className="mc-empty">
            {category === 'image' ? (
              <Image aria-hidden />
            ) : category === 'video' ? (
              <Video aria-hidden />
            ) : (
              <Server aria-hidden />
            )}
            <h2>
              {c(
                search
                  ? '没有匹配的连接或模型'
                  : source === 'gateway'
                    ? '还没有中转站'
                    : '该分类暂无官方模型',
              )}
            </h2>
            <p>
              {c(
                source === 'gateway'
                  ? '新增一个中转站，即可在其中管理三类模型。'
                  : '官方模型会随接入更新；也可到中转站添加自己的模型。',
              )}
            </p>
          </div>
        ) : null}
      </div>
      <ModelConnectionEditor view={editor} gateway={gateway} />
      <Dialog
        open={!!deleting}
        onOpenChange={(value) => {
          if (!value && !busy) cancelDelete();
        }}
      >
        <DialogContent variant="confirm" closeDisabled={busy}>
          <DialogTitle>{c('删除连接')}</DialogTitle>
          <DialogDescription>
            {c('删除 {name} 的连接、凭证及模型配置；已被项目引用的连接不能删除。', {
              name: deleting?.name || '',
            })}
          </DialogDescription>
          <div className="mc-form-footer">
            <Button variant="outline" disabled={busy} onClick={() => cancelDelete()}>
              {c('取消')}
            </Button>
            <Button disabled={busy} onClick={() => void remove()}>
              {c('删除')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </section>
  );
}
