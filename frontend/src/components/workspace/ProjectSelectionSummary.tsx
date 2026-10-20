import { stylePreviewUrl } from '../../features/project/style-preview';
import { protectedResourceUrl } from '../../api/client';
import { useCatalogText } from '../../contexts/useCatalogText';
import {
  BrainCircuit,
  Clapperboard,
  Image as ImageIcon,
  Palette,
  RectangleHorizontal,
  RectangleVertical,
  Square,
} from 'lucide-react';
import { useWorkspace } from '../../state/WorkspaceContext';
import { useState } from 'react';
import { Button } from '../ui/button';
import { ProjectChoicesDialog } from '../../features/project/components/ProjectChoicesDialog';
export function ProjectSelectionSummary() {
  const { c, styleName } = useCatalogText();
  const { currentProject: p, models, status } = useWorkspace();
  const [open, setOpen] = useState(false);
  if (!p) return null;
  const settings = p.settings,
    ratio = settings?.aspectRatio || '';
  const RatioIcon =
    ratio === '9:16' ? RectangleVertical : ratio === '1:1' ? Square : RectangleHorizontal;
  const selectedStyleName = p.style ? styleName(p.style) : c('风格未选择');
  return (
    <>
      <div role="group" aria-label={c('已确认的项目参数')} className="xy-project-selections">
        <span
          className="xy-project-selection"
          title={selectedStyleName}
          aria-label={c('风格：{name}', {
            name: selectedStyleName,
          })}
        >
          {p.style ? (
            <img src={stylePreviewUrl(p.style.previewUrl)} alt="" />
          ) : (
            <Palette aria-hidden />
          )}
          <span>{selectedStyleName}</span>
        </span>
        <span className="xy-selection-divider" aria-hidden />
        <span
          className="xy-project-selection xy-selection-ratio"
          title={c('比例：{ratio}', {
            ratio: ratio || c('未选择'),
          })}
          aria-label={c('比例：{ratio}', {
            ratio: ratio || c('未选择'),
          })}
        >
          <RatioIcon aria-hidden />
          <span>{ratio || c('比例未选择')}</span>
        </span>
        <span className="xy-selection-divider" aria-hidden />
        {(
          [
            {
              key: 'llmModel',
              label: c('分析模型'),
              options: models.analysis,
              Icon: BrainCircuit,
            },
            {
              key: 'imageModel',
              label: c('图像模型'),
              options: models.image,
              Icon: ImageIcon,
            },
            {
              key: 'videoModel',
              label: c('视频模型'),
              options: models.video,
              Icon: Clapperboard,
            },
          ] as const
        ).map(({ key, label, options, Icon }) => {
          const id = settings?.[key] || '',
            option = options.find((m) => m.id === id),
            known = option || models.catalog?.find((m) => m.id === id),
            name = id
              ? option?.name ||
                c('{name}（不可用）', {
                  name: known?.name || id,
                })
              : c('未指定');
          const text = c('{label}：{name}', {
            label,
            name,
          });
          return (
            <span
              key={key}
              className="xy-project-selection xy-project-selection--model"
              title={text}
              aria-label={text}
            >
              {known?.logoUrl ? (
                <img src={protectedResourceUrl(known.logoUrl)} alt="" />
              ) : (
                <Icon aria-hidden />
              )}
              <span>{!id ? text : name}</span>
            </span>
          );
        })}
        {status?.capabilities.projectSettings &&
        (!p.style ||
          !settings?.aspectRatio ||
          !settings?.llmModel ||
          !settings?.imageModel ||
          !settings?.videoModel) ? (
          <Button
            variant="secondary"
            size="sm"
            disabled={status.readOnly || p.runs.some((r) => r.status === 'running')}
            onClick={() => setOpen(true)}
          >
            {c('选择项目参数')}
          </Button>
        ) : null}
      </div>
      <ProjectChoicesDialog open={open} onOpenChange={setOpen} />
    </>
  );
}
