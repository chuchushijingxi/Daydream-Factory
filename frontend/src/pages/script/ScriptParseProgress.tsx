import { useEffect, useRef, useState } from 'react';
import { Check, CheckCircle2, CircleAlert, FileSearch, ChevronUp, Info } from 'lucide-react';
import { Button } from '../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from '../../components/ui/dialog';
import { indexLabel } from '../../domain/storyboard';
import { Spinner } from '../../components/ui/spinner';
import { toast } from '../../components/ui/sonner';
import { useCatalogText } from '../../contexts/useCatalogText';
import { scriptParsing } from '../../domain/script-parsing';
import { useWorkspace } from '../../state/WorkspaceContext';
import type { useAssetPreparation } from '../../features/project/useAssetPreparation';
import type { WorkflowNotice } from '../../features/project/workflow-footer-state';

export function ScriptParseProgress({
  task,
  notice,
}: {
  task: ReturnType<typeof useAssetPreparation>;
  notice: WorkflowNotice;
}) {
  const { c } = useCatalogText();
  const { storyboardIndex, storyboardError } = useWorkspace();
  const { project, status } = task;
  const [open, setOpen] = useState(false);
  const dialog = useRef<HTMLDivElement>(null);
  useEffect(() => setOpen(false), [project?.id]);
  const previous = useRef<{ projectId: string; running: boolean } | null>(null);
  const reading =
    !status || (!!status.capabilities.storyboard && !storyboardIndex && !storyboardError);
  const progress =
    project && !reading
      ? scriptParsing(project, storyboardIndex, !!status?.capabilities.storyboard)
      : null;
  useEffect(() => {
    if (!project || !progress) return;
    if (previous.current?.projectId === project.id && previous.current.running && progress.complete)
      toast.success(c('解析完成，资产资料和视觉提示词已准备好。'), {
        id: 'parse-complete-' + project.id,
      });
    previous.current = { projectId: project.id, running: progress.active };
  }, [project?.id, progress?.active, progress?.complete]);
  if (!project) return null;
  if (!progress)
    return (
      <div className="script-parse-dock-reading" role="status">
        <Spinner />
        {c('读取解析状态')}
      </div>
    );
  const { complete, steps, finished, percent, active, stopped, run, saved, targetCount } = progress;
  const current = steps.findIndex((step) => !step.done);
  const title = complete
    ? c('解析完成，资产已准备好')
    : active
      ? c(run?.stage === 'visualPrompt' ? '正在生成资产图片提示词' : '正在解析剧本')
      : stopped
        ? c(run?.status === 'cancelled' ? '解析已取消' : '解析未完成')
        : c('剧本已就绪，开始解析');
  const visualProgress =
    saved > 0
      ? c('已保存 {saved}/{total} 项图片提示词，正在核对。', { saved, total: targetCount })
      : c('正在为角色、场景和道具编写图片提示词。');
  const description = complete
    ? c('角色 {characters} · 场景 {locations} · 道具 {props} · 图片提示词 {prompts}', {
        characters: project.assets.filter((a) => a.kind === 'character').length,
        locations: project.assets.filter((a) => a.kind === 'location').length,
        props: project.assets.filter((a) => a.kind === 'prop').length,
        prompts: task.flow.generatedCount,
      })
    : stopped
      ? c(
          run?.status === 'cancelled'
            ? '解析已停止，已保存的分集、资产和提示词会保留；已发送的请求可能仍计费。'
            : '已保存的内容会保留，请查看具体原因后重试。',
        )
      : active && run?.stage === 'visualPrompt'
        ? visualProgress
        : active
          ? c('正在{step}，完成后自动进入下一阶段。', { step: c(steps[current]?.label || '核对') })
          : c('一次完成资产提取、补漏纠错和视觉提示词，解析完成后再审核资产。');
  const tone = complete
    ? 'success'
    : active
      ? 'info'
      : stopped
        ? run?.status === 'failed'
          ? 'danger'
          : 'warning'
        : 'neutral';
  const state = complete ? 'completed' : active ? 'running' : stopped ? 'stopped' : 'ready';
  const phase =
    progress.hasIndex && !steps[0].done
      ? indexLabel(storyboardIndex, project.source.version, c)
      : stopped
        ? c('查看原因与已有结果')
        : complete
          ? c('查看解析详情')
          : c('分集、资产与视觉正文');
  // 运行中第二行只展示真实保存进度；步骤名已包含在主提示里，不重复。
  const supporting = !active
    ? phase
    : run?.stage === 'visualPrompt'
      ? visualProgress
      : '';
  const icon =
    notice.tone === 'success' ? (
      <Check />
    ) : notice.tone === 'info' ? (
      <Spinner />
    ) : notice.tone === 'warning' || notice.tone === 'danger' ? (
      <CircleAlert />
    ) : (
      <Info />
    );
  const noticeValues = notice.values?.step
    ? { ...notice.values, step: c(String(notice.values.step)) }
    : notice.values;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <button
          type="button"
          className="script-parse-dock-status"
          aria-label={c('查看解析进度')}
          data-state={state}
          data-tone={notice.tone}
        >
          <span className="script-parse-dock-icon" aria-hidden>
            {icon}
          </span>
          <span
            className="script-parse-dock-copy"
            aria-live="polite"
            key={state + ':' + run?.stage}
          >
            <strong>{c(notice.tip, noticeValues)}</strong>
            {supporting ? <small>{supporting}</small> : null}
          </span>
          <span className="script-parse-dock-meter">
            <span
              className="script-parse-mini-track"
              role={open ? undefined : 'progressbar'}
              aria-label={c('解析步骤完成度')}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={percent}
            >
              <span style={{ width: percent + '%' }} />
            </span>
            <small>{percent}%</small>
          </span>
          <span className="editor-progress-disclosure">
            {c('查看进度')}
            <ChevronUp className="script-parse-dock-chevron" aria-hidden />
          </span>
        </button>
      </DialogTrigger>
      <DialogContent
        variant="form"
        className="script-parse-dialog"
        ref={dialog}
        tabIndex={-1}
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          dialog.current?.focus({ preventScroll: true });
        }}
      >
        <section className="script-parse-panel" data-state={state} data-tone={tone}>
          <div className="script-parse-heading">
            <span className="script-parse-icon" aria-hidden>
              {complete ? (
                <CheckCircle2 />
              ) : active ? (
                <Spinner />
              ) : stopped ? (
                <CircleAlert />
              ) : (
                <FileSearch />
              )}
            </span>
            <div className="script-parse-copy">
              <DialogTitle>{title}</DialogTitle>
              <DialogDescription>{description}</DialogDescription>
            </div>
            <div className="script-parse-meter">
              <strong>
                {percent}
                <span>%</span>
              </strong>
              <small>
                {c('已完成 {done}/{total} 步', { done: finished, total: steps.length })}
              </small>
            </div>
          </div>
          <div
            className="script-parse-track"
            role="progressbar"
            aria-label={c('解析步骤完成度')}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent}
          >
            <span style={{ width: percent + '%' }} />
          </div>
          <ol className="script-parse-steps">
            {steps.map((step, i) => (
              <li
                key={step.key}
                data-state={
                  step.done
                    ? 'done'
                    : i === current && active
                      ? 'active'
                      : i === current && stopped
                        ? 'stopped'
                        : 'waiting'
                }
                aria-current={i === current && active ? 'step' : undefined}
              >
                <span className="script-parse-step-icon" aria-hidden>
                  {step.done ? <Check /> : i === current && active ? <Spinner /> : i + 1}
                </span>
                <div>
                  <strong>{c(step.label)}</strong>
                  <small>
                    {step.done
                      ? c('已完成')
                      : i === current && stopped
                        ? c(run?.status === 'cancelled' ? '已取消' : '未完成')
                        : i === current && active
                          ? c('进行中')
                          : c('待处理')}
                  </small>
                </div>
              </li>
            ))}
          </ol>
          {task.flow.active?.id === run?.id && active ? (
            <div className="script-parse-bottom">
              <div>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={task.readonly || task.sending || task.readback}
                  onClick={() => void task.cancel()}
                  title={c('保留已保存内容；已发出的模型请求可能仍产生费用。')}
                >
                  {c('取消解析')}
                </Button>
              </div>
            </div>
          ) : null}
          {stopped && run?.error ? (
            <p role="alert" className="jm-inline-error">
              {run.errorCode} · {run.error}
            </p>
          ) : null}
          {task.error ? (
            <p role="alert" className="jm-inline-error">
              {task.error}
            </p>
          ) : null}
          {task.readback ? (
            <Button
              variant="link"
              size="sm"
              disabled={task.sending}
              onClick={() => void task.reload()}
            >
              {c('读取最新版本')}
            </Button>
          ) : null}
        </section>
      </DialogContent>
    </Dialog>
  );
}
