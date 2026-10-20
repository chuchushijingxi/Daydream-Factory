import { Spinner } from '../../components/ui/spinner';
import type { ProjectRow } from '../../api/types';
import { useCatalogText } from '../../contexts/useCatalogText';

export function ProjectRunStatus({ run }: { run: ProjectRow['latestRun'] }) {
  const { c } = useCatalogText();
  if (!run || run.status !== 'running') return null;
  const label =
    run.contract === 'episode-index-agent-v1'
      ? c('正在识别分集')
      : run.contract === 'storyboard-agent-v1'
        ? c('正在生成场景分镜')
        : run.stage === 'extract'
          ? c('正在提取资产')
          : run.stage === 'correct'
            ? c('正在补漏纠错')
            : run.stage === 'visualPrompt'
              ? c('正在生成图片提示词')
              : c('正在解析');
  return (
    <span className="project-run-status" role="status" data-state={run.status}>
      <Spinner />
      {label}
    </span>
  );
}
