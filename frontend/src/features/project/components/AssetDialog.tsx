import { workflow } from '../../../domain/project-workflow.ts';
import { useCatalogText, workflowCopy } from '../../../contexts/useCatalogText';
import { useEffect, useState, useRef } from 'react';
import { toast } from '../../../components/ui/sonner';
import { Button } from '../../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from '../../../components/ui/dialog';
import { AssetDetails } from './AssetDetails';
import { AssetImageCard } from './AssetImageCard';
import { Label } from '../../../components/ui/label';
import { useWorkspace } from '../../../state/WorkspaceContext';
import { api } from '../../../api/client.ts';
import { type Asset, type Character, type Look } from '../../../api/types';
import {
  needsProjectReadback,
  projectRequestMessage,
  writeResultUnknown,
} from '../../../api/errors';
const emptyCharacter: Character = {
  roleType: '',
  age: '',
  gender: '',
  biography: '',
  appearance: '',
  personality: '',
};
const inputClass = 'jm-field-input',
  textareaClass = 'jm-field-textarea';
export function AssetDialog({
  open,
  onOpenChange,
  kind,
  asset: incomingAsset,
  look: incomingLook,
  createLook: incomingCreateLook = false,
  initialDelete = false,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  kind: Asset['kind'];
  asset?: Asset;
  look?: Look;
  createLook?: boolean;
  initialDelete?: boolean;
}) {
  const { c } = useCatalogText();
  const displayed = useRef<{
    asset?: Asset;
    look?: Look;
    createLook: boolean;
  }>({
    createLook: false,
  });
  const detailContent = useRef<HTMLDivElement>(null);
  if (open)
    displayed.current = {
      asset: incomingAsset,
      look: incomingLook,
      createLook: incomingCreateLook,
    };
  const { asset: cachedAsset, look: cachedLook, createLook } = displayed.current;
  const {
    currentProject: p,
    source,
    status,
    acceptProject,
    refreshCurrentProject,
  } = useWorkspace();
  const asset = p?.assets.find((a) => a.id === cachedAsset?.id) || cachedAsset,
    look = asset?.looks.find((l) => l.id === cachedLook?.id) || cachedLook;
  const [lookDialog, setLookDialog] = useState<{
    mode: 'create' | 'edit' | 'delete';
    look?: Look;
  } | null>(null);
  const [name, setName] = useState(''),
    [description, setDescription] = useState(''),
    [aliases, setAliases] = useState(''),
    [character, setCharacter] = useState<Character>(emptyCharacter),
    [saving, setSaving] = useState(false),
    [imageBusy, setImageBusy] = useState(false),
    [error, setError] = useState(''),
    [deleting, setDeleting] = useState(false),
    [draftRevision, setDraftRevision] = useState(0),
    [readback, setReadback] = useState(false),
    [uncertainCreate, setUncertainCreate] = useState(false);
  const isLook = createLook || Boolean(look);
  useEffect(() => {
    if (open) {
      setDraftRevision(p?.revision || 0);
      setReadback(false);
      setUncertainCreate(false);
      setName(isLook ? (look?.name ?? '') : (asset?.name ?? ''));
      setDescription(isLook ? (look?.description ?? '') : (asset?.description ?? ''));
      setAliases(asset?.aliases.join('\n') || '');
      setCharacter(asset?.character || emptyCharacter);
      setError('');
      setDeleting(initialDelete);
      setLookDialog(null);
    }
  }, [open, asset?.id, look?.id, createLook, initialDelete]);
  if (!p) return null;
  const editable = Boolean(
      status &&
        !status.readOnly &&
        status.capabilities.assetEditing === true &&
        (!isLook || status.capabilities.characterLooks === true) &&
        workflow.state(p).editable,
    ),
    label = isLook
      ? c('附加形象')
      : kind === 'character'
        ? c('角色')
        : kind === 'location'
          ? c('场景')
          : c('道具');
  const missing =
    Boolean(cachedAsset && !p.assets.some((a) => a.id === cachedAsset.id)) ||
    Boolean(cachedLook && !asset?.looks.some((l) => l.id === cachedLook.id));
  const canSubmit = editable && !missing && !readback && !imageBusy;
  const reload = async () => {
    setSaving(true);
    try {
      const latest = await refreshCurrentProject(p.id);
      setDraftRevision(latest.revision);
      const parent = latest.assets.find((a) => a.id === cachedAsset?.id);
      const gone =
        Boolean(cachedAsset && !parent) ||
        Boolean(cachedLook && !parent?.looks.some((l) => l.id === cachedLook.id));
      setReadback(gone || uncertainCreate);
      setError(
        c(
          uncertainCreate
            ? '保存结果已读取，请核对目录；草稿保留，不再重复新增。'
            : gone
              ? '对象已不存在，草稿保留，请关闭后查看最新目录。'
              : '最新版本已读取，草稿保留；请核对后保存。',
        ),
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const failure = (e: unknown) => {
    setError(projectRequestMessage(e, c));
    if (needsProjectReadback(e)) setReadback(true);
    if ((!asset || createLook) && writeResultUnknown(e)) setUncertainCreate(true);
  };
  const errorView = error ? (
    <div role="alert" className="jm-inline-error rounded-lg p-3">
      {error}
      <Button
        variant="link"
        size="sm"
        className="ml-2"
        disabled={saving}
        onClick={() => void reload()}
      >
        {c('读取最新版本')}
      </Button>
    </div>
  ) : null;
  const detailMode = Boolean(asset && !isLook && !createLook);
  const prompt =
    asset && !createLook
      ? workflow.prompt(p, {
          assetId: asset.id,
          ...(look
            ? {
                lookId: look.id,
              }
            : {}),
        })
      : null;
  const save = async () => {
    if (!canSubmit || saving || !name.trim()) return;
    setSaving(true);
    setError('');
    try {
      let result;
      if (isLook) {
        if (!asset) throw new Error(c('缺少所属角色'));
        const payload = {
          revision: draftRevision,
          look: {
            name,
            description,
          },
        };
        result = look
          ? await api.updateLook(p.id, asset.id, look.id, payload)
          : await api.createLook(p.id, asset.id, payload);
      } else {
        const payload = {
          revision: draftRevision,
          asset: {
            kind,
            name,
            description,
            aliases: aliases
              .split('\n')
              .map((v) => v.trim())
              .filter(Boolean),
            character: kind === 'character' ? character : null,
          },
        };
        result = asset
          ? await api.updateAsset(p.id, asset.id, payload)
          : await api.createAsset(p.id, payload);
      }
      acceptProject(result);
      onOpenChange(false);
      toast.success(c('资料已保存'));
    } catch (e) {
      failure(e);
    } finally {
      setSaving(false);
    }
  };
  const remove = async () => {
    if (!canSubmit || saving || !asset) return;
    setSaving(true);
    setError('');
    try {
      const result = look
        ? await api.deleteLook(p.id, asset.id, look.id, {
            revision: draftRevision,
          })
        : await api.deleteAsset(p.id, asset.id, {
            revision: draftRevision,
          });
      acceptProject(result);
      onOpenChange(false);
      toast.success(c('已删除'));
    } catch (e) {
      failure(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!saving) onOpenChange(v);
      }}
    >
      <DialogContent
        variant={deleting ? 'confirm' : 'panel'}
        ref={detailContent}
        closeDisabled={saving}
        onOpenAutoFocus={(event) => {
          if (detailMode && !deleting) {
            event.preventDefault();
            detailContent.current?.focus({
              preventScroll: true,
            });
          }
        }}
        aria-label={
          detailMode && !deleting
            ? c('查看{kind}', {
                kind: label,
              })
            : undefined
        }
        className="flex max-h-[92vh] w-[min(calc(100vw-2rem),976px)] max-w-[976px] flex-col gap-0 overflow-hidden rounded-2xl p-0"
      >
        {deleting ? (
          <>
            <DialogTitle>{c('删除{kind}', { kind: label })}</DialogTitle>
            <DialogDescription>
              {c('将删除「{name}」，不会自动恢复；原文和历史费用保留。', { name })}
            </DialogDescription>
            {errorView}
            <DialogFooter>
              <Button variant="outline" disabled={saving} onClick={() => setDeleting(false)}>
                {c('取消')}
              </Button>
              <Button
                variant="destructive"
                disabled={!canSubmit}
                busy={saving}
                onClick={() => void remove()}
              >
                {c('确认删除{kind}', { kind: label })}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <div className="asset-detail-header">
              <div>
                <p className="asset-detail-kicker">
                  {label} · {detailMode ? c('资产详情') : deleting ? c('确认删除') : c('资料编辑')}
                </p>
                {detailMode ? (
                  <>
                    <DialogTitle className="sr-only">
                      {c('查看{kind}', {
                        kind: label,
                      })}
                    </DialogTitle>
                    <input
                      id="asset-name"
                      aria-label={c('名称')}
                      className="asset-detail-title-input"
                      value={name}
                      disabled={!editable || saving || deleting}
                      onChange={(e) => setName(e.target.value)}
                    />
                    <DialogDescription className="sr-only">{c('资产详情')}</DialogDescription>
                  </>
                ) : (
                  <DialogTitle>
                    {deleting
                      ? c('删除{kind}', {
                          kind: label,
                        })
                      : c(asset && !createLook ? '编辑{kind}' : '新增{kind}', {
                          kind: label,
                        })}
                  </DialogTitle>
                )}
                {!detailMode ? (
                  <DialogDescription>
                    {status?.readOnly
                      ? c('当前服务只读。')
                      : editable
                        ? c('普通保存只更新资料，不调用模型。')
                        : workflowCopy(workflow.state(p), c).editReason}
                  </DialogDescription>
                ) : null}
              </div>
            </div>
            {detailMode && asset ? (
              <>
                <div className="asset-detail-scroll">
                  <AssetDetails
                    active={open}
                    asset={asset}
                    editable={editable && !saving && !deleting}
                    description={description}
                    onDescription={setDescription}
                    onImageSaved={(result, expectedRevision) =>
                      setDraftRevision((previous) =>
                        previous === expectedRevision ? result.revision : previous,
                      )
                    }
                    onImageBusy={setImageBusy}
                    onLookAction={(mode, look) =>
                      setLookDialog({
                        mode,
                        look,
                      })
                    }
                  />
                  {errorView}
                </div>
                <div className="asset-detail-footer">
                  {deleting ? (
                    <>
                      <p className="jm-inline-error">
                        {c('将删除「{name}」{looks}。原文和历史费用保留。', {
                          name: asset.name,
                          looks: asset.looks.length
                            ? c('及其 {count} 项附加形象', {
                                count: asset.looks.length,
                              })
                            : '',
                        })}
                      </p>
                      <Button
                        variant="outline"
                        disabled={saving}
                        onClick={() => setDeleting(false)}
                      >
                        {c('取消')}
                      </Button>
                      <Button
                        variant="destructive"
                        disabled={!canSubmit}
                        busy={saving}
                        onClick={() => void remove()}
                      >
                        {c('确认删除{kind}', {
                          kind: label,
                        })}
                      </Button>
                    </>
                  ) : (
                    <>
                      <Button variant="outline" onClick={() => onOpenChange(false)}>
                        {c('取消')}
                      </Button>
                      <Button
                        disabled={!name.trim() || !canSubmit}
                        busy={saving}
                        onClick={() => void save()}
                      >
                        {c('保存')}
                      </Button>
                    </>
                  )}
                </div>
              </>
            ) : null}
            {detailMode && asset?.kind === 'character' ? (
              <AssetDialog
                open={lookDialog !== null}
                kind="character"
                asset={asset}
                look={lookDialog?.look}
                createLook={lookDialog?.mode === 'create'}
                initialDelete={lookDialog?.mode === 'delete'}
                onOpenChange={(v) => {
                  if (!v) setLookDialog(null);
                }}
              />
            ) : null}
            {!detailMode ? (
              <>
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void save();
                  }}
                  className="flex min-h-0 flex-col"
                >
                  <div className="min-h-0 overflow-y-auto px-6 pb-6">
                    <div className="grid grid-cols-1 gap-6 md:grid-cols-[1fr_320px]">
                      <div className="min-w-0 space-y-4">
                        <div>
                          <Label htmlFor="asset-form-name">{c('名称')}</Label>
                          <input
                            id="asset-form-name"
                            required
                            value={name}
                            disabled={!editable || saving || deleting}
                            onChange={(e) => setName(e.target.value)}
                            className={inputClass}
                          />
                        </div>
                        <div>
                          <Label htmlFor="asset-form-description">{c('描述')}</Label>
                          <textarea
                            id="asset-form-description"
                            rows={5}
                            value={description}
                            disabled={!editable || saving || deleting}
                            onChange={(e) => setDescription(e.target.value)}
                            className={textareaClass}
                          />
                        </div>
                        {!isLook ? (
                          <div>
                            <Label htmlFor="asset-aliases">{c('别名（每行一项）')}</Label>
                            <textarea
                              id="asset-aliases"
                              rows={2}
                              value={aliases}
                              disabled={!editable || saving || deleting}
                              onChange={(e) => setAliases(e.target.value)}
                              className={textareaClass}
                            />
                          </div>
                        ) : null}
                        {kind === 'character' && !isLook
                          ? (
                              Object.entries({
                                roleType: c('定位'),
                                age: c('年龄'),
                                gender: c('性别'),
                                biography: c('小传'),
                                appearance: c('外貌'),
                                personality: c('性格'),
                              }) as [keyof Character, string][]
                            ).map(([key, title]) => (
                              <div key={key}>
                                <Label htmlFor={'character-' + key}>{title}</Label>
                                <textarea
                                  id={'character-' + key}
                                  rows={
                                    key === 'biography' ||
                                    key === 'appearance' ||
                                    key === 'personality'
                                      ? 3
                                      : 1
                                  }
                                  value={character[key]}
                                  disabled={!editable || saving || deleting}
                                  onChange={(e) =>
                                    setCharacter({
                                      ...character,
                                      [key]: e.target.value,
                                    })
                                  }
                                  className={textareaClass}
                                />
                              </div>
                            ))
                          : null}
                        {asset && !isLook && asset.sourceIds.length ? (
                          <details>
                            <summary className="cursor-pointer text-sm">
                              {c('原文依据 · {count} 段', {
                                count: asset.sourceIds.length,
                              })}
                            </summary>
                            {asset.sourceIds.map((id) => (
                              <p
                                key={id}
                                className="mt-3 whitespace-pre-wrap text-[13px] leading-6 text-slate-500 dark:text-slate-300"
                              >
                                {source?.spans.find((s) => s.id === id)?.text ||
                                  c('来源读取失败，请刷新。')}
                              </p>
                            ))}
                          </details>
                        ) : null}
                        {deleting ? (
                          <p className="jm-inline-error">
                            {c('将删除「{name}」，不会自动恢复；原文和历史费用保留。', {
                              name,
                            })}
                          </p>
                        ) : null}
                        {errorView}
                      </div>
                      <div className="min-w-0">
                        <AssetImageCard
                          image={isLook ? look?.image : asset?.image}
                          active={open}
                          target={
                            asset && !createLook
                              ? { assetId: asset.id, ...(look ? { lookId: look.id } : {}) }
                              : undefined
                          }
                          editable={
                            editable && !missing && !readback && !saving && !deleting && open
                          }
                          onBusy={setImageBusy}
                          onSaved={(result, expectedRevision) =>
                            setDraftRevision((previous) =>
                              previous === expectedRevision ? result.revision : previous,
                            )
                          }
                          label={
                            isLook
                              ? c('附加形象图片')
                              : kind === 'character'
                                ? c('角色图片')
                                : kind === 'location'
                                  ? c('场景图片')
                                  : c('道具图片')
                          }
                        />
                        {asset && !createLook ? (
                          <div className="mt-5">
                            <h3 className="text-sm font-medium dark:text-white">
                              {c('视觉提示词 · {status}', {
                                status: prompt
                                  ? prompt.stale
                                    ? c('已失效')
                                    : c('已生成')
                                  : c('未生成'),
                              })}
                            </h3>
                            <p className="mt-2 whitespace-pre-wrap text-[13px] leading-6 text-slate-500 dark:text-slate-300">
                              {prompt?.text || c('未生成')}
                            </p>
                            {prompt?.text ? (
                              <Button
                                variant="link"
                                size="sm"
                                className="mt-2"
                                onClick={() =>
                                  void navigator.clipboard
                                    .writeText(prompt.text)
                                    .then(() => toast.success(c('正文已复制')))
                                    .catch(() => toast.error(c('复制失败')))
                                }
                              >
                                {c('复制正文')}
                              </Button>
                            ) : null}
                          </div>
                        ) : null}
                      </div>
                    </div>
                  </div>
                  <DialogFooter className="shrink-0 flex-row items-center justify-end gap-3 border-t border-[#E5E7EB] px-6 py-5">
                    <Button variant="outline" disabled={saving} onClick={() => onOpenChange(false)}>
                      {c('关闭')}
                    </Button>
                    {editable && asset && !createLook ? (
                      <Button
                        variant="destructive"
                        disabled={saving || !canSubmit}
                        busy={saving && deleting}
                        onClick={() => (deleting ? void remove() : setDeleting(true))}
                      >
                        {c(deleting ? '确认删除{kind}' : '删除{kind}', {
                          kind: label,
                        })}
                      </Button>
                    ) : null}
                    {editable && !deleting ? (
                      <Button type="submit" disabled={!name.trim() || !canSubmit} busy={saving}>
                        {c('保存')}
                      </Button>
                    ) : null}
                  </DialogFooter>
                </form>
              </>
            ) : null}
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
