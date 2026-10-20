import { useCatalogText } from '../../contexts/useCatalogText';

// 限免模型只展示活动标签，不再显示 0 积分：0 会被误读为一次真实扣费或免费额度。
export function FreeBadge({ className }: { className?: string }) {
  const { c } = useCatalogText();
  return (
    <span className={className ? 'xy-model-free-badge ' + className : 'xy-model-free-badge'}>
      {c('限免')}
    </span>
  );
}
