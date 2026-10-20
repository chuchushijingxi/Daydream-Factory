import { useEffect, useRef, useState } from 'react';
import type { ModelConfigurationViews } from './model-configuration-views';
import { api } from '../../api/client.ts';
import {
  type ModelConfiguration,
  type ModelConnection,
  type ConnectionInput,
  type ModelSettingsInput,
  type TokenPricing,
} from '../../api/types';
import { useWorkspace } from '../../state/WorkspaceContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { toast } from '../../components/ui/sonner';
import { categories, blankConnection, blankModel, type DirectoryItem } from './model-config-data';

// 连接编辑负责草稿、CAS 冲突、显式保存与凭证清除；渲染组件不管理请求生命周期。
export function useModelConfiguration(): ModelConfigurationViews {
  const { c } = useCatalogText(),
    { status, refreshModels, error: appError } = useWorkspace();
  const [registry, setRegistry] = useState<ModelConfiguration | null>(null),
    [category, setCategory] = useState<(typeof categories)[number]>('analysis'),
    [source, setSource] = useState('official'),
    [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<ConnectionInput>(blankConnection),
    [original, setOriginal] = useState<ModelConnection | null>(null),
    [apiKey, setApiKey] = useState(''),
    [showKey, setShowKey] = useState(false),
    [draftRevision, setDraftRevision] = useState(0),
    [formError, setFormError] = useState(''),
    [conflict, setConflict] = useState(false),
    [expanded, setExpanded] = useState<number[]>([]),
    [deleting, setDeleting] = useState<ModelConnection | null>(null),
    [editCategory, setEditCategory] = useState('analysis');
  const [directory, setDirectory] = useState<DirectoryItem[] | null>(null),
    [directoryError, setDirectoryError] = useState(''),
    [directorySearch, setDirectorySearch] = useState(''),
    [activity, setActivity] = useState('');
  const alive = useRef(true),
    submitting = useRef(false),
    configTrigger = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const readConfig = async () => {
    setLoading(true);
    try {
      const v = await api.getModelConfiguration();
      if (alive.current) {
        setRegistry(v);
        setError('');
        if (source !== 'gateway')
          setCategory((current) =>
            v.connections.some(
              (conn) => conn.kind === source && conn.models.some((m) => m.category === current),
            )
              ? current
              : v.connections.find((conn) => conn.kind === source)?.models[0]?.category ||
                'analysis',
          );
      }
      return v;
    } catch (e) {
      if (alive.current) setError((e as Error).message);
      throw e;
    } finally {
      if (alive.current) setLoading(false);
    }
  };
  useEffect(() => {
    if (status?.capabilities.modelConfiguration) void readConfig().catch(() => {});
    else if (status) setLoading(false);
  }, [status?.capabilities.modelConfiguration]);
  useEffect(() => {
    if (appError && !status) {
      setLoading(false);
      setError(appError);
    }
  }, [appError, status]);
  const editable = Boolean(registry?.editable && !status?.readOnly && !busy),
    unsupported = status && !status.capabilities.modelConfiguration,
    official = draft.kind !== 'gateway';
  const close = () => {
    if (submitting.current) return;
    setOpen(false);
    setApiKey('');
    setShowKey(false);
    setFormError('');
    setConflict(false);
    setDirectory(null);
  };
  const edit = (conn?: ModelConnection, trigger?: HTMLButtonElement) => {
    if (trigger) configTrigger.current = trigger;
    if (!registry) return;
    setOriginal(conn || null);
    setDraft(
      conn
        ? {
            id: conn.id,
            name: conn.name,
            kind: conn.kind,
            protocol: conn.protocol,
            baseUrl: conn.baseUrl,
            models: conn.models.map(
              ({
                id,
                logoId,
                modelId,
                alias,
                description,
                category,
                enabled,
                contextWindow,
                maxOutputTokens,
                pricing,
                creditPrice,
                resolutionEnabled,
                mockBehavior,
              }) => ({
                id,
                ...(mockBehavior ? { mockBehavior: { ...mockBehavior } } : {}),
                creditPrice: creditPrice
                  ? {
                      ...creditPrice,
                      resolutionPrices: creditPrice.resolutionPrices
                        ? { ...creditPrice.resolutionPrices }
                        : undefined,
                    }
                  : null,
                ...(resolutionEnabled ? { resolutionEnabled: { ...resolutionEnabled } } : {}),
                logoId: logoId || '',
                modelId,
                alias,
                description: description || '',
                category,
                enabled,
                contextWindow,
                maxOutputTokens,
                pricing: pricing
                  ? {
                      ...pricing,
                    }
                  : null,
              }),
            ),
          }
        : blankConnection(),
    );
    setDraftRevision(registry.revision);
    setApiKey('');
    setShowKey(false);
    setFormError('');
    setConflict(false);
    setExpanded([]);
    setEditCategory(
      conn?.models.some((m) => m.category === category)
        ? category
        : conn?.models[0]?.category || 'analysis',
    );
    setDirectory(null);
    setDirectoryError('');
    setActivity('');
    setOpen(true);
  };
  const accepted = async (v: ModelConfiguration) => {
    if (!alive.current) return;
    setRegistry(v);
    toast.success(c('模型配置已保存'));
    try {
      await refreshModels();
    } catch (e) {
      if (alive.current)
        setError(
          c('配置已保存，模型候选刷新失败：{reason}', {
            reason: (e as Error).message,
          }),
        );
    }
  };
  const patch = (index: number, change: Partial<ModelSettingsInput>) =>
    setDraft((v) => ({
      ...v,
      models: v.models.map((m, i) =>
        i === index
          ? {
              ...m,
              ...change,
            }
          : m,
      ),
    }));
  const pricing = (index: number, key: keyof TokenPricing, value: string) => {
    const old = draft.models[index].pricing || {
      inputCnyPerMillion: 0,
      outputCnyPerMillion: 0,
      cacheHitHundredthsCnyPerMillion: 0,
    };
    const next = {
      ...old,
      [key]: Number(value),
    };
    patch(index, {
      pricing:
        next.inputCnyPerMillion === 0 &&
        next.outputCnyPerMillion === 0 &&
        next.cacheHitHundredthsCnyPerMillion === 0
          ? null
          : next,
    });
  };
  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting.current || !editable) return;
    if (
      !event.currentTarget.reportValidity() ||
      draft.models.some((m) => m.creditPrice && m.creditPrice.unitPrice < 0)
    ) {
      setFormError(c('请为已开启的档位填写有效积分价格。'));
      return;
    }
    if (
      draft.models.some((model) => {
        const profile = original?.models.find((saved) => saved.id === model.id)?.mediaProfile;
        return (
          profile &&
          Object.entries(model.resolutionEnabled || {}).some(
            ([resolution, enabled]) =>
              enabled &&
              (!model.creditPrice ||
                (model.creditPrice.resolutionPrices?.[resolution] === undefined &&
                  (resolution !== profile.defaultResolution || model.creditPrice.unitPrice < 0))),
          )
        );
      })
    ) {
      setFormError(c('请为已开启的档位填写积分价格。'));
      return;
    }
    if (draft.models.some((model) => [...model.description.trim()].length > 200)) {
      setFormError(c('模型描述最多200个字符'));
      return;
    }
    if (
      draft.kind === 'builtin' &&
      (draft.models.some(
        (model) =>
          !model.mockBehavior ||
          !Number.isInteger(model.mockBehavior.delaySeconds) ||
          model.mockBehavior.delaySeconds < 0 ||
          model.mockBehavior.delaySeconds > 60 ||
          !Number.isInteger(model.mockBehavior.failurePercent) ||
          model.mockBehavior.failurePercent < 0 ||
          model.mockBehavior.failurePercent > 100,
      ) ||
        !event.currentTarget.checkValidity())
    ) {
      setFormError(c('模拟延迟须为0～60秒的整数，失败概率须为0～100的整数。'));
      return;
    }
    if (
      draft.kind !== 'builtin' &&
      ((original?.credentialConfigured && draft.baseUrl !== original.baseUrl) ||
        (!original?.credentialConfigured && draft.models.some((m) => m.enabled))) &&
      !apiKey
    ) {
      setFormError(c('请填写该 API 地址的 API Key'));
      document.getElementById('model-api-key')?.focus();
      return;
    }
    submitting.current = true;
    setBusy(true);
    setActivity(c('保存中…'));
    setFormError('');
    setConflict(false);
    const connection = {
      ...draft,
      models: draft.models.map((m) =>
        m.category === 'analysis'
          ? m
          : {
              ...m,
              contextWindow: 0,
              maxOutputTokens: 0,
              pricing: null,
            },
      ),
    };
    try {
      const v = await api.saveModelConfiguration({
        revision: draftRevision,
        connection,
        ...(apiKey
          ? {
              apiKey,
            }
          : {}),
      });
      if (alive.current) {
        setOpen(false);
        setApiKey('');
        setShowKey(false);
      }
      await accepted(v);
    } catch (e) {
      if (alive.current) {
        setFormError((e as Error).message);
        setConflict(
          (
            e as {
              code?: string;
            }
          ).code === 'CONFLICT',
        );
      }
    } finally {
      submitting.current = false;
      if (alive.current) {
        setBusy(false);
        setActivity('');
      }
    }
  };
  const refreshDraft = async () => {
    try {
      const v = await readConfig();
      if (alive.current) {
        setDraftRevision(v.revision);
        setConflict(false);
        setOriginal(v.connections.find((x) => x.id === draft.id) || null);
        setFormError(c('已读取最新配置，当前输入已保留，请核对后保存。'));
      }
    } catch {}
  };
  const remove = async () => {
    if (!registry || !deleting || submitting.current) return;
    submitting.current = true;
    setBusy(true);
    try {
      const v = await api.deleteModelConnection(deleting.id, {
        revision: registry.revision,
      });
      setDeleting(null);
      await accepted(v);
    } catch (e) {
      if (alive.current) {
        setError((e as Error).message);
        setDeleting(null);
      }
    } finally {
      submitting.current = false;
      if (alive.current) setBusy(false);
    }
  };
  const upload = async (index: number, file?: File) => {
    if (!file || submitting.current) return;
    if (file.size > 2 * 1024 * 1024) {
      setFormError(c('Logo支持PNG或JPEG，最大2MB。'));
      return;
    }
    submitting.current = true;
    setBusy(true);
    setActivity(c('上传Logo…'));
    setFormError('');
    try {
      const logo = await api.uploadModelLogo(file);
      if (alive.current)
        patch(index, {
          logoId: logo.logoId,
        });
    } catch (e) {
      if (alive.current) setFormError((e as Error).message);
    } finally {
      submitting.current = false;
      if (alive.current) {
        setBusy(false);
        setActivity('');
      }
    }
  };
  const discover = async () => {
    if (submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setActivity(c('获取模型列表…'));
    setDirectoryError('');
    setDirectory(null);
    try {
      const v = await api.discoverModels({
        revision: draftRevision,
        connectionId: draft.id,
        baseUrl: draft.baseUrl,
        protocol: draft.protocol,
        ...(apiKey
          ? {
              apiKey,
            }
          : {}),
      });
      if (alive.current) {
        setDirectory(
          v.models.map((m) => ({
            ...m,
            category: 'analysis',
            selected: false,
          })),
        );
        setDirectorySearch('');
      }
    } catch (e) {
      if (alive.current) {
        setDirectoryError((e as Error).message);
        setConflict(
          (
            e as {
              code?: string;
            }
          ).code === 'CONFLICT',
        );
      }
    } finally {
      submitting.current = false;
      if (alive.current) {
        setBusy(false);
        setActivity('');
      }
    }
  };
  const importSelected = () => {
    const selected = (directory || []).filter(
      (m) => m.selected && !draft.models.some((old) => old.modelId === m.modelId),
    );
    if (draft.models.length + selected.length > 100) {
      setDirectoryError(c('每个连接最多100个模型，请减少选择。'));
      return;
    }
    setDraft((v) => ({
      ...v,
      models: [
        ...v.models,
        ...selected.map((m) => ({
          ...blankModel(),
          modelId: m.modelId,
          category: m.category,
        })),
      ],
    }));
    setDirectory(
      (v) =>
        v?.map((m) => ({
          ...m,
          selected: false,
        })) || null,
    );
  };

  const updateConnection = (
    change: Partial<Pick<ConnectionInput, 'name' | 'protocol' | 'baseUrl'>>,
  ) => setDraft((value) => ({ ...value, ...change }));
  const addModel = (category: ModelSettingsInput['category']) => {
    setExpanded((value) => [...value, draft.models.length]);
    setDraft((value) => ({ ...value, models: [...value.models, { ...blankModel(), category }] }));
  };
  const removeModel = (index: number) => {
    setDraft((value) => ({ ...value, models: value.models.filter((_, i) => i !== index) }));
    setExpanded([]);
  };
  const toggleModelDetails = (index: number) =>
    setExpanded((value) =>
      value.includes(index) ? value.filter((i) => i !== index) : [...value, index],
    );
  const updateDiscoveredModel = (
    id: string,
    change: Partial<Pick<DirectoryItem, 'selected' | 'category'>>,
  ) =>
    setDirectory(
      (value) =>
        value?.map((item) => (item.modelId === id ? { ...item, ...change } : item)) || null,
    );
  // 视图按角色取得资料与显式动作；原始 setter、提交标记和焦点 ref 留在用例内部。
  const fields = {
    c,
    loading,
    busy,
    draft,
    original,
    apiKey,
    showKey,
    formError,
    conflict,
    expanded,
    editCategory,
    updateCredential: (value: string) => setApiKey(value),
    toggleCredentialVisibility: () => setShowKey((value) => !value),
    selectEditorCategory: (value: string) => setEditCategory(value),
    toggleModelDetails,
    patch,
    pricing,
    refreshDraft,
  };
  return {
    listing: {
      c,
      status,
      registry,
      category,
      source,
      search,
      loading,
      error,
      busy,
      deleting,
      editable,
      unsupported,
      selectCategory: (value) => setCategory(value),
      selectSource: (value) => setSource(value),
      searchConnections: (value) => setSearch(value),
      refresh: readConfig,
      edit,
      requestDelete: (connection) => setDeleting(connection),
      cancelDelete: () => setDeleting(null),
      remove,
    },
    editor: {
      ...fields,
      open,
      activity,
      editable,
      official,
      returnFocusId: configTrigger.current?.id,
      close,
      clearFormError: () => setFormError(''),
      save,
      upload,
    },
    gateway: {
      ...fields,
      directory,
      directoryError,
      directorySearch,
      updateConnection,
      addModel,
      removeModel,
      closeDiscovery: () => setDirectory(null),
      searchDiscovery: (value) => setDirectorySearch(value),
      updateDiscoveredModel,
      discover,
      importSelected,
    },
  };
}
