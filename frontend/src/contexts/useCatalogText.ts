import { useLanguage } from './LanguageContext';
import type { VisualStyle, ProjectWorkflowState } from '../api/types';
export type CatalogText = (text: string, values?: Record<string, string | number>) => string;
export function useCatalogText() {
  const { t, language } = useLanguage();
  const c: CatalogText = (text, values = {}) =>
    t('catalog.' + text, {
      defaultValue: text,
      ...values,
    });
  const styleName = (style: VisualStyle) =>
    language === 'en'
      ? t('catalog.style.' + style.id + '.name', {
          defaultValue: style.name,
        })
      : style.name;
  const styleDescription = (style: VisualStyle) =>
    language === 'en'
      ? t('catalog.style.' + style.id + '.description', {
          defaultValue: style.description || '',
        })
      : style.description || '';
  return {
    c,
    styleName,
    styleDescription,
  };
}
// 从真实任务状态本地化展示文案；绝不从错误文字推断结果。
export function workflowCopy(flow: ProjectWorkflowState, c: CatalogText) {
  const taskName = (contract: string) =>
    c(
      contract === 'outline-agent-v1'
        ? '大纲'
        : contract === 'asset-preparation-v1' || contract === 'catalog-agent-v3'
          ? '资产生成'
          : '资产完善',
    );
  const label = flow.label ? c(flow.label) : '';
  let reason = '';
  if (flow.failed) {
    reason = c('{task}生成失败：{reason}', {
      task: taskName(flow.failed.contract),
      reason: flow.failed.error || flow.failed.errorCode || c('原因未返回，请重新读取。'),
    });
  } else if (flow.cancelled) {
    reason = c('{task}任务已取消，已保存部分可查看。', {
      task: taskName(flow.cancelled.contract),
    });
  } else if (flow.visual?.status === 'failed') {
    reason = c('资产内容未完成：{reason}', {
      reason: flow.visual.error || flow.visual.errorCode || c('原因未返回，请重新读取。'),
    });
  } else if (flow.visual?.status === 'cancelled') {
    reason = c('资产内容生成已取消，已保存部分可查看。');
  }
  const editReason = reason || c(flow.editReason);
  return {
    label,
    reason,
    editReason,
  };
}
