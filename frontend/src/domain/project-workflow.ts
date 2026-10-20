import type { Project, TaskRun, VisualTarget } from '../api/types';

const contracts = {
  outline: 'outline-agent-v1',
  extraction: 'asset-preparation-v1',
  visual: 'visual-prompt-agent-v1',
} as const;
type TaskKey = keyof typeof contracts;
const names = {
  outline: '大纲',
  extraction: '资产生成',
  visual: '资产完善',
};
function tasks(project: Project | null | undefined) {
  const runs = project?.runs || [];
  const result: Record<TaskKey, TaskRun | null> = {
    outline: null,
    extraction: null,
    visual: null,
  };
  for (const run of runs) {
    for (const [key, contract] of Object.entries(contracts) as [TaskKey, string][]) {
      if (
        (run.contract === contract ||
          (key === 'extraction' && run.contract === 'catalog-agent-v3')) &&
        (key === 'outline' || run.sourceVersion === project?.source.version)
      )
        result[key] = run;
    }
    // 视觉任务含合并合同进入的 visualPrompt 阶段；runs 按时间序，后者覆盖前者。
    if (isVisualRun(run) && run.sourceVersion === project?.source.version) result.visual = run;
  }
  return result;
}
function isVisualRun(run: TaskRun) {
  return (
    run.contract === contracts.visual ||
    (run.contract === contracts.extraction && run.stage === 'visualPrompt')
  );
}
function catalogReady(project: Project | null | undefined) {
  const run = tasks(project).extraction;
  return (
    !!run &&
    (run.status === 'completed' ||
      (run.contract === contracts.extraction && run.stage === 'visualPrompt'))
  );
}
function prompt(project: Project | null | undefined, target: VisualTarget) {
  return (
    (project?.visualPrompts || []).find(
      (p) =>
        p.target.assetId === target.assetId && (p.target.lookId || '') === (target.lookId || ''),
    ) || null
  );
}
const sameTarget = (a: VisualTarget | undefined, b: VisualTarget | undefined) =>
  a?.assetId === b?.assetId && (a?.lookId || '') === (b?.lookId || '');
function targets(project: Project | null | undefined) {
  return (project?.assets || []).flatMap((a) => [
    {
      assetId: a.id,
    },
    ...(a.looks || []).map((l) => ({
      assetId: a.id,
      lookId: l.id,
    })),
  ]);
}
function effectivePrompt(project: Project | null | undefined, target: VisualTarget) {
  const p = prompt(project, target),
    owner = (project?.runs || []).find((r) => r.id === p?.runId);
  return !!(
    p &&
    !p.stale &&
    p.sourceVersion === project?.source.version &&
    p.text.trim() &&
    (p.origin === 'user' ||
      (owner &&
        isVisualRun(owner) &&
        owner.status === 'completed' &&
        owner.sourceVersion === project?.source.version &&
        owner.targets?.some((t) => sameTarget(t, target))))
  );
}
function pendingTargets(project: Project | null | undefined) {
  return targets(project).filter((t) => !effectivePrompt(project, t));
}
function targetTask(project: Project | null | undefined, target: VisualTarget) {
  return (
    [...(project?.runs || [])]
      .reverse()
      .find(
        (r) =>
          isVisualRun(r) &&
          r.sourceVersion === project?.source.version &&
          r.targets?.some((t) => sameTarget(t, target)),
      ) || null
  );
}
function state(project: Project | null | undefined) {
  const current = tasks(project);
  const emptyFailure = (project?.runs || []).find(
    (r) =>
      isVisualRun(r) &&
      r.sourceVersion === project?.source.version &&
      r.status === 'failed' &&
      !r.target &&
      r.targets?.length === 0,
  );
  const failed =
    (Object.entries(current) as [TaskKey, TaskRun | null][]).find(
      ([key, r]) =>
        key !== 'visual' &&
        r?.status === 'failed' &&
        !(
          key === 'extraction' &&
          r.contract === contracts.extraction &&
          r.stage === 'visualPrompt'
        ),
    ) || (emptyFailure ? (['visual' as const, emptyFailure] as const) : null);
  const cancelled = (Object.entries(current) as [TaskKey, TaskRun | null][]).find(
    ([key, r]) =>
      key !== 'visual' &&
      r?.status === 'cancelled' &&
      !(key === 'extraction' && r.contract === contracts.extraction && r.stage === 'visualPrompt'),
  );
  const active = (project?.runs || []).find((r) => r.status === 'running') || null;
  const visual = current.visual;
  const all = targets(project),
    generatedCount = all.filter((t) => effectivePrompt(project, t)).length;
  const editable = !!project && !active && !failed && !cancelled && catalogReady(project);
  let label = '原文已保存',
    status = 'uploaded',
    reason = '';
  if (failed) {
    label = '项目失败';
    status = 'failed';
    reason =
      names[failed[0]] +
      '生成失败：' +
      (failed[1]!.error || failed[1]!.errorCode || '原因未返回，请重新读取。');
  } else if (cancelled) {
    label = '已取消';
    status = 'cancelled';
    reason = names[cancelled[0]] + '任务已取消，已保存部分可查看。';
  } else if (active) {
    status = 'running';
    label =
      active.contract === 'episode-index-agent-v1'
        ? '正在识别剧本分集'
        : active.contract === 'storyboard-agent-v1'
          ? '正在制作分集场景与分镜'
          : active.contract === contracts.extraction
            ? active.stage === 'visualPrompt'
              ? '正在完善并核对资产'
              : active.stage === 'extract'
                ? '正在提取资产'
                : '正在补漏纠错'
            : active.contract === contracts.outline
              ? '大纲生成中'
              : '正在完善并核对资产';
  } else if (visual?.status === 'failed') {
    status = 'failed';
    label = visual.contract === contracts.extraction ? '资产生成失败' : '资产完善失败';
    reason = visual.error || visual.errorCode || '原因未返回，请重新读取。';
  } else if (visual?.status === 'cancelled') {
    status = 'cancelled';
    label = visual.contract === contracts.extraction ? '资产生成已取消' : '资产完善已取消';
  } else if (catalogReady(project) && all.length > 0) {
    status = 'completed';
    label = generatedCount === all.length ? '' : '资产内容待完善';
  } else if (current.extraction?.status === 'completed') {
    status = 'completed';
    label = '提取完成';
  } else if (current.outline?.status === 'completed') {
    status = 'completed';
    label = '大纲已生成';
  }
  const editReason = failed
    ? reason
    : cancelled
      ? reason
      : active
        ? '任务进行中，资产与附加形象暂时只读。'
        : !editable
          ? '资产生成结束后，可检查资料、编辑提示词或调整附加形象。'
          : '';
  return {
    ...current,
    active,
    failed: failed?.[1] || null,
    cancelled: cancelled?.[1] || null,
    editable,
    label,
    status,
    reason,
    editReason,
    generatedCount,
    targetCount: all.length,
  };
}
export const workflow = {
  contracts,
  catalogReady,
  names,
  tasks,
  prompt,
  state,
  targets,
  pendingTargets,
  effectivePrompt,
  targetTask,
};
