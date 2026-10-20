import { ModelCreditPricingFields } from './ModelCreditPricingFields';
import { ModelMockBehaviorFields } from './ModelMockBehaviorFields';
import { modelCategoryOptions } from './model-config-data';
import { ChevronDown, Eye, EyeOff, Server, Upload, RotateCcw, Pencil } from 'lucide-react';
import * as Menu from '@radix-ui/react-dropdown-menu';
import { type ModelSettingsInput } from '../../api/types';
import { Button } from '../../components/ui/button';

import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { ModelMark, ModelSwitch, ModelCategoryTabs } from './ModelConfigurationControls';
import { fallbackModelName, defaultLogo } from './model-config-data';
import type { ConnectionEditorView, GatewayEditorView } from './model-configuration-views';

import { GatewayConnectionFields } from './GatewayConnectionFields';
function modelDraft(model: ModelSettingsInput) {
  const { id, modelId, alias, category, enabled, contextWindow, maxOutputTokens, pricing } = model;
  const price = model.creditPrice;
  return {
    id,
    logoId: model.logoId || '',
    modelId,
    alias,
    description: model.description || '',
    category,
    enabled,
    contextWindow,
    maxOutputTokens,
    pricing,
    mockBehavior: model.mockBehavior ?? null,
    resolutionEnabled: model.resolutionEnabled ? { ...model.resolutionEnabled } : undefined,
    creditPrice: price ? { ...price } : null,
  };
}
// 官方连接与中转站共用同一草稿生命周期，弹窗只负责展示与显式动作。
export function ModelConnectionEditor({
  view,
  gateway,
}: {
  view: ConnectionEditorView;
  gateway: GatewayEditorView;
}) {
  const {
    c,
    loading,
    busy,
    open,
    draft,
    original,
    apiKey,
    updateCredential,
    showKey,
    toggleCredentialVisibility,
    formError,
    clearFormError,
    conflict,
    expanded,
    toggleModelDetails,
    editCategory,
    selectEditorCategory,
    activity,
    returnFocusId,
    editable,
    official,
    close,
    patch,
    save,
    refreshDraft,
    upload,
  } = view;
  const logoControl = (model: ModelSettingsInput, index: number) => (
    <>
      <Menu.Root>
        <Menu.Trigger asChild>
          <button
            type="button"
            className="mc-logo-trigger"
            disabled={busy}
            aria-label={c('修改模型 Logo {index}', {
              index: index + 1,
            })}
          >
            <ModelMark url={modelLogo(model)} category={model.category} />
            <Pencil className="mc-logo-pencil" aria-hidden />
          </button>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Content
            className={'xy-dropdown mc-logo-menu' + ' mc-official-logo-menu'}
            sideOffset={6}
            align="start"
            collisionPadding={12}
          >
            <Menu.Item
              className="xy-menu-row"
              onSelect={() => document.getElementById('mc-logo-file-' + index)?.click()}
            >
              <Upload />
              {c('自定义Logo')}
            </Menu.Item>
            <Menu.Item
              className="xy-menu-row"
              disabled={!model.logoId}
              onSelect={() =>
                patch(index, {
                  logoId: '',
                })
              }
            >
              <RotateCcw />
              {c('恢复默认')}
            </Menu.Item>
          </Menu.Content>
        </Menu.Portal>
      </Menu.Root>
      <input
        id={'mc-logo-file-' + index}
        type="file"
        hidden
        accept="image/png,image/jpeg"
        onChange={(e) => {
          void upload(index, e.target.files?.[0]);
          e.target.value = '';
        }}
      />
    </>
  );
  const modelName = (model: ModelSettingsInput) =>
    model.alias ||
    original?.models.find((m) => m.id === model.id)?.defaultName ||
    fallbackModelName(model);
  const modelLogo = (model: ModelSettingsInput) =>
    model.logoId
      ? '/api/model-logos/' + model.logoId
      : original?.models.find((m) => m.id === model.id)?.defaultLogoUrl || defaultLogo(model);
  const capabilityOptions = modelCategoryOptions(c);
  const editorCategories = official
    ? capabilityOptions.filter((o) => draft.models.some((m) => m.category === o.value))
    : capabilityOptions;
  const officialDirty =
    Boolean(apiKey) ||
    draft.models.some((model) => {
      const saved = original?.models.find((m) => m.id === model.id);
      return (
        saved &&
        (saved.alias !== model.alias ||
          (saved.description || '') !== model.description ||
          saved.enabled !== model.enabled ||
          JSON.stringify(saved.creditPrice ?? null) !== JSON.stringify(model.creditPrice ?? null) ||
          JSON.stringify(saved.resolutionEnabled ?? null) !==
            JSON.stringify(model.resolutionEnabled ?? null) ||
          JSON.stringify(saved.mockBehavior ?? null) !==
            JSON.stringify(model.mockBehavior ?? null) ||
          (saved.logoId || '') !== (model.logoId || ''))
      );
    });
  const editorDirty = official
    ? officialDirty
    : Boolean(apiKey) ||
      !original ||
      draft.name !== original.name ||
      draft.baseUrl !== original.baseUrl ||
      draft.protocol !== original.protocol ||
      JSON.stringify(draft.models.map(modelDraft)) !==
        JSON.stringify(original.models.map(modelDraft));
  const officialKeyMissing = Boolean(
    formError === c('请填写该 API 地址的 API Key') &&
      !original?.credentialConfigured &&
      !apiKey &&
      draft.models.some((m) => m.enabled),
  );
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) close();
      }}
    >
      <DialogContent
        variant="settings"
        className={
          'mc-editor mc-official-dialog' +
          (!official ? ' mc-gateway-dialog' : '') +
          (official && editCategory !== 'analysis' ? ' mc-media-dialog' : '') +
          (draft.kind === 'builtin' ? ' mc-mock-dialog' : '')
        }
        closeDisabled={busy}
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          document
            .getElementById(
              draft.kind === 'builtin'
                ? 'mc-alias-' + draft.models.findIndex((model) => model.category === editCategory)
                : official
                  ? 'model-api-key'
                  : 'mc-connection-name',
            )
            ?.focus({
              preventScroll: true,
            });
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          document
            .getElementById(
              returnFocusId || (original ? 'mc-config-' + original.id : 'mc-new-gateway'),
            )
            ?.focus({
              preventScroll: true,
            });
        }}
      >
        <header className="mc-official-dialog-header">
          <div className="mc-model-mark">
            {official ? (
              <ModelMark
                url={
                  original?.models[0]?.defaultLogoUrl ||
                  (draft.models[0] ? defaultLogo(draft.models[0]) : '')
                }
                category="analysis"
              />
            ) : (
              <Server aria-hidden />
            )}
          </div>
          <div>
            <DialogTitle>
              {official ? draft.name : c(original ? '配置中转站' : '新增中转站')}
            </DialogTitle>
            <DialogDescription>
              {c(
                draft.kind === 'builtin'
                  ? '本地模拟 · 无需 API Key'
                  : official
                    ? '官方模型'
                    : '中转站',
              )}
            </DialogDescription>
          </div>
        </header>
        <form onSubmit={save} className="mc-official-form">
          <div className="mc-official-body">
            {official ? (
              <>
                {draft.kind !== 'builtin' ? (
                  <div className="mc-official-credentials">
                    <div className="mc-field">
                      <label htmlFor="model-base-url">{c('API 地址')}</label>
                      <input
                        id="model-base-url"
                        value={draft.baseUrl}
                        disabled
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
                            clearFormError();
                          }}
                          type={showKey ? 'text' : 'password'}
                          placeholder={c(
                            original?.credentialConfigured ? '不填写则保留原密钥' : '填写 API Key',
                          )}
                          disabled={busy}
                          autoComplete="off"
                          spellCheck={false}
                          aria-invalid={officialKeyMissing || undefined}
                          aria-describedby={
                            officialKeyMissing ? 'mc-official-key-error' : 'mc-official-key-hint'
                          }
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
                      {officialKeyMissing ? (
                        <p
                          id="mc-official-key-error"
                          role="alert"
                          className="mc-official-field-error"
                        >
                          {formError}
                        </p>
                      ) : (
                        <p id="mc-official-key-hint">{c('所有模型共用此密钥')}</p>
                      )}
                    </div>
                  </div>
                ) : null}
                <div className="mc-official-model-toolbar">
                  {editorCategories.length > 1 ? (
                    <ModelCategoryTabs
                      items={editorCategories}
                      value={editCategory}
                      onChange={selectEditorCategory}
                      label={c('连接模型分类')}
                      prefix="mc-edit-tab-"
                      variant="pill"
                    />
                  ) : null}
                </div>
                <div className="mc-official-models">
                  {draft.models.map((model, index) =>
                    model.category === editCategory ? (
                      <article
                        key={model.id}
                        className="mc-official-model"
                        data-media={model.category !== 'analysis'}
                      >
                        <div className="mc-model-edit-header">
                          {logoControl(model, index)}
                          <strong>{modelName(model)}</strong>
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
                              name: modelName(model),
                            })}
                          />
                        </div>
                        <div className="mc-model-identity">
                          <div className="mc-fields-grid">
                            <div className="mc-field">
                              <label htmlFor={'mc-model-id-' + index}>{c('实际模型 ID')}</label>
                              <input
                                id={'mc-model-id-' + index}
                                aria-label={c('实际模型 ID {index}', {
                                  index: index + 1,
                                })}
                                value={model.modelId}
                                disabled
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
                                placeholder={modelName({
                                  ...model,
                                  alias: '',
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
                            <label htmlFor={'mc-description-' + index}>
                              {c('模型描述（可选）')}
                            </label>
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
                          {draft.kind === 'builtin' ? (
                            <ModelMockBehaviorFields
                              value={model.mockBehavior}
                              index={index}
                              disabled={busy || !editable}
                              onChange={(mockBehavior) => {
                                patch(index, { mockBehavior });
                                clearFormError();
                              }}
                            />
                          ) : null}
                        </div>
                        <ModelCreditPricingFields
                          resolutionEnabled={model.resolutionEnabled ?? undefined}
                          onResolutionEnabledChange={(resolutionEnabled) =>
                            patch(index, { resolutionEnabled })
                          }
                          profile={original?.models.find((m) => m.id === model.id)?.mediaProfile}
                          category={model.category}
                          value={model.creditPrice}
                          disabled={busy}
                          onChange={(creditPrice) => patch(index, { creditPrice })}
                        />
                        {model.contextWindow > 0 || model.maxOutputTokens > 0 || model.pricing ? (
                          <>
                            <button
                              type="button"
                              id={'mc-parameters-toggle-' + index}
                              className="mc-profile-toggle"
                              aria-expanded={expanded.includes(index)}
                              aria-controls={'mc-model-parameters-' + index}
                              aria-label={c('模型参数') + ' · ' + modelName(model)}
                              disabled={busy}
                              onClick={() => toggleModelDetails(index)}
                            >
                              {c('模型参数')}
                              <ChevronDown aria-hidden />
                            </button>
                            {expanded.includes(index) ? (
                              <div
                                id={'mc-model-parameters-' + index}
                                className="mc-readonly-parameters"
                              >
                                <dl className="mc-parameter-capacity">
                                  {model.contextWindow > 0 ? (
                                    <div>
                                      <dt>{c('上下文上限（token）')}</dt>
                                      <dd>{model.contextWindow.toLocaleString()}</dd>
                                    </div>
                                  ) : null}
                                  {model.maxOutputTokens > 0 ? (
                                    <div>
                                      <dt>{c('最大输出（token）')}</dt>
                                      <dd>{model.maxOutputTokens.toLocaleString()}</dd>
                                    </div>
                                  ) : null}
                                </dl>
                                {model.pricing ? (
                                  <div className="mc-catalog-pricing">
                                    <span className="mc-catalog-unit">{c('元 / 百万 token')}</span>
                                    <dl>
                                      {(
                                        [
                                          ['输入单价', model.pricing.inputCnyPerMillion],
                                          ['输出单价', model.pricing.outputCnyPerMillion],
                                          [
                                            '缓存输入单价',
                                            model.pricing.cacheHitHundredthsCnyPerMillion / 100,
                                          ],
                                        ] as const
                                      ).map(([label, value]) => (
                                        <div key={label}>
                                          <dt>{c(label)}</dt>
                                          <dd>
                                            ¥
                                            {value.toLocaleString(undefined, {
                                              maximumFractionDigits: 2,
                                            })}
                                          </dd>
                                        </div>
                                      ))}
                                    </dl>
                                  </div>
                                ) : null}
                              </div>
                            ) : null}
                          </>
                        ) : null}
                      </article>
                    ) : null,
                  )}
                </div>
              </>
            ) : (
              <GatewayConnectionFields
                view={gateway}
                logoControl={logoControl}
                modelName={modelName}
                capabilityOptions={capabilityOptions}
                editorCategories={editorCategories}
              />
            )}
            {official && ((formError && !officialKeyMissing) || conflict) ? (
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
          </div>
          <footer className="mc-official-footer">
            <span className="mc-official-save-state" role="status">
              {busy
                ? activity
                : editorDirty &&
                    (official || original || draft.name || draft.baseUrl || draft.models.length)
                  ? c('有未保存的修改')
                  : ''}
            </span>
            <div className="mc-inline-actions">
              <Button type="button" variant="outline" disabled={busy} onClick={close}>
                {c('取消')}
              </Button>
              <Button
                type="submit"
                disabled={
                  !editable ||
                  !editorDirty ||
                  (draft.kind === 'builtin' && draft.models.some((model) => !model.mockBehavior))
                }
                busy={busy}
              >
                {c('保存')}
              </Button>
            </div>
          </footer>
        </form>
      </DialogContent>
    </Dialog>
  );
}
