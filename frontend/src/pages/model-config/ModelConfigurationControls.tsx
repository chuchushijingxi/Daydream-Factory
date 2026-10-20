import { protectedResourceUrl } from '../../api/client';
import { Check, ChevronDown, Image, Video, BrainCircuit } from 'lucide-react';
import * as Menu from '@radix-ui/react-dropdown-menu';

export function ModelMark({ url, category }: { url?: string; category: string }) {
  const Icon = category === 'image' ? Image : category === 'video' ? Video : BrainCircuit;
  return url ? <img src={protectedResourceUrl(url)} alt="" /> : <Icon aria-hidden />;
}

export function ModelSwitch({
  checked,
  disabled,
  onChange,
  label,
  id,
}: {
  checked: boolean;
  disabled: boolean;
  onChange: () => void;
  label: string;
  id: string;
}) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-label={label}
      aria-checked={checked}
      disabled={disabled}
      onClick={onChange}
      className="mc-switch"
    >
      <span />
    </button>
  );
}

export function ModelSelect({
  value,
  options,
  onChange,
  label,
  disabled = false,
  menuClassName = '',
}: {
  value: string;
  options: {
    value: string;
    label: string;
  }[];
  onChange: (value: string) => void;
  label: string;
  disabled?: boolean;
  menuClassName?: string;
}) {
  return (
    <Menu.Root>
      <Menu.Trigger asChild>
        <button type="button" className="mc-select" aria-label={label} disabled={disabled}>
          <span>{options.find((o) => o.value === value)?.label}</span>
          <ChevronDown aria-hidden />
        </button>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Content
          className={`xy-dropdown mc-select-menu ${menuClassName}`}
          sideOffset={4}
          collisionPadding={12}
          align="start"
        >
          <Menu.RadioGroup value={value} onValueChange={onChange}>
            {options.map((o) => (
              <Menu.RadioItem className="xy-menu-row" key={o.value} value={o.value}>
                {o.label}
                <Menu.ItemIndicator className="xy-menu-check">
                  <Check aria-hidden />
                </Menu.ItemIndicator>
              </Menu.RadioItem>
            ))}
          </Menu.RadioGroup>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  );
}

export { ViewTabs as ModelCategoryTabs } from '../../components/ui/view-tabs';
