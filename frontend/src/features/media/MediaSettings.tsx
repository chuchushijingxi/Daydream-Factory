import { ChevronDown } from 'lucide-react';
import type { MediaOptions, MediaProfile } from '../../api/types';
import { useCatalogText } from '../../contexts/useCatalogText';
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from '../../components/ui/dropdown-menu';
import { mediaResolutionOptions, highestMediaResolution } from './media-resolution-options';
import { DurationParameter } from './DurationParameter';
export function selectMediaOptions(
  kind: 'image' | 'video',
  profile: MediaProfile | null | undefined,
  draft?: MediaOptions,
  estimatedDurationSeconds?: number | null,
): MediaOptions | undefined {
  if (
    !profile ||
    profile.retired ||
    !profile.resolutions.some((r) => r.id === profile.defaultResolution)
  )
    return;
  const resolutions = mediaResolutionOptions(kind, profile);
  const resolution = resolutions.some((r) => r.id === draft?.resolution)
    ? draft!.resolution
    : kind === 'image'
      ? highestMediaResolution(resolutions)
      : resolutions.some((r) => r.id === profile.defaultResolution)
        ? profile.defaultResolution
        : resolutions[0].id;
  if (kind === 'video' && profile.durations?.length) {
    if (
      profile.defaultDuration === undefined ||
      !profile.durations.includes(profile.defaultDuration)
    )
      return;
    const durationSeconds = profile.durations.includes(draft?.durationSeconds as number)
      ? draft!.durationSeconds
      : Number.isFinite(estimatedDurationSeconds) && estimatedDurationSeconds! > 0
        ? profile.durations.filter(seconds => seconds >= Math.ceil(estimatedDurationSeconds!))
            .sort((a, b) => a - b)[0]
        : profile.defaultDuration;
    if (durationSeconds === undefined) return;
    return {
      resolution,
      durationSeconds,
    };
  }
  return { resolution };
}
function Parameter({
  label,
  value,
  items,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  items: { id: string; label: string }[];
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="sb-choice media-parameter-trigger"
          aria-label={label}
          disabled={disabled}
        >
          <span>{items.find((i) => i.id === value)?.label || label}</span>
          <ChevronDown size={12} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="sb-command-menu media-parameter-menu"
        align="start"
        collisionPadding={12}
        aria-label={label}
      >
        <DropdownMenuRadioGroup value={value} onValueChange={onChange}>
          {items.map((i) => (
            <DropdownMenuRadioItem key={i.id} value={i.id}>
              {i.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
export function MediaSettings({
  kind,
  profile,
  value,
  disabled,
  frozen,
  onChange,
}: {
  kind: 'image' | 'video';
  profile?: MediaProfile | null;
  value: MediaOptions | undefined;
  disabled: boolean;
  frozen?: Partial<MediaOptions>;
  onChange: (v: MediaOptions) => void;
}) {
  const { c } = useCatalogText();
  if (frozen)
    return (
      <div
        className="media-shot-settings"
        aria-label={kind === 'video' ? c('视频参数') : c('图片参数')}
      >
        {frozen.resolution ? (
          <>
            <span aria-label={c('分辨率')}>{frozen.resolution.toUpperCase()}</span>
            {kind === 'video' && frozen.durationSeconds !== undefined ? (
              <span aria-label={c('时长')}>
                {c('{seconds} 秒', { seconds: Number(frozen.durationSeconds.toFixed(2)) })}
              </span>
            ) : null}
          </>
        ) : (
          <span>{c('媒体规格尚未核实')}</span>
        )}
      </div>
    );
  if (!profile || !value)
    return (
      <div className="media-shot-settings">
        <span>{c('媒体规格尚未核实')}</span>
      </div>
    );
  const resolutions = mediaResolutionOptions(kind, profile);
  return (
    <div
      className="media-shot-settings"
      aria-label={kind === 'video' ? c('视频参数') : c('图片参数')}
    >
      <Parameter
        label={c('分辨率')}
        value={value.resolution}
        items={resolutions.map((r) => ({
          id: r.id,
          label: kind === 'video' ? r.label.toUpperCase() : r.label,
        }))}
        disabled={disabled || profile.retired || resolutions.length === 1}
        onChange={(resolution) => onChange({ ...value, resolution })}
      />
      {kind === 'video' ? (
        profile.durations?.length && value.durationSeconds !== undefined ? (
          <DurationParameter
            value={value.durationSeconds}
            durations={profile.durations}
            disabled={disabled || profile.retired || profile.durations.length === 1}
            onChange={(durationSeconds) => onChange({ ...value, durationSeconds })}
          />
        ) : (
          <span>
            {c('时长')} <strong>{c('跟随输入视频')}</strong>
          </span>
        )
      ) : null}
    </div>
  );
}
