import type { ConnectionInput, ModelSettingsInput } from '../../api/types';
export const categories = ['analysis', 'image', 'video'] as const;
export const blankConnection = (): ConnectionInput => ({
  id: '',
  name: '',
  kind: 'gateway',
  protocol: 'openai-chat',
  baseUrl: '',
  models: [],
});
export const blankModel = (): ModelSettingsInput => ({
  resolutionEnabled: null,
  creditPrice: null,
  id: '',
  logoId: '',
  modelId: '',
  alias: '',
  description: '',
  category: 'analysis',
  enabled: false,
  contextWindow: 0,
  maxOutputTokens: 0,
  pricing: null,
});
export type DirectoryItem = {
  modelId: string;
  category: ModelSettingsInput['category'];
  selected: boolean;
};

export const labels = {
  analysis: '语言',
  image: '图片',
  video: '视频',
};

export const fallbackModelName = (m: ModelSettingsInput) =>
  m.alias ||
  (m.modelId === 'deepseek-flash'
    ? 'DeepSeek Flash'
    : m.modelId === 'deepseek-v4-pro'
      ? 'DeepSeek Pro'
      : m.modelId);

export const defaultLogo = (m: ModelSettingsInput) =>
  ['deepseek-flash', 'deepseek-v4-pro'].includes(m.modelId)
    ? '/demo/assets/providers/deepseek.svg'
    : '';

// 目录与编辑器共用能力分类文案，分类值仍是现行 API 的三个稳定值。
export const modelCategoryOptions = (translate: (text: string) => string) =>
  categories.map((value) => ({ value, label: translate(labels[value]) }));
