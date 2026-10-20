import { ModelCreditPricingFields } from './ModelCreditPricingFields';
import { ChevronDown, Eye, EyeOff, Plus, Trash2, Download, Search } from 'lucide-react';

import { type ConnectionInput, type ModelSettingsInput } from '../../api/types';
import { Button } from '../../components/ui/button';

import { ModelSwitch, ModelSelect, ModelCategoryTabs } from './ModelConfigurationControls';

import type { GatewayEditorView } from './model-configuration-views';
import type { ReactNode } from 'react';
type GatewayConnectionFieldsProps = {
  view: GatewayEditorView;
  logoControl: (model: ModelSettingsInput, index: number) => ReactNode;
  modelName: (model: ModelSettingsInput) => string;
  capabilityOptions: {
    value: string;
    label: string;
  }[];
  editorCategories: {
    value: string;
    label: string;
  }[];
};
// 中转站字段只展示草稿；请求、CAS 冲突及凭证清除由父用例控制。
export function GatewayConnectionFields({
  view,
  logoControl,
  modelName,
  capabilityOptions,
  editorCategories,
}: GatewayConnectionFieldsProps) {
  const {
    c,
    loading,
    busy,
    draft,
    updateConnection,
    addModel,
    removeModel,
    original,
    apiKey,
    updateCredential,
    showKey,
    toggleCredentialVisibility,
    formError,
    conflict,
    expanded,
    toggleModelDetails,
    editCategory,
    selectEditorCategory,
    directory,
    closeDiscovery,
    updateDiscoveredModel,
    directoryError,
    directorySearch,
    searchDiscovery,
    patch,
    pricing,
    refreshDraft,
    discover,
    importSelected,
  } = view;
  return (
    <>
      <div className="mc-gateway-credentials" data-source={draft.kind}>
        {
          <div className="mc-fields-grid">
            <div className="mc-field">
              <label htmlFor="mc-connection-name">{c('中转站名称')}</label>
              <input
                id="mc-connection-name"
                value={draft.name}
                onChange={(e) => updateConnection({ name: e.target.value })}
                required
                maxLength={120}
                disabled={busy}
              />
            </div>
            <div className="mc-field">
              <label>{c('语言接口类型')}</label>
              <ModelSelect
                value={draft.protocol}
                options={[
                  {
                    value: 'openai-chat',
                    label: c('OpenAI 兼容'),
                  },
                  {
                    value: 'deepseek',
                    label: c('DeepSeek 兼容'),
                  },
                ]}
                label={c('语言接口类型')}
                onChange={(value) => {
                  updateConnection({ protocol: value as ConnectionInput['protocol'] });
                  closeDiscovery();
                }}
                disabled={busy}
              />
            </div>
          </div>
        }
        <div className="mc-field">
          <label htmlFor="model-base-url">
            {c('API 地址')}
            {null}
          </label>
          <input
            id="model-base-url"
            value={draft.baseUrl}
            onChange={(e) => {
              updateConnection({ baseUrl: e.target.value });
              closeDiscovery();
            }}
            type="url"
            required
            disabled={busy}
            autoComplete="off"
            spellCheck={false}
          />
        </div>
        <div className="mc-field">
          <label htmlFor="model-api-key">API Key</label>
          <div className="mc-key-input">
            <input
              id="model-api-key"
              value={apiKey}
              onChange={(e) => {
                updateCredential(e.target.value);
                closeDiscovery();
              }}
              type={showKey ? 'text' : 'password'}
              placeholder={c(
                original?.credentialConfigured && draft.baseUrl === original.baseUrl
                  ? '不填写则保留原密钥'
                  : '填写 API Key',
              )}
              disabled={busy}
              autoComplete="off"
              spellCheck={false}
            />
            <button
              type="button"
              onClick={() => toggleCredentialVisibility()}
              aria-label={c(showKey ? '隐藏新密钥' : '显示新密钥')}
              disabled={busy}
            >
              {showKey ? <EyeOff /> : <Eye />}
            </button>
          </div>
          <p>{c('所有模型共用此密钥')}</p>
        </div>
      </div>
      <div className="mc-model-toolbar">
        {
          <ModelCategoryTabs
            items={editorCategories}
            value={editCategory}
            onChange={selectEditorCategory}
            label={c('连接模型分类')}
            prefix="mc-edit-tab-"
            variant="pill"
          />
        }
        {
          <div className="mc-inline-actions">
            <Button
              type="button"
              variant="outline"
              disabled={busy || !draft.baseUrl}
              onClick={() => void discover()}
            >
              <Download />
              {c('获取模型列表')}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={busy || draft.models.length >= 100}
              onClick={() => {
                addModel(editCategory as ModelSettingsInput['category']);
              }}
            >
              <Plus />
              {c('手动添加')}
            </Button>
          </div>
        }
      </div>
      {directoryError ? (
        <p role="alert" className="mc-form-error">
          {directoryError}
        </p>
      ) : null}
      {directory !== null ? (
        <section className="mc-directory-import" aria-label={c('获取到的模型')}>
          <header>
            <div>
              <h3>{c('选择要导入的模型')}</h3>
              <p>{c('类别请按站点实际能力确认；导入后默认关闭，已有模型不会被覆盖。')}</p>
            </div>
            <button
              type="button"
              className="mc-text-button"
              disabled={busy}
              onClick={() => closeDiscovery()}
            >
              {c('收起')}
            </button>
          </header>
          <label className="mc-search">
            <Search aria-hidden />
            <input
              aria-label={c('搜索获取到的模型')}
              value={directorySearch}
              onChange={(e) => searchDiscovery(e.target.value)}
              placeholder={c('搜索模型 ID')}
            />
          </label>
          <div className="mc-discovered-list">
            {directory
              .filter((m) => m.modelId.toLowerCase().includes(directorySearch.toLowerCase()))
              .map((m) => {
                const exists = draft.models.some((old) => old.modelId === m.modelId);
                return (
                  <div key={m.modelId} className="mc-discovered-row">
                    <label>
                      <input
                        type="checkbox"
                        disabled={busy || exists}
                        checked={m.selected}
                        onChange={(e) =>
                          updateDiscoveredModel(m.modelId, { selected: e.target.checked })
                        }
                      />
                      <span>
                        {m.modelId}
                        {exists ? <small>{c('已添加')}</small> : null}
                      </span>
                    </label>
                    <ModelSelect
                      value={m.category}
                      options={capabilityOptions}
                      label={c('导入类别 {id}', {
                        id: m.modelId,
                      })}
                      disabled={busy || exists}
                      onChange={(value) =>
                        updateDiscoveredModel(m.modelId, {
                          category: value as ModelSettingsInput['category'],
                        })
                      }
                    />
                  </div>
                );
              })}
            {directory.length === 0 ? <p>{c('接口返回了空模型目录，可手动添加。')}</p> : null}
          </div>
          <Button
            type="button"
            disabled={busy || !directory.some((m) => m.selected)}
            onClick={importSelected}
          >
            {c('导入所选模型')}
          </Button>
        </section>
      ) : null}
      <div className="mc-edit-models mc-gateway-models">
        {draft.models.map((model, index) => {
          if (model.category !== editCategory) return null;
          const referenced = original?.models.find((m) => m.id === model.id)?.referenced;
          return (
            <article className="mc-edit-model mc-official-model" key={model.id || 'new-' + index}>
              <div className="mc-model-edit-header">
                {logoControl(model, index)}
                <strong>{modelName(model) || c('新模型')}</strong>
                {
                  <ModelSelect
                    value={model.category}
                    options={capabilityOptions}
                    label={c('模型类别 {index}', {
                      index: index + 1,
                    })}
                    onChange={(value) => {
                      patch(index, {
                        category: value as ModelSettingsInput['category'],
                        contextWindow: 0,
                        maxOutputTokens: 0,
                        pricing: null,
                      });
                      selectEditorCategory(value);
                    }}
                    disabled={busy || referenced}
                  />
                }
                <ModelSwitch
                  id={'mc-enable-' + index}
                  checked={model.enabled}
                  disabled={busy}
                  onChange={() =>
                    patch(index, {
                      enabled: !model.enabled,
                    })
                  }
                  label={c('启用 {name}', {
                    name: modelName(model) || c('新模型'),
                  })}
                />
                {
                  <button
                    type="button"
                    className="mc-icon-button"
                    aria-label={c('移除模型 {index}', {
                      index: index + 1,
                    })}
                    disabled={busy || referenced}
                    onClick={() => {
                      removeModel(index);
                    }}
                  >
                    <Trash2 />
                  </button>
                }
              </div>
              <div className="mc-fields-grid">
                <div className="mc-field">
                  <label htmlFor={'mc-model-id-' + index}>
                    {c('实际模型 ID')}
                    {null}
                  </label>
                  <input
                    id={'mc-model-id-' + index}
                    aria-label={c('实际模型 ID {index}', {
                      index: index + 1,
                    })}
                    value={model.modelId}
                    required
                    disabled={busy || referenced}
                    onChange={(e) =>
                      patch(index, {
                        modelId: e.target.value,
                      })
                    }
                    spellCheck={false}
                  />
                </div>
                <div className="mc-field">
                  <label htmlFor={'mc-alias-' + index}>{c('别名（可选）')}</label>
                  <input
                    id={'mc-alias-' + index}
                    aria-label={c('模型别名 {index}', {
                      index: index + 1,
                    })}
                    value={model.alias}
                    maxLength={120}
                    disabled={busy}
                    onChange={(e) =>
                      patch(index, {
                        alias: e.target.value,
                      })
                    }
                  />
                </div>
              </div>
              <div className="mc-field mc-model-description-field">
                <label htmlFor={'mc-description-' + index}>{c('模型描述（可选）')}</label>
                <input
                  id={'mc-description-' + index}
                  aria-label={c('模型描述 {index}', { index: index + 1 })}
                  value={model.description}
                  maxLength={400}
                  disabled={busy}
                  placeholder={c('简要介绍模型特点')}
                  onChange={(e) => patch(index, { description: e.target.value })}
                />
              </div>
              {model.category === 'analysis' ? (
                <button
                  type="button"
                  className="mc-profile-toggle"
                  aria-expanded={expanded.includes(index)}
                  onClick={() => toggleModelDetails(index)}
                >
                  {c('容量与文本计价')}
                  <ChevronDown aria-hidden />
                </button>
              ) : null}
              {model.category === 'analysis' && expanded.includes(index) ? (
                <div className="mc-model-profile">
                  <p>{c('按此连接的容量和最高单价预留预算；中转站不套用官方价格。')}</p>
                  <div className="mc-fields-grid">
                    {(['contextWindow', 'maxOutputTokens'] as const).map((key) => (
                      <div className="mc-field" key={key}>
                        <label htmlFor={'mc-' + key + '-' + index}>
                          {c(key === 'contextWindow' ? '上下文上限（token）' : '最大输出（token）')}
                        </label>
                        <input
                          id={'mc-' + key + '-' + index}
                          type="number"
                          min="0"
                          max="10000000"
                          step="1"
                          disabled={busy}
                          value={model[key] || ''}
                          onChange={(e) =>
                            patch(index, {
                              [key]: Number(e.target.value),
                            })
                          }
                        />
                      </div>
                    ))}
                  </div>
                  <div className="mc-price-grid">
                    {(
                      [
                        'inputCnyPerMillion',
                        'outputCnyPerMillion',
                        'cacheHitHundredthsCnyPerMillion',
                      ] as const
                    ).map((key, i) => (
                      <div className="mc-field" key={key}>
                        <label htmlFor={'mc-price-' + key + '-' + index}>
                          {c(['输入单价', '输出单价', '缓存输入单价'][i])}
                          <span>{c('元 / 百万 token')}</span>
                        </label>
                        <input
                          id={'mc-price-' + key + '-' + index}
                          type="number"
                          min="0"
                          step="any"
                          disabled={busy}
                          value={
                            model.pricing
                              ? key === 'cacheHitHundredthsCnyPerMillion'
                                ? model.pricing[key] / 100
                                : model.pricing[key]
                              : ''
                          }
                          onChange={(e) =>
                            pricing(
                              index,
                              key,
                              String(
                                Number(e.target.value) *
                                  (key === 'cacheHitHundredthsCnyPerMillion' ? 100 : 1),
                              ),
                            )
                          }
                        />
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}
              <ModelCreditPricingFields
                category={model.category}
                value={model.creditPrice}
                disabled={busy}
                onChange={(creditPrice) => patch(index, { creditPrice })}
              />
              {model.category !== 'analysis' ? (
                <small className="mc-model-media-state">{c('生成待接入')}</small>
              ) : null}
              {referenced ? (
                <p className="mc-reason">
                  {c('已被项目引用，实际模型 ID 与类别固定；仍可改别名或关闭。')}
                </p>
              ) : null}
            </article>
          );
        })}
        {!draft.models.some((m) => m.category === editCategory) ? (
          <p className="mc-model-empty">
            {c('该分类暂无模型')}。{c('可获取模型列表或手动添加。')}
          </p>
        ) : null}
      </div>
      {formError || conflict ? (
        <div role="alert" className="mc-form-error">
          {formError}
          {conflict ? (
            <Button
              type="button"
              variant="outline"
              disabled={busy || loading}
              onClick={() => void refreshDraft()}
            >
              {c('读取最新配置')}
            </Button>
          ) : null}
        </div>
      ) : null}
    </>
  );
}
