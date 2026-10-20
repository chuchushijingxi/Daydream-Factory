import { workflow } from '../../../domain/project-workflow.ts';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { useEffect, useRef, useState } from 'react';
import { Plus, ChevronDown } from 'lucide-react';
import { Button } from '../../../components/ui/button';
import { useWorkspace } from '../../../state/WorkspaceContext';
import type { Asset, Look, Project, ScriptSource } from '../../../api/types';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '../../../components/ui/dialog';
import { AssetImageCard } from './AssetImageCard';
import { VisualPromptEditor } from './VisualPromptEditor';

export function VisualPromptDetail({
  assetId,
  lookId,
  title,
}: {
  assetId: string;
  lookId?: string;
  title: string;
}) {
  const { c } = useCatalogText();
  const { currentProject: p } = useWorkspace();
  const target = {
      assetId,
      ...(lookId
        ? {
            lookId,
          }
        : {}),
    },
    prompt = p ? workflow.prompt(p, target) : null,
    task = p ? workflow.targetTask(p, target) : null;
  return (
    <section className="asset-detail-section">
      <h3 className="asset-detail-label">{title}</h3>
      {task?.status === 'failed' ? (
        <p role="alert" className="jm-inline-error">
          {task.error || task.errorCode || c('原因未返回，请重新读取。')}
        </p>
      ) : null}
      <div className="asset-prompt-box">
        <VisualPromptEditor
          key={(p?.id || '') + ':' + assetId + ':' + (lookId || '')}
          target={target}
          text={prompt?.text || ''}
        />
      </div>
    </section>
  );
}
function SourceDetails({
  source,
  targetId,
  onClose,
}: {
  source: ScriptSource | null;
  targetId: string | null;
  onClose: () => void;
}) {
  const { c } = useCatalogText();
  const selected = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!targetId) return;
    const frame = requestAnimationFrame(() =>
      selected.current?.scrollIntoView({
        block: 'center',
      }),
    );
    return () => cancelAnimationFrame(frame);
  }, [targetId]);
  return (
    <Dialog
      open={targetId !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent variant="panel" className="source-details-dialog">
        <div className="asset-detail-header">
          <DialogTitle>{c('原文依据')}</DialogTitle>
          <DialogDescription>
            {source
              ? c('{filename} · {count} 段', {
                  filename: source.filename,
                  count: source.spans.length,
                })
              : c('原文读取失败，请刷新。')}
          </DialogDescription>
        </div>
        <div className="source-details-body">
          {source?.spans.map((span, index) => (
            <div
              key={span.id}
              ref={span.id === targetId ? selected : undefined}
              className="source-details-span"
              data-selected={span.id === targetId}
            >
              <h3>
                {c('第 {index} 段', {
                  index: index + 1,
                })}
              </h3>
              <p>{span.text}</p>
            </div>
          ))}
        </div>
        <div className="asset-detail-footer">
          <Button variant="outline" onClick={onClose}>
            {c('关闭')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
export function AssetDetails({
  asset,
  editable,
  active = true,
  description,
  onDescription,
  onImageSaved,
  onImageBusy,
  onLookAction,
}: {
  asset: Asset;
  editable: boolean;
  active?: boolean;
  description: string;
  onDescription: (value: string) => void;
  onImageSaved?: (project: Project, expectedRevision: number) => void;
  onImageBusy?: (busy: boolean) => void;
  onLookAction: (mode: 'create' | 'edit' | 'delete', look?: Look) => void;
}) {
  const { c } = useCatalogText();
  const { source, status } = useWorkspace();
  const [sourceTarget, setSourceTarget] = useState<string | null>(null);
  const looksEditable = editable && status?.capabilities.characterLooks === true;
  return (
    <>
      <div className="asset-detail-layout">
        <div className="asset-detail-image">
          <AssetImageCard
            showGenerate={false}
            image={asset.image}
            target={{ assetId: asset.id }}
            editable={editable}
            active={active}
            onSaved={onImageSaved}
            onBusy={onImageBusy}
            label={
              asset.kind === 'character'
                ? c('角色图片')
                : asset.kind === 'location'
                  ? c('场景图片')
                  : c('道具图片')
            }
          />
          <aside className="asset-detail-context" aria-label={c('别名与原文来源')}>
            {asset.aliases.length ? (
              <div className="asset-alias-row">
                <h3 className="asset-detail-label">{c('别名')}</h3>
                <div className="asset-aliases">
                  {asset.aliases.map((alias, index) => (
                    <span key={index}>{alias}</span>
                  ))}
                </div>
              </div>
            ) : null}
            <details key={asset.id} className="asset-source-disclosure asset-detail-context-gap">
              <summary className="asset-detail-label">
                {c('原文来源 · {count} 段', {
                  count: asset.sourceIds.length,
                })}
                <ChevronDown aria-hidden />
              </summary>
              <div className="asset-source-list">
                {asset.sourceIds.length ? (
                  asset.sourceIds.map((id) => {
                    const index = source?.spans.findIndex((span) => span.id === id) ?? -1;
                    return (
                      <article key={id} className="asset-source-quote">
                        <Button variant="link" size="sm" onClick={() => setSourceTarget(id)}>
                          {index >= 0
                            ? c('第 {index} 段', {
                                index: index + 1,
                              })
                            : c('原文片段')}{' '}
                          ↗
                        </Button>
                        <p>
                          {index >= 0 ? source?.spans[index].text : c('来源读取失败，请刷新。')}
                        </p>
                      </article>
                    );
                  })
                ) : (
                  <p className="asset-detail-empty">
                    {c('未引用原文；不为用户填写的内容补造引用。')}
                  </p>
                )}
              </div>
            </details>
          </aside>
        </div>
        <section className="asset-detail-main" aria-label={c('资产完整说明')}>
          <h3 className="asset-detail-label">
            {asset.kind === 'character' ? c('角色资料') : c('完整说明')}
          </h3>
          <textarea
            id="asset-description"
            className="jm-field-textarea asset-detail-textarea"
            rows={4}
            value={description}
            disabled={!editable}
            aria-label={asset.kind === 'character' ? c('角色资料') : c('完整说明')}
            onChange={(e) => onDescription(e.target.value)}
          />
          <VisualPromptDetail assetId={asset.id} title={c('视觉提示词')} />
          {asset.kind === 'character' ? (
            <section className="asset-detail-section">
              <div className="asset-detail-section-head">
                <h3>
                  {c('附加形象 · {count} 项', {
                    count: asset.looks.length,
                  })}
                </h3>
                <div className="asset-detail-section-actions">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!looksEditable}
                    onClick={() => onLookAction('create')}
                  >
                    <Plus />
                    {c('新增附加形象')}
                  </Button>
                </div>
              </div>
              {asset.looks.length ? (
                asset.looks.map((look) => (
                  <article key={look.id} className="asset-look-detail">
                    <div className="asset-look-head">
                      <h4>{look.name}</h4>
                      <span className="asset-origin-chip">
                        {look.origin === 'user' ? c('用户填写') : c('模型提取')}
                      </span>
                    </div>
                    <div className="asset-look-content">
                      <AssetImageCard
                        showGenerate={false}
                        image={look.image}
                        target={{ assetId: asset.id, lookId: look.id }}
                        editable={looksEditable}
                        active={active}
                        label={c('{name} · 图片', {
                          name: look.name,
                        })}
                      />
                      <div>
                        <p className="asset-detail-description">
                          {look.description || c('暂无说明')}
                        </p>
                        <VisualPromptDetail
                          assetId={asset.id}
                          lookId={look.id}
                          title={c('附加视觉提示词')}
                        />
                      </div>
                    </div>
                    <div className="asset-look-actions">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={!looksEditable}
                        onClick={() => onLookAction('edit', look)}
                      >
                        {c('编辑')}
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        disabled={!looksEditable}
                        onClick={() => onLookAction('delete', look)}
                      >
                        {c('删除')}
                      </Button>
                    </div>
                  </article>
                ))
              ) : (
                <p className="asset-detail-empty">
                  {c('暂无附加形象。需要不同造型或状态时再新增。')}
                </p>
              )}
            </section>
          ) : null}
        </section>
      </div>
      <SourceDetails
        source={source}
        targetId={sourceTarget}
        onClose={() => setSourceTarget(null)}
      />
    </>
  );
}
