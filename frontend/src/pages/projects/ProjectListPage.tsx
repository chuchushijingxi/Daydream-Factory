import { ResourceSharingDialog } from '../../components/spaces/ResourceSharingDialog';
import { useOptionalSpace } from '../../state/SpaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useContext, useEffect, useRef, useState } from 'react';
import { SessionOptionalContext } from '../../state/SessionContext';
import { billingApi } from '../../api/billing-client';
import { ChargeNotice, useChargeQuote } from '../../features/billing/useChargeQuote';
import { useNavigate } from 'react-router-dom';
import { toast } from '../../components/ui/sonner';
import { ImageWithFallback } from '../../components/media/ImageWithFallback';
import { useLanguage } from '../../contexts/LanguageContext';
import { useWorkspace } from '../../state/WorkspaceContext';
import { cn } from '../../components/ui/utils';
import { api } from '../../api/client.ts';
import { type Project, type VisualStyle, type UploadedFile } from '../../api/types';
import { needsProjectReadback } from '../../api/errors';
import { Button } from '../../components/ui/button';
import { ScriptUploadPanel } from '../../features/project/components/ScriptUploadPanel';
import {
  CreationSettingsControls,
  EMPTY_CREATION_SETTINGS,
} from '../../features/project/components/CreationSettingsControls';
import { useProjectManagement, ProjectSelectionMark } from './ProjectManagement';
import { ProjectRunStatus } from './ProjectRunStatus';
export function ProjectListPage() {
  const { c } = useCatalogText();
  const {
    projects,
    handleSelectProject,
    theme,
    status,
    refreshProjects,
  } = useWorkspace();
  const space = useOptionalSpace();
  const [sharing, setSharing] = useState<import('../../api/types').ProjectRow | null>(null);
  const { t, language } = useLanguage();
  const navigate = useNavigate();
  const isDark = theme === 'dark';
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [uploadedFile, setUploadedFile] = useState<File | null>(null),
    [storedFile, setStoredFile] = useState<UploadedFile | null>(null),
    [isUploading, setIsUploading] = useState(false),
    [isDragging, setIsDragging] = useState(false);
  const [settings, setSettings] = useState(EMPTY_CREATION_SETTINGS),
    [style, setStyle] = useState<VisualStyle | null>(null),
    [created, setCreated] = useState<Project | null>(null),
    [saveError, setSaveError] = useState(''),
    [readback, setReadback] = useState(false);
  const uploadingRef = useRef(false);
  const session = useContext(SessionOptionalContext);
  const [fileLoading, setFileLoading] = useState(false);
  useEffect(() => {
    if (!session?.user || !uploadedFile || storedFile || created) return;
    let stopped = false;
    const controller = new AbortController();
    setFileLoading(true);
    setSaveError('');
    void api
      .uploadFile(uploadedFile, {}, controller.signal)
      .then((file) => {
        if (!stopped) setStoredFile(file);
      })
      .catch((error) => {
        if (!stopped) {
          setSaveError(error.message);
          setReadback(true);
        }
      })
      .finally(() => {
        if (!stopped) setFileLoading(false);
      });
    return () => {
      stopped = true;
      controller.abort();
    };
  }, [uploadedFile, session?.epoch]);
  const quote = useChargeQuote(
    () => billingApi.file(storedFile!.fileId, settings),
    JSON.stringify([storedFile?.fileId, settings]),
    !!storedFile && !!settings.llmModel && !created && !status?.readOnly,
  );

  const management = useProjectManagement();
  const choose = (file: File) => {
    if (created || isUploading || !status || status.readOnly) return false;
    if (!['txt', 'md', 'docx'].includes(file.name.split('.').pop()?.toLowerCase() || '')) {
      toast.error(c('支持 TXT、Markdown、DOCX'));
      return false;
    }
    if (file.size > 20 * 1024 * 1024) {
      toast.error(c('文件超过 20 MB'));
      return false;
    }
    setUploadedFile(file);
    setStoredFile(null);
    return true;
  };
  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files?.[0]) choose(e.target.files[0]);
  };
  const handleDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(false);
    if (e.dataTransfer.files[0]) choose(e.dataTransfer.files[0]);
  };
  const handleRemoveFile = () => {
    if (created || uploadingRef.current) return;
    setUploadedFile(null);
    setStoredFile(null);
    if (fileInputRef.current) fileInputRef.current.value = '';
  };
  const handleParseScriptClick = async () => {
    if (
      !uploadedFile ||
      !status ||
      status.readOnly ||
      !settings.llmModel ||
      uploadingRef.current ||
      readback ||
      (!created && quote.blocked)
    )
      return;
    uploadingRef.current = true;
    setIsUploading(true);
    setSaveError('');
    let p = created;
    try {
      if (Object.values(settings).some(Boolean) && !status.capabilities.projectSettings)
        throw new Error(c('项目设置尚未接入，草稿保留。'));
      if (style && !status.capabilities.projectStyle)
        throw new Error(c('固定风格尚未接入，草稿保留。'));
      if (!p) {
        const file = storedFile || (await api.uploadFile(uploadedFile));
        setStoredFile(file);
        p = await api.createProject({
          fileId: file.fileId,
          ...(quote.basis ? { billingBasis: quote.basis } : {}),
          title: uploadedFile.name.replace(/\.[^.]+$/, ''),
          ...(Object.values(settings).some(Boolean) ? { settings } : {}),
          ...(style ? { styleId: style.id, promptVersion: style.promptVersion } : {}),
        });
        setCreated(p);
      }
      navigate(`/playlet/${p.id}/outline`);
      void refreshProjects().catch((e) =>
        toast.error(
          c('原文已保存；列表刷新失败：{reason}', {
            reason: e.message,
          }),
        ),
      );
    } catch (e) {
      if ((e as { code?: string }).code === 'BILLING_BASIS_CHANGED') quote.refresh();
      const message = p
        ? c('原文已保存；配置保存失败，保留草稿，可再次保存：{reason}', {
            reason: (e as Error).message,
          })
        : (e as Error).message;
      setSaveError(message);
      if (needsProjectReadback(e)) setReadback(true);
      toast.error(message);
    } finally {
      uploadingRef.current = false;
      setIsUploading(false);
    }
  };
  const reloadCreated = async () => {
    if (!created || uploadingRef.current) return;
    uploadingRef.current = true;
    setIsUploading(true);
    try {
      const saved = await api.getProject(created.id);
      setCreated(saved);
      if (saved.settings) setSettings(saved.settings);
      if (saved.style) setStyle(saved.style);
      setReadback(false);
      setSaveError(c('已读取项目保存结果，请核对参数后继续。'));
    } catch (e) {
      setSaveError((e as Error).message);
    } finally {
      uploadingRef.current = false;
      setIsUploading(false);
    }
  };
  return (
    <>
      <div className="xy-home-page jm-workspace-surface flex min-h-0 flex-1 flex-col">
        <div
          className={
            'xy-home-scroll jm-workspace-surface min-h-0 flex-1 overflow-y-auto' +
            (management.managing ? ' project-management-scroll' : '')
          }
        >
          <div className="xy-home-create mx-auto max-w-[1320px]">
            <div className="xy-home-heading px-8">
              <div className="text-center">
                <h1
                  className={cn(
                    'text-[32px] font-medium leading-none',
                    isDark ? 'text-white' : 'text-[#000000]',
                  )}
                >
                  {t('playletHome.title')}
                </h1>
                <p
                  className={cn(
                    'mt-[12px] text-[12px] font-normal leading-none',
                    isDark ? 'text-slate-400' : 'text-[#0A1B317A]',
                  )}
                >
                  {c('让文字成戏，让想象开机。')}
                </p>
              </div>
              {uploadedFile && !created && quote.error ? <ChargeNotice quote={quote} /> : null}
            </div>
            <div className="px-8">
              <ScriptUploadPanel
                file={uploadedFile}
                inputRef={fileInputRef}
                status={status}
                busy={isUploading || fileLoading}
                saveBlocked={readback || !settings.llmModel || (!created && quote.blocked)}
                fileLocked={Boolean(created)}
                dragging={isDragging}
                onChoose={choose}
                onFileChange={handleFileSelect}
                onDrop={handleDrop}
                onDragOver={(event) => {
                  event.preventDefault();
                  if (!created && !isUploading && status && !status.readOnly) setIsDragging(true);
                }}
                onDragLeave={() => setIsDragging(false)}
                onRemove={handleRemoveFile}
                onSave={() => void handleParseScriptClick()}
                saveLabel={created ? c('进入剧本') : c('开始解析')}
                saveBusy={quote.busy}
                saveAmount={
                  quote.required && !created && !quote.blocked ? quote.value?.amount : undefined
                }
                settingsControls={
                  <CreationSettingsControls
                    value={settings}
                    style={created?.style || style}
                    disabled={isUploading}
                    parametersLocked={Boolean(created?.settings)}
                    onStyleChange={setStyle}
                    styleLocked={Boolean(created?.style)}
                    onChange={(key, value) =>
                      setSettings((s) => ({
                        ...s,
                        [key]: value,
                      }))
                    }
                  />
                }
              />
            </div>
            <div className="xy-home-following">
              {saveError ? (
                <p role="alert" className="jm-inline-error mt-3 text-center">
                  {saveError}
                  {readback && !created ? (
                    <span>
                      {storedFile
                        ? c('项目保存结果未确认，请刷新项目列表核对，勿重复提交。')
                        : c('文件上传结果未确认，请勿重复提交。')}
                    </span>
                  ) : null}
                  {created ? (
                    <Button
                      variant="link"
                      size="sm"
                      disabled={isUploading}
                      onClick={() => void reloadCreated()}
                    >
                      {c('读取最新版本')}
                    </Button>
                  ) : null}
                </p>
              ) : null}
              {status?.readOnly ? (
                <p
                  className={cn(
                    'mt-5 text-center text-[12px] font-normal',
                    isDark ? 'text-slate-500' : 'text-black/40',
                  )}
                >
                  {c('真实项目只读浏览，上传已禁用。') + ' '}
                </p>
              ) : null}

              <div className="xy-home-projects jm-workspace-surface">
                <div className="mx-auto max-w-[1320px]">
                  <div className="flex items-center justify-between px-10">
                    <h2
                      className={cn(
                        'text-[18px] font-medium',
                        isDark ? 'text-slate-100' : 'text-playlet-title',
                      )}
                    >
                      {t('playletHome.myProjects')}
                    </h2>
                    {management.controls}
                  </div>
                  {management.feedback}

                  <div className="mt-5 grid grid-cols-1 gap-5 px-10 md:grid-cols-2 xl:grid-cols-4">
                    {projects.map((project) => (
                      <div
                        key={project.id}
                        role={management.managing ? 'checkbox' : 'button'}
                        aria-checked={
                          management.managing ? management.selected.has(project.id) : undefined
                        }
                        aria-label={
                          management.managing
                            ? c('选择项目：{name}', {
                                name: project.title,
                              })
                            : undefined
                        }
                        aria-disabled={(management.managing && management.busy) || undefined}
                        tabIndex={0}
                        onClick={() => {
                          if (management.managing) management.toggle(project.id);
                          else void handleSelectProject(project);
                        }}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            if (management.managing) management.toggle(project.id);
                            else void handleSelectProject(project);
                          }
                        }}
                        className={cn(
                          'project-list-card cursor-pointer overflow-hidden rounded-[12px] text-left outline-none focus-visible:ring-1 focus-visible:ring-black/20 dark:focus-visible:ring-white/20',
                          isDark ? 'bg-[#1c1c1c]' : 'bg-[#f5f5f5]',
                          management.managing &&
                            management.selected.has(project.id) &&
                            'project-card-selected',
                        )}
                      >
                        <div
                          className={cn(
                            'relative h-[160px]',
                            isDark ? 'bg-[#1c1c1c]' : 'bg-[#f5f5f5]',
                          )}
                        >
                          {management.managing ? (
                            <span className="project-card-checkbox">
                              <ProjectSelectionMark checked={management.selected.has(project.id)} />
                            </span>
                          ) : null}
                          <div
                            className="grid h-full gap-2 pl-[12px] pr-[12px] pt-[12px]"
                            style={{
                              gridTemplateColumns: `repeat(${Math.max(1, project.previewImages?.length || 0)}, minmax(0, 1fr))`,
                            }}
                          >
                            {(project.previewImages || []).map((img, imgIndex) => (
                              <ImageWithFallback
                                key={`${project.id}-${imgIndex}`}
                                src={img}
                                alt={`${project.title}-${imgIndex + 1}`}
                                className="h-full w-full rounded-[12px] object-cover"
                              />
                            ))}
                            {!project.previewImages?.length ? (
                              <div
                                className={cn(
                                  'rounded-[12px]',
                                  isDark ? 'bg-black' : 'bg-[#d9d9d9]',
                                )}
                              />
                            ) : null}
                          </div>
                          <ProjectRunStatus run={project.latestRun} />
                        </div>
                        <div className="px-3 pb-3 pt-2.5">
                          <div
                            className={cn(
                              'line-clamp-1 text-[14px] font-medium leading-tight',
                              isDark ? 'text-white' : 'text-[#1f1f1f]',
                            )}
                          >
                            {project.title}
                          </div>
                          <div className="space-resource-meta">
                            <span>{c('创建人：{name}', { name: project.creatorName })}</span>
                            {project.sharedWithMe ? <span>{c('共享给我')}</span> : null}
                          </div>
                          {space?.current?.kind === 'team' &&
                          (space.current.role !== 'member' ||
                            project.ownerId === session?.user?.identity) ? (
                            <button
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation();
                                setSharing(project);
                              }}
                              onKeyDown={(event) => event.stopPropagation()}
                            >
                              {c('共享')}
                            </button>
                          ) : null}

                          <div
                            className={cn(
                              'mt-1.5 flex items-center gap-1.5 text-[12px]',
                              isDark ? 'text-white/50' : 'text-[#8c8c8c]',
                            )}
                          >
                            {project.uploadedAt &&
                            Number.isFinite(Date.parse(project.uploadedAt)) ? (
                              <time
                                dateTime={project.uploadedAt}
                                title={new Date(project.uploadedAt).toLocaleString(
                                  language === 'zh' ? 'zh-CN' : 'en-GB',
                                )}
                              >
                                {new Date(project.uploadedAt).toLocaleString(
                                  language === 'zh' ? 'zh-CN' : 'en-GB',
                                  {
                                    year: 'numeric',
                                    month: '2-digit',
                                    day: '2-digit',
                                    hour: '2-digit',
                                    minute: '2-digit',
                                    hour12: false,
                                  },
                                )}
                              </time>
                            ) : (
                              <span>{c('上传时间未知')}</span>
                            )}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                  {projects.length === 0 && (
                    <div
                      className={cn(
                        'mt-8 text-center text-sm',
                        isDark ? 'text-slate-400' : 'text-black/50',
                      )}
                    >
                      {c('暂无项目，请先上传剧本创建一个项目')}
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>

        {management.footer}
        {management.dialog}
        {sharing ? (
          <ResourceSharingDialog
            open
            onOpenChange={(open) => {
              if (!open) setSharing(null);
            }}
            spaceId={sharing.spaceId}
            projectId={sharing.id}
          />
        ) : null}
      </div>
    </>
  );
}
