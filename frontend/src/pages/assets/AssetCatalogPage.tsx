import { workflow } from '../../domain/project-workflow.ts';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useWorkspace } from '../../state/WorkspaceContext';
import { cn } from '../../components/ui/utils';
import type { Asset } from '../../api/types';
import { AssetCategoryTabs } from '../../features/project/components/AssetCategoryTabs';
import { AddAssetCard, AssetCard } from '../../features/project/components/AssetCards';
import { ProjectWorkflowFooter } from '../../features/project/components/ProjectWorkflowFooter';
import { ProjectTaskActions } from '../../features/project/components/ProjectTaskActions';
import { AssetDialog } from '../../features/project/components/AssetDialog';
import { Button } from '../../components/ui/button';
import { assetFooterState } from '../../features/project/workflow-footer-state';
import { AssetImageTasksProvider } from '../../features/media/AssetImageTasks';
import { BatchAssetImages } from '../../features/media/BatchAssetImages';
export function AssetCatalogPage({ kind }: { kind: Asset['kind'] }) {
  return <AssetImageTasksProvider><AssetCatalogContent kind={kind} /></AssetImageTasksProvider>;
}
function AssetCatalogContent({ kind }: { kind: Asset['kind'] }) {
  const { c } = useCatalogText();
  const { currentProject: p, theme, status, storyboardIndex, storyboardError } = useWorkspace();
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<{
    asset?: Asset;
    deleting?: boolean;
  } | null>(null);
  useEffect(() => setDialog(null), [kind, p?.id]);
  if (!p) return null;
  const footer = assetFooterState(p, status, storyboardIndex, storyboardError);
  const editable = Boolean(
      status &&
        !status.readOnly &&
        status.capabilities.assetEditing === true &&
        workflow.state(p).editable,
    ),
    isDark = theme === 'dark';
  return (
    <div
      className={cn(
        'jm-workspace-surface flex min-h-0 flex-1 flex-col overflow-hidden',
        isDark ? 'text-slate-100' : 'text-playlet-title',
      )}
    >
      <div className="xy-project-scroll flex min-h-0 flex-1 flex-col overflow-y-auto">
        <AssetCategoryTabs
          playletId={p.id}
          characterCount={p.assets.filter((a) => a.kind === 'character').length}
          sceneCount={p.assets.filter((a) => a.kind === 'location').length}
          propCount={p.assets.filter((a) => a.kind === 'prop').length}
          actions={<><BatchAssetImages kind={kind} /><ProjectTaskActions statusOnly={!workflow.catalogReady(p)} /></>}
        />
        <div className="asset-catalog-body">
          {!workflow.catalogReady(p) ? (
            <div className="script-parse-library-hint">
              <p>{c('请在剧本页面完成解析，再查看和调整资产。')}</p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => navigate(`/playlet/${p.id}/outline`)}
              >
                {c('查看解析进度')}
              </Button>
            </div>
          ) : null}
          <div className="asset-catalog-grid">
            {p.assets
              .filter((a) => a.kind === kind)
              .map((a) => (
                <AssetCard
                  key={a.id}
                  asset={a}
                  editable={editable}
                  onDelete={() => setDialog({ asset: a, deleting: true })}
                  onClick={() =>
                    setDialog({
                      asset: a,
                    })
                  }
                />
              ))}
            <AddAssetCard
              kind={kind}
              disabled={!editable}
              onClick={() => {
                if (editable) setDialog({});
              }}
            />
          </div>
        </div>
      </div>
      <ProjectWorkflowFooter
        tip={c(footer.tip, footer.values)}
        tipTone={footer.tone}
        nextDisabled={footer.disabled}
        onPrevious={() => navigate(`/playlet/${p.id}/outline`)}
        onNext={() => navigate(`/playlet/${p.id}/episodes`)}
      />
      <AssetDialog
        key={kind}
        kind={kind}
        open={dialog !== null}
        asset={dialog?.asset}
        initialDelete={dialog?.deleting}
        onOpenChange={(open) => {
          if (!open) setDialog(null);
        }}
      />
    </div>
  );
}
