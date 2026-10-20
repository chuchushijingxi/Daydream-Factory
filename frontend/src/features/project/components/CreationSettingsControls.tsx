import { protectedResourceUrl } from '../../../api/client';
import { ModelPriceLabel } from '../../billing/useChargeQuote';
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useCatalogText } from '../../../contexts/useCatalogText';
import * as Menu from '@radix-ui/react-dropdown-menu';
import {
  BrainCircuit,
  Check,
  ChevronDown,
  Clapperboard,
  Image as ImageIcon,
  Layers,
  RectangleHorizontal,
  RectangleVertical,
  Square,
} from 'lucide-react';
import type { ModelOption, ProjectSettings, VisualStyle } from '../../../api/types';
import { useWorkspace } from '../../../state/WorkspaceContext';
import { StylePicker } from './StylePicker';
export const EMPTY_CREATION_SETTINGS: ProjectSettings = {
  aspectRatio: '',
  llmModel: '',
  imageModel: '',
  videoModel: '',
};
const ratios = [
  {
    id: '9:16',
    name: '9:16（竖屏）',
    Icon: RectangleVertical,
  },
  {
    id: '16:9',
    name: '16:9（横屏）',
    Icon: RectangleHorizontal,
  },
  {
    id: '4:3',
    name: '4:3',
    Icon: RectangleHorizontal,
  },
  {
    id: '1:1',
    name: '1:1',
    Icon: Square,
  },
];
const deepseekLogo = '/demo/assets/providers/deepseek.svg';
const isDeepSeek = (id: string) => id === 'deepseek-flash' || id === 'deepseek-v4-pro';
export function CreationSettingsControls({
  value,
  onChange,
  style,
  onStyleChange,
  disabled = false,
  analysisLocked = false,
  parametersLocked = false,
  confirmed,
  styleLocked = false,
}: {
  value: ProjectSettings;
  onChange: (key: keyof ProjectSettings, value: string) => void;
  style: VisualStyle | null;
  onStyleChange: (style: VisualStyle) => void;
  disabled?: boolean;
  analysisLocked?: boolean;
  parametersLocked?: boolean;
  confirmed?: ProjectSettings | null;
  styleLocked?: boolean;
}) {
  const { c } = useCatalogText();
  const { models } = useWorkspace(),
    RatioIcon = ratios.find((r) => r.id === value.aspectRatio)?.Icon;
  const categories = [
    {
      key: 'llmModel' as const,
      label: c('分析模型'),
      options: models.analysis,
      Icon: BrainCircuit,
      locked: analysisLocked,
    },
    {
      key: 'imageModel' as const,
      label: c('图像模型'),
      options: models.image,
      Icon: ImageIcon,
      locked: false,
    },
    {
      key: 'videoModel' as const,
      label: c('视频模型'),
      options: models.video,
      Icon: Clapperboard,
      locked: false,
    },
  ];
  const [panelOpen, setPanelOpen] = useState(false),
    [panelPos, setPanelPos] = useState<{ left: number; top: number; width: number } | null>(null),
    [activeKey, setActiveKey] = useState<keyof ProjectSettings>('llmModel'),
    pickerRef = useRef<HTMLDivElement>(null),
    panelRef = useRef<HTMLDivElement>(null);
  const lockedFor = (key: keyof ProjectSettings) =>
    disabled ||
    parametersLocked ||
    Boolean(confirmed?.[key]) ||
    (key === 'llmModel' && analysisLocked);
  const displayName = (key: keyof ProjectSettings) => {
    const saved = value[key];
    if (!saved) return '';
    const category = categories.find((cat) => cat.key === key);
    const option = category?.options.find((o) => o.id === saved);
    return option?.name || c('{id}（不可用）', { id: saved });
  };
  const selected = categories.map((cat) => displayName(cat.key)).filter(Boolean);
  const summary =
    selected.length === 0
      ? c('模型')
      : selected.length === 1
        ? selected[0]
        : c('模型 {count}/3', { count: selected.length });
  useEffect(() => {
    if (!panelOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      const node = event.target instanceof Node ? event.target : null;
      if (node && (panelRef.current?.contains(node) || pickerRef.current?.contains(node))) return;
      setPanelOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        setPanelOpen(false);
        pickerRef.current?.querySelector('button')?.focus({ preventScroll: true });
      }
    };
    // 页面滚动/缩放时面板跟随锚点重新定位，不关闭。
    const reposition = () => {
      const rect = pickerRef.current?.getBoundingClientRect();
      if (!rect) return;
      const width = Math.min(460, window.innerWidth - 24);
      setPanelPos({
        left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)),
        top: rect.bottom + 6,
        width,
      });
    };
    window.addEventListener('resize', reposition);
    document.addEventListener('scroll', reposition, true);
    document.addEventListener('pointerdown', onPointerDown);
    window.addEventListener('keydown', onKey, true);
    return () => {
      window.removeEventListener('resize', reposition);
      document.removeEventListener('scroll', reposition, true);
      document.removeEventListener('pointerdown', onPointerDown);
      window.removeEventListener('keydown', onKey, true);
    };
  }, [panelOpen]);
  const openPanel = () => {
    const rect = pickerRef.current!.getBoundingClientRect();
    const width = Math.min(460, window.innerWidth - 24);
    // 与另外两个下拉一致：锚定触发器左缘、向下展开。
    setPanelPos({
      left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)),
      top: rect.bottom + 6,
      width,
    });
    setActiveKey(!value.llmModel ? 'llmModel' : !value.imageModel ? 'imageModel' : 'videoModel');
    setPanelOpen(true);
  };
  const activeCategory = categories.find((cat) => cat.key === activeKey) || categories[0];
  const activeOptions: ModelOption[] = activeCategory.options;
  const savedActive = value[activeCategory.key];
  const activeOption = activeOptions.find((o) => o.id === savedActive);
  const unavailableDisplay = activeOption;
  const activeUnavailable = !!savedActive && !activeOption;
  return (
    <div className="xy-creation-settings" aria-label={c('创作参数')}>
      <StylePicker value={style} onChange={onStyleChange} disabled={disabled || styleLocked} />
      <Menu.Root>
        <Menu.Trigger asChild>
          <button
            type="button"
            className="xy-parameter xy-parameter--ratio"
            aria-label={c('比例')}
            disabled={
              disabled || analysisLocked || parametersLocked || Boolean(confirmed?.aspectRatio)
            }
          >
            {RatioIcon ? <RatioIcon aria-hidden /> : null}
            <span>{value.aspectRatio || c('比例')}</span>
            <ChevronDown aria-hidden />
          </button>
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Content
            sideOffset={4}
            align="start"
            collisionPadding={12}
            className="xy-dropdown xy-choice-dropdown xy-ratio-dropdown"
            aria-label={c('画面比例')}
          >
            <Menu.RadioGroup
              value={value.aspectRatio}
              onValueChange={(v) => onChange('aspectRatio', v)}
            >
              {ratios.map(({ id, name }) => (
                <Menu.RadioItem
                  className="xy-menu-row xy-choice-row xy-ratio-choice"
                  key={id}
                  value={id}
                  textValue={c(name)}
                >
                  <span className="xy-ratio-preview" aria-hidden>
                    <span
                      style={{
                        aspectRatio: id.replace(':', '/'),
                        width: id === '9:16' ? 9 : 16,
                      }}
                    />
                  </span>
                  <span className="xy-ratio-name">{c(name)}</span>
                  <Menu.ItemIndicator className="xy-menu-check">
                    <Check aria-hidden />
                  </Menu.ItemIndicator>
                </Menu.RadioItem>
              ))}
            </Menu.RadioGroup>
          </Menu.Content>
        </Menu.Portal>
      </Menu.Root>
      <div className="xy-model-picker" ref={pickerRef}>
        <button
          type="button"
          className="xy-parameter xy-parameter--model"
          aria-label={c('模型')}
          aria-expanded={panelOpen}
          disabled={disabled}
          onClick={() => (panelOpen ? setPanelOpen(false) : openPanel())}
        >
          <Layers aria-hidden />
          <span>{summary}</span>
          <ChevronDown aria-hidden />
        </button>
        {panelOpen && panelPos
          ? createPortal(
              <div
                className="xy-model-panel"
                ref={panelRef}
                role="dialog"
                aria-label={c('选择模型')}
                style={{
                  position: 'fixed',
                  left: panelPos.left,
                  top: panelPos.top,
                  width: panelPos.width,
                }}
              >
                <div className="xy-model-panel-side">
                  <div className="xy-model-panel-rail">
                    {categories.map((cat) => {
                      const CatIcon = cat.Icon;
                      return (
                        <button
                          type="button"
                          key={cat.key}
                          className="xy-model-cat"
                          data-active={activeCategory.key === cat.key}
                          aria-label={cat.label}
                          onClick={() => setActiveKey(cat.key)}
                        >
                          <span className="xy-model-cat-dot" aria-hidden />
                          <CatIcon aria-hidden />
                          <div>
                            <span>{cat.label}</span>
                            <small>{displayName(cat.key) || c('未指定')}</small>
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
                <div className="xy-model-panel-list">
                  {activeCategory.options.length === 0 && !activeUnavailable ? (
                    <p className="xy-model-panel-empty">
                      {c('暂无可用{label}', { label: activeCategory.label })}
                    </p>
                  ) : null}
                  <button
                    type="button"
                    className="xy-menu-row xy-choice-row xy-model-row"
                    data-disabled={lockedFor(activeCategory.key) || undefined}
                    aria-pressed={savedActive === ''}
                    onClick={() => {
                      if (lockedFor(activeCategory.key)) return;
                      onChange(activeCategory.key, '');
                    }}
                  >
                    <span className="xy-model-mark" aria-hidden>
                      <activeCategory.Icon />
                    </span>
                    <span className="xy-choice-name">{c('未指定')}</span>
                    {savedActive === '' ? (
                      <span className="xy-menu-check">
                        <Check aria-hidden />
                      </span>
                    ) : null}
                  </button>
                  {activeOptions.map((o) => {
                    const rowLocked = lockedFor(activeCategory.key);
                    return (
                      <button
                        type="button"
                        key={o.id}
                        className="xy-menu-row xy-choice-row xy-model-row"
                        data-disabled={rowLocked || undefined}
                        aria-pressed={savedActive === o.id}
                        onClick={() => {
                          if (rowLocked) return;
                          onChange(activeCategory.key, o.id);
                        }}
                      >
                        <span className="xy-model-mark" aria-hidden>
                          {o.logoUrl || o.provider === 'deepseek' || isDeepSeek(o.id) ? (
                            <img src={protectedResourceUrl(o.logoUrl || deepseekLogo)} alt="" />
                          ) : (
                            <activeCategory.Icon />
                          )}
                        </span>
                        <span className="xy-choice-name xy-model-details">
                          <span className="xy-model-heading">
                            <span>{o.name}</span>
                            <span className="xy-model-charge">
                              <ModelPriceLabel
                                model={o}
                                category={
                                  activeCategory.key as 'llmModel' | 'imageModel' | 'videoModel'
                                }
                              />
                            </span>
                          </span>
                          {o.description ? (
                            <small className="xy-model-description" title={o.description}>
                              {o.description}
                            </small>
                          ) : null}
                        </span>
                        {savedActive === o.id ? (
                          <span className="xy-menu-check">
                            <Check aria-hidden />
                          </span>
                        ) : null}
                      </button>
                    );
                  })}
                  {activeUnavailable ? (
                    <button
                      type="button"
                      className="xy-menu-row xy-choice-row xy-model-row"
                      data-disabled="true"
                    >
                      <span className="xy-model-mark" aria-hidden>
                        {unavailableDisplay?.logoUrl ||
                        unavailableDisplay?.provider === 'deepseek' ||
                        isDeepSeek(savedActive) ? (
                          <img
                            src={protectedResourceUrl(unavailableDisplay?.logoUrl || deepseekLogo)}
                            alt=""
                          />
                        ) : (
                          <activeCategory.Icon />
                        )}
                      </span>
                      <span className="xy-choice-name">
                        {c('{id}（不可用）', {
                          id: unavailableDisplay?.name || savedActive,
                        })}
                      </span>
                    </button>
                  ) : null}
                </div>
              </div>,
              pickerRef.current?.closest('[role="dialog"]') || document.body,
            )
          : null}
      </div>
    </div>
  );
}
