import * as Menu from '@radix-ui/react-dropdown-menu';
import { useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { ChevronDown, Palette } from 'lucide-react';
import { stylePreviewUrl } from '../style-preview';
import type { VisualStyle } from '../../../api/types';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { useWorkspace } from '../../../state/WorkspaceContext';

export function StylePicker({
  value,
  onChange,
  disabled = false,
}: {
  value: VisualStyle | null;
  onChange: (style: VisualStyle) => void;
  disabled?: boolean;
}) {
  const { styles } = useWorkspace();
  const { c, styleName, styleDescription } = useCatalogText();
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [placement, setPlacement] = useState({ offset: 0, height: 0 });
  useLayoutEffect(() => {
    if (!open) return;
    // 保留向下展开，仅校正横向溢出和下方可用高度，避免自动翻到按钮上方。
    const update = () => {
      const rect = trigger.current?.getBoundingClientRect();
      if (!rect) return;
      const width = Math.min(560, window.innerWidth - 24);
      const left = Math.max(12, Math.min(rect.left, window.innerWidth - width - 12));
      setPlacement({
        offset: left - rect.left,
        height: Math.max(0, window.innerHeight - rect.bottom - 18),
      });
    };
    update();
    window.addEventListener('resize', update);
    document.addEventListener('scroll', update, true);
    return () => {
      window.removeEventListener('resize', update);
      document.removeEventListener('scroll', update, true);
    };
  }, [open]);
  return (
    <Menu.Root open={open} onOpenChange={setOpen} modal={false}>
      <Menu.Trigger asChild>
        <button
          ref={trigger}
          type="button"
          className="xy-parameter xy-parameter--style"
          aria-label={c('选择项目风格')}
          disabled={disabled}
        >
          {value ? (
            <img src={stylePreviewUrl(value.previewUrl)} alt="" />
          ) : (
            <Palette aria-hidden />
          )}
          <span>{value ? styleName(value) : c('风格')}</span>
          <ChevronDown aria-hidden />
        </button>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content
          side="bottom"
          align="start"
          sideOffset={6}
          avoidCollisions={false}
          alignOffset={placement.offset}
          className="xy-dropdown xy-choice-dropdown xy-style-dropdown"
          aria-label={c('选择风格')}
          style={{ '--style-available-height': `${placement.height}px` } as CSSProperties}
        >
          <div className="xy-style-scroll">
            <Menu.RadioGroup
              className="xy-style-grid"
              value={value?.id || ''}
              onValueChange={(id) => {
                const option = styles.find((style) => style.id === id);
                if (option && !disabled) onChange(option);
              }}
            >
              {styles.map((option) => {
                const name = styleName(option);
                const description = styleDescription(option);
                return (
                  <Menu.RadioItem
                    key={option.id}
                    value={option.id}
                    textValue={name}
                    disabled={disabled}
                    className="xy-style-option"
                    title={description ? `${name}\n${description}` : name}
                    aria-label={name}
                    aria-description={description || undefined}
                  >
                    <span className="xy-style-thumbnail">
                      <img
                        src={stylePreviewUrl(option.previewUrl)}
                        alt=""
                        loading="lazy"
                        draggable={false}
                      />
                    </span>
                    <span className="xy-style-name">{name}</span>
                  </Menu.RadioItem>
                );
              })}
            </Menu.RadioGroup>
            {styles.length === 0 ? <p className="xy-style-empty">{c('暂无可用风格')}</p> : null}
          </div>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}
