import type { Asset, ScriptSource, VisualTarget } from '../api/types';
import type {
  PromptPart,
  ReadPart,
  ShotInput,
  SourceRange,
  StoryboardIndexView,
  StoryShot,
} from '../api/storyboard-types';
import { translate } from '../contexts/translate';
// 组件展示处传入 useCatalogText() 的 c；领域函数默认保持中文源文案（含占位符插值）。
type Translate = (text: string, values?: Record<string, string | number>) => string;
const sourceText: Translate = (text, values = {}) =>
  Object.entries(values).reduce(
    (out, [key, value]) => out.split('{' + key + '}').join(String(value)),
    text,
  );
export const refKey = (ref: VisualTarget) => JSON.stringify([ref.assetId, ref.lookId ?? null]);
export const sameRef = (a: VisualTarget, b: VisualTarget) => refKey(a) === refKey(b);
export function accessMessage(reason?: string, t: Translate = sourceText) {
  const messages: Record<string, (translate: Translate) => string> = {
    RUN_ACTIVE: (translate) => translate('项目有任务正在运行，暂时只读。'),
    ASSETS_NOT_READY: (translate) => translate('请先完成资产及全部视觉正文，再进入分集制作。'),
    EPISODE_INDEX_NOT_READY: (translate) => translate('分集解析尚未完整完成。'),
    EPISODE_INDEX_REQUIRED: (translate) => translate('需要当前原文的完整分集目录。'),
    EPISODE_RANGE_STALE: (translate) => translate('本集原文范围已失效，请回到解析步骤核对。'),
    MODEL_RESULT_UNKNOWN: (translate) =>
      translate('存在结果未明的调用，请重新读取，不能重复发起。'),
    VISUAL_PROMPTS_REQUIRED: (translate) => translate('所选资产的视觉正文尚未有效完成。'),
    SERVICE_READ_ONLY: (translate) => translate('当前服务只读。'),
  };
  const message = messages[reason || ''];
  return message ? message(t) : reason || '';
}
export function assetLabel(
  assets: Asset[],
  ref: VisualTarget,
  t: Translate = sourceText,
) {
  const a = assets.find((a) => a.id === ref.assetId);
  if (!a) return ref.assetId + t('（对象不存在）');
  return (
    a.name +
    (ref.lookId
      ? ' · ' + (a.looks.find((l) => l.id === ref.lookId)?.name || ref.lookId + t('（形象不存在）'))
      : a.kind === 'character'
        ? t(' · 基础')
        : '')
  );
}
export const cleanParts = (parts: ReadPart[]): PromptPart[] =>
  parts
    .filter((p) => p.type !== 'text' || p.text.length > 0)
    .map((p) =>
      p.type === 'text'
        ? { type: 'text', text: p.text }
        : { type: 'asset', assetId: p.assetId, ...(p.lookId ? { lookId: p.lookId } : {}) },
    );
export function readable(parts: ReadPart[], assets: Asset[]) {
  return parts
    .map((p) =>
      p.type === 'text'
        ? p.text
        : '@' +
          (p.available === false && p.label
            ? p.label
            : assets.some((a) => a.id === p.assetId)
              ? assetLabel(assets, p)
              : p.label || p.assetId),
    )
    .join('');
}
export const cleanRefs = (refs: VisualTarget[]): VisualTarget[] =>
  refs.map((r) => ({ assetId: r.assetId, ...(r.lookId ? { lookId: r.lookId } : {}) }));
export const shotInput = (s: StoryShot): ShotInput => ({
  sourceRanges: s.sourceRanges,
  action: s.action,
  dialogues: s.dialogues,
  camera: s.camera,
  durationSeconds: s.durationSeconds,
  designNotes: s.designNotes,
  selectedAssets: cleanRefs(s.selectedAssets),
  videoPrompt: s.videoPrompt ? { parts: cleanParts(s.videoPrompt.parts) } : null,
});
export function shotPatch(base: ShotInput, draft: ShotInput): Partial<ShotInput> {
  const patch: Partial<ShotInput> = {};
  for (const key of Object.keys(draft) as (keyof ShotInput)[])
    if (JSON.stringify(base[key]) !== JSON.stringify(draft[key]))
      Object.assign(patch, { [key]: draft[key] });
  // 选用关系与引用始终同一命令；未改正文时不刷新其依据。
  return patch;
}
export function referenceLocations(
  parts: PromptPart[],
  ref: VisualTarget,
  t: Translate = sourceText,
) {
  let line = 1,
    column = 1;
  const positions: string[] = [];
  for (const p of parts) {
    if (p.type === 'asset') {
      if (sameRef(p, ref))
        positions.push(t('第 {line} 行，第 {column} 位置', { line, column }));
      column++;
    } else
      for (const char of p.text) {
        if (char === '\n') {
          line++;
          column = 1;
        } else column++;
      }
  }
  return positions;
}
export function indexReady(v: StoryboardIndexView | null, sourceVersion?: number) {
  return (
    !!v &&
    v.sourceVersion === sourceVersion &&
    v.index?.sourceVersion === sourceVersion &&
    v.index.valid &&
    !!v.index.segmentation &&
    v.index.episodes.length > 0 &&
    v.latestIndexRun?.sourceVersion === sourceVersion &&
    v.latestIndexRun.status === 'completed'
  );
}
export function indexLabel(
  v: StoryboardIndexView | null,
  sourceVersion?: number,
  t: Translate = sourceText,
) {
  if (!v) return t('尚未取得分集目录');
  const run = v.latestIndexRun?.sourceVersion === sourceVersion ? v.latestIndexRun : null;
  if (run?.status === 'running') return t('正在识别剧本分集');
  if (run?.status === 'failed') return t('分集识别失败');
  if (run?.status === 'cancelled') return t('分集识别已取消');
  if (!indexReady(v, sourceVersion)) return t('分集目录尚未完成或已失效');
  if (v.index?.segmentation === 'whole') return t('未分集／整篇');
  return v.episodeCount === null
    ? t('目录已返回，集数未确定')
    : t('本次上传共 {count} 集', { count: v.episodeCount });
}
// SourceRange.offset 是 Unicode 码点，Array.from 保留 emoji 等非 BMP 字符。
export function sourceSlice(source: ScriptSource, ranges: SourceRange[]): string[] {
  return ranges.map((r) => {
    const start = source.spans.findIndex((s) => s.id === r.start.spanId),
      end = source.spans.findIndex((s) => s.id === r.end.spanId);
    if (start < 0 || end < start) throw new Error(translate('原文段不存在或顺序失效。'));
    return source.spans
      .slice(start, end + 1)
      .map((s, i) => {
        const chars = Array.from(s.text),
          from = i === 0 ? r.start.offset : 0,
          to = i === end - start ? r.end.offset : chars.length;
        if (
          !Number.isInteger(from) ||
          !Number.isInteger(to) ||
          from < 0 ||
          to < from ||
          to > chars.length
        )
          throw new Error(translate('原文码点范围失效。'));
        return chars.slice(from, to).join('');
      })
      .join('\n');
  });
}
