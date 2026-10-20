// 对齐当前 Go storyboard_views.go 与共享写 schema；仅在实时 capability 开放时请求。
import type { MediaTask, TaskRun, VisualTarget } from './types';
export type SourcePoint = { spanId: string; offset: number };
export type BreakdownMode = 'general' | 'story' | 'production';
export type SourceRange = { start: SourcePoint; end: SourcePoint };
export type Issue = {
  code: string;
  assetId?: string;
  lookId?: string;
  sceneId?: string;
  shotId?: string;
};
export type PromptPart = { type: 'text'; text: string } | ({ type: 'asset' } & VisualTarget);
export type ReadPart = PromptPart & { label?: string; available?: boolean; thumbnailUrl?: string };
export type VideoPrompt = {
  parts: ReadPart[];
  plainText: string;
  origin: string;
  valid: boolean;
  issues: Issue[];
};
export type StoryboardRun = TaskRun & {
  storyboardScope?: {
    operation: 'index' | 'breakdown' | 'fillPrompts' | 'regeneratePrompts';
    episodeId?: string;
    shotIds?: string[];
    episodeRangeDigest?: string;
    breakdownMode?: BreakdownMode;
    videoConstraint?: { modelId: string; modelName: string; maxDurationSeconds: number };
  };
};
export type StoryboardRoot = {
  projectId: string;
  revision: number;
  sourceVersion: number;
  access: { editable: boolean; canStart: boolean; reason: string };
  activeRun: StoryboardRun | null;
};
export type Episode = {
  id: string;
  episodeNumber: number | null;
  title: string;
  sourceRanges: SourceRange[];
  sceneIds: string[];
  origin: string;
  latestRun?: StoryboardRun | null;
  characterCount?: number;
  sceneCount?: number;
  shotCount?: number;
  pendingPromptCount?: number;
};
export type StoryboardIndexView = StoryboardRoot & {
  episodeCount: number | null;
  index: null | {
    sourceVersion: number;
    segmentation: 'episodes' | 'whole' | null;
    supportRanges: SourceRange[];
    valid: boolean;
    issues: Issue[];
    episodes: Episode[];
  };
  latestIndexRun: StoryboardRun | null;
};
export type StoryScene = {
  id: string;
  episodeId: string;
  title: string;
  sourceRanges: SourceRange[];
  locationAssetId: string | null;
  time: string;
  summary: string;
  shotIds: string[];
  valid: boolean;
  issues: Issue[];
};
export type ShotInput = {
  sourceRanges: SourceRange[];
  action: string;
  dialogues: { speakerAssetId: string | null; text: string }[];
  camera: { size: string; angle: string; movement: string };
  durationSeconds: number | null;
  designNotes: string;
  selectedAssets: VisualTarget[];
  videoPrompt: { parts: PromptPart[] } | null;
};
export type StoryShot = Omit<ShotInput, 'videoPrompt'> & {
  id: string;
  sceneId: string;
  thumbnailUrl?: string;
  previewDurationSeconds?: number;
  videoTask?: Pick<MediaTask, 'id' | 'status'>;
  videoPrompt: VideoPrompt | null;
  valid: boolean;
  issues: Issue[];
};
export type EpisodeView = StoryboardRoot & {
  episode: Episode;
  scenes: StoryScene[];
  shots: StoryShot[];
  index: { valid: boolean; issues: Issue[]; supportRanges?: SourceRange[] };
  latestRun: StoryboardRun | null;
};
export type EpisodeSourceView = StoryboardRoot & {
  episodeId: string;
  ranges: SourceRange[];
  fragments: { spanId: string; startOffset: number; endOffset: number; text: string }[];
  supportRanges: SourceRange[];
};
export type EpisodeRunInput =
  | { billingBasis?: string; revision: number; operation: 'breakdown'; breakdownMode?: BreakdownMode }
  | { billingBasis?: string; revision: number; operation: 'fillPrompts' }
  | { billingBasis?: string; revision: number; operation: 'regeneratePrompts'; shotIds: string[] };
export type SceneInput = Pick<
  StoryScene,
  'title' | 'sourceRanges' | 'locationAssetId' | 'time' | 'summary'
>;
export type StoryboardOperation =
  | { op: 'updateShot'; shotId: string; patch: Partial<ShotInput> & { sceneId?: string } }
  | { op: 'createShot'; sceneId: string; beforeShotId?: string; afterShotId?: string; value: ShotInput }
  | { op: 'deleteShot'; shotId: string }
  | { op: 'setShotOrder'; sceneId: string; shotIds: string[] }
  | { op: 'createScene'; episodeId: string; value: SceneInput }
  | { op: 'updateScene'; sceneId: string; patch: Partial<SceneInput> }
  | { op: 'deleteScene'; sceneId: string }
  | { op: 'setSceneOrder'; episodeId: string; sceneIds: string[] };
export type ChangeReceipt = {
  projectId: string;
  revision: number;
  changes: { entityType: string; id: string; before: unknown; after: unknown }[];
  invalidated: { entityType: string; id: string; reasons: Issue[] }[];
};
