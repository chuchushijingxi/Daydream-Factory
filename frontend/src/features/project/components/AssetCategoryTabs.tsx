import { useCatalogText } from '../../../contexts/useCatalogText';
import { NavLink } from 'react-router-dom';
import { cn } from '../../../components/ui/utils';
import type { ReactNode } from 'react';
type Props = {
  playletId: string;
  characterCount: number;
  sceneCount: number;
  propCount?: number;
  className?: string;
  actions?: ReactNode;
};
export function AssetCategoryTabs({
  playletId,
  characterCount,
  sceneCount,
  propCount,
  className,
  actions,
}: Props) {
  const { c } = useCatalogText();
  const tabs = [
    {
      route: 'characters',
      label: c('全部角色'),
      count: characterCount,
    },
    {
      route: 'scenes',
      label: c('全部场景'),
      count: sceneCount,
    },
    ...(typeof propCount === 'number'
      ? [
          {
            route: 'props',
            label: c('全部道具'),
            count: propCount,
          },
        ]
      : []),
  ];
  return (
    <div className={cn('asset-category-strip w-full shrink-0', className)}>
      <div className="asset-category-header mx-auto w-full max-w-[1480px] px-4 pt-6 sm:px-6">
        <nav className="asset-category-switcher" aria-label={c('角色与场景切换')}>
          {tabs.map((tab) => (
            <NavLink key={tab.route} to={`/playlet/${playletId}/${tab.route}`} className="jm-tab">
              <span className="jm-tab-label">{tab.label}</span>
              <span className="jm-tab-count">{tab.count}</span>
            </NavLink>
          ))}
        </nav>
        {actions ? <div className="asset-category-actions">{actions}</div> : null}
      </div>
    </div>
  );
}
