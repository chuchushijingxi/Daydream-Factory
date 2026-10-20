import { useCatalogText } from '../../../contexts/useCatalogText';
import { cn } from '../../../components/ui/utils';
type PlayletImageNoPreviewPlaceholderProps = {
  className?: string;
};

/** 角色/形象无预览占位：64×56 图 + 12px 间距 +「暂无预览」 */
export function ImageUnavailablePlaceholder({ className }: PlayletImageNoPreviewPlaceholderProps) {
  const { c } = useCatalogText();
  return (
    <div className={cn('flex flex-col items-center', className)}>
      <img
        src="/demo/app/images/placeholder.png"
        alt=""
        width={64}
        height={56}
        className="h-[56px] w-[64px] shrink-0 object-contain"
        draggable={false}
      />
      <p className="mt-[12px] text-[12px] font-normal leading-normal text-[var(--jm-muted)]">
        {c('暂无预览')}
      </p>
    </div>
  );
}
