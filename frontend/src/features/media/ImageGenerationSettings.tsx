import { useId } from 'react';
import { ChevronDown, Image } from 'lucide-react';
import type { CharacterImageType } from '../../api/types';
import { useCatalogText } from '../../contexts/useCatalogText';
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent,
  DropdownMenuRadioGroup, DropdownMenuRadioItem,
} from '../../components/ui/dropdown-menu';

const imageTypes: { id: CharacterImageType; label: string; description: string; image?: string }[] = [
  { id: 'closeup', label: '特写', description: '面部与上半身特写', image: 'closeup-sample' },
  { id: 'main_view', label: '主视图', description: '展示全身比例、服装和整体造型', image: 'main-view-sample' },
  { id: 'closeup_main', label: '特写 + 主视图', description: '兼顾脸部识别与整体造型', image: 'closeup-main-sample' },
  { id: 'three_view', label: '三视图', description: '正面、侧面、背面角色设定', image: 'three-view-sample' },
  { id: 'closeup_three_view', label: '特写 + 三视图', description: '特写与三视图同图输出', image: 'closeup-three-view-sample' },
  { id: 'no_preset', label: '无预设提示词', description: '不额外限定视角或构图' },
];
function CharacterImageTypeSelect({ value, disabled, onChange }: {
  value: CharacterImageType;
  disabled: boolean;
  onChange: (value: CharacterImageType) => void;
}) {
  const labelId = useId();
  const { c } = useCatalogText();
  const selected = imageTypes.find(option => option.id === value)!;
  const content = (option: typeof selected) => <>
    <span className="character-image-type-preview">
      {option.image ? <img src={`/demo/app/images/image-types/${option.image}.webp`} alt="" /> : <Image aria-hidden="true" />}
    </span>
    <span className="character-image-type-copy"><strong>{c(option.label)}</strong><small>{c(option.description)}</small></span>
  </>;
  return <div className="character-image-type-field">
    <label id={labelId}>{c('生图类型')}</label>
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <button type="button" className="character-image-type-trigger" aria-label={c('生图类型')}>
          {content(selected)}<ChevronDown aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="character-image-type-menu" align="start" collisionPadding={16} aria-labelledby={labelId}>
        <DropdownMenuRadioGroup value={value} onValueChange={next => onChange(next as CharacterImageType)}>
          {imageTypes.map(option => <DropdownMenuRadioItem key={option.id} value={option.id} aria-label={c(option.label)}>
            {content(option)}
          </DropdownMenuRadioItem>)}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>;
}

export function lockedCharacterRatio(imageType?: CharacterImageType) {
  return imageType === 'closeup_main' || imageType === 'three_view' || imageType === 'closeup_three_view';
}

export function ImageGenerationSettings({ imageType, aspectRatio, aspectRatios, disabled, onImageTypeChange, onAspectRatioChange }: {
  imageType?: CharacterImageType;
  aspectRatio: string;
  aspectRatios: string[];
  disabled: boolean;
  onImageTypeChange: (value: CharacterImageType) => void;
  onAspectRatioChange: (value: string) => void;
}) {
  const locked = lockedCharacterRatio(imageType);
  const { c } = useCatalogText();
  return <>
    {imageType ? <CharacterImageTypeSelect value={imageType} disabled={disabled} onChange={onImageTypeChange} /> : null}
    <div className="character-image-type-field image-aspect-ratio-field">
      <label>{c('比例')}</label>
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={disabled || locked || aspectRatios.length < 2}>
          <button type="button" className="character-image-type-trigger image-aspect-ratio-trigger" aria-label={c('图片比例')}>
            <span>{aspectRatio}{aspectRatio === '16:9' ? ' ' + c('横屏') : aspectRatio === '9:16' ? ' ' + c('竖屏') : aspectRatio === '1:1' ? ' ' + c('方形') : ''}</span>
            {!locked && aspectRatios.length > 1 ? <ChevronDown aria-hidden="true" /> : null}
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="character-image-type-menu image-aspect-ratio-menu" align="start" collisionPadding={16} aria-label={c('图片比例')}>
          <DropdownMenuRadioGroup value={aspectRatio} onValueChange={onAspectRatioChange}>
            {aspectRatios.map(ratio => <DropdownMenuRadioItem key={ratio} value={ratio}>{ratio}</DropdownMenuRadioItem>)}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  </>;
}
