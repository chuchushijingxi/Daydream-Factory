import type { BreakdownMode } from '../../api/storyboard-types';

export const breakdownModes: { id: BreakdownMode; label: string; description: string; result: string }[] = [
  { id: 'general', label: '通用型', description: '剧情清楚，节奏自然',
    result: '兼顾故事推进和镜头连贯，适合大多数剧集。' },
  { id: 'story', label: '剧情友好', description: '突出冲突、情绪与人物反应',
    result: '更关注原文已有的情绪变化、信息揭示和关键反应，让戏剧节点更清楚。' },
  { id: 'production', label: '镜头友好', description: '空间清楚，动作连续',
    result: '倾向在连续镜头中完成同一动作，减少无必要切换，让画面更连贯。' },
];
