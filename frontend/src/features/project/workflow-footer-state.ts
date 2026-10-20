import type { Project, ServiceStatus } from '../../api/types';
import type { StoryboardIndexView } from '../../api/storyboard-types';
import type { scriptParsing } from '../../domain/script-parsing';
import { indexReady } from '../../domain/storyboard';
import { workflow } from '../../domain/project-workflow';
import type { useAssetPreparation } from './useAssetPreparation';

export type WorkflowNotice = {
  tip: string;
  tone: 'neutral' | 'info' | 'success' | 'warning' | 'danger';
  values?: Record<string, string | number>;
};
export const workflowNotice = (
  tip: string,
  tone: WorkflowNotice['tone'] = 'neutral',
  values?: WorkflowNotice['values'],
): WorkflowNotice => ({ tip, tone, values });

export function scriptFooterState(
  task: ReturnType<typeof useAssetPreparation>,
  progress: ReturnType<typeof scriptParsing>,
  reading: boolean,
  indexError = '',
) {
  const disabled = reading || progress.active || (!progress.complete && task.blocked);
  let notice: WorkflowNotice;
  if (reading) notice = workflowNotice('正在读取解析状态', 'info');
  else if (indexError) notice = workflowNotice('分集目录读取失败，请重新读取', 'danger');
  else if (progress.complete) notice = workflowNotice('解析完成，可查看资产', 'success');
  else if (task.sending) notice = workflowNotice('正在提交解析请求，请稍候', 'info');
  else if (
    task.readback ||
    progress.run?.errorCode === 'MODEL_RESULT_UNKNOWN' ||
    task.flow.extraction?.errorCode === 'MODEL_RESULT_UNKNOWN' ||
    task.flow.visual?.errorCode === 'MODEL_RESULT_UNKNOWN'
  )
    notice = workflowNotice('解析结果待确认，请重新读取', 'warning');
  else if (progress.active || task.flow.active) {
    const stage = progress.steps.find((step) => !step.done);
    notice = workflowNotice(
      progress.active && stage
        ? stage.key === 'visualPrompt'
          ? '正在生成并核对图片提示词，请等待完成'
          : '正在{step}，请等待完成'
        : '项目任务进行中，请等待完成',
      'info',
      { step: stage?.label || '' },
    );
  } else if (task.readonly) notice = workflowNotice('当前为只读模式，暂不能解析', 'warning');
  else if (task.selectionMissing)
    notice = workflowNotice('请先选择分析模型，再开始解析', 'warning');
  else if (task.modelMissing) notice = workflowNotice('分析模型不可用，请先检查配置', 'warning');
  else if (task.retryIndex) notice = workflowNotice('分集识别未完成，下一步重试识别', 'warning');
  else if (task.indexing) notice = workflowNotice('分集识别未完成，请查看解析进度', 'warning');
  else if (!task.taskAvailable)
    notice = workflowNotice('资产完善暂不可用，请检查服务配置', 'warning');
  else if (task.quote.blocked)
    notice = workflowNotice(
      task.quote.busy ? '正在确认本次积分，请稍候' : '本次积分未确认，请重新读取价格',
      task.quote.busy ? 'info' : 'warning',
    );
  else if (disabled) notice = workflowNotice('解析暂不能继续，请核对错误信息', 'warning');
  else if (progress.stopped)
    notice = workflowNotice(
      progress.run?.status === 'cancelled'
        ? '解析已取消，下一步继续解析'
        : '解析未完成，下一步继续解析',
      'warning',
    );
  else if (task.pending && workflow.catalogReady(task.project))
    notice = workflowNotice('还有 {count} 项图片提示词待完善，下一步继续', 'warning', {
      count: task.pending,
    });
  else notice = workflowNotice('剧本已就绪，下一步开始解析');
  return { ...notice, disabled };
}

export function assetFooterState(
  project: Project,
  status: ServiceStatus | null,
  index: StoryboardIndexView | null,
  indexError: string,
) {
  const flow = workflow.state(project),
    pending = workflow.pendingTargets(project).length;
  const catalogReady = workflow.catalogReady(project);
  const unknown =
    flow.extraction?.errorCode === 'MODEL_RESULT_UNKNOWN' ||
    flow.visual?.errorCode === 'MODEL_RESULT_UNKNOWN';
  const disabled =
    !status?.capabilities.storyboard ||
    !!flow.active ||
    !catalogReady ||
    unknown ||
    !project.assets.length ||
    pending > 0 ||
    !indexReady(index, project.source.version);
  let notice: WorkflowNotice;
  if (!status) notice = workflowNotice('正在读取项目状态', 'info');
  else if (!status.capabilities.storyboard)
    notice = workflowNotice('分集制作暂不可用，请检查服务配置', 'warning');
  else if (flow.active) notice = workflowNotice('项目任务进行中，请等待完成', 'info');
  else if (unknown) notice = workflowNotice('资产生成结果待确认，请重新读取', 'warning');
  else if (!catalogReady || !project.assets.length)
    notice = workflowNotice('资产提取未完成，请返回剧本继续解析', 'warning');
  else if (pending)
    notice = workflowNotice(
      status.readOnly
        ? '还有 {count} 项图片提示词未就绪'
        : '还有 {count} 项图片提示词待完善，请先补齐',
      'warning',
      { count: pending },
    );
  else if (indexError && !indexReady(index, project.source.version))
    notice = workflowNotice('分集目录读取失败，请返回剧本重读', 'danger');
  else if (!index) notice = workflowNotice('正在读取分集目录', 'info');
  else if (!indexReady(index, project.source.version))
    notice = workflowNotice('分集目录未就绪，请返回剧本完成解析', 'warning');
  else
    notice = workflowNotice(
      status.readOnly ? '资产已就绪，可只读查看分集' : '资产已就绪，可选择分集',
      'success',
    );
  return { ...notice, disabled };
}
