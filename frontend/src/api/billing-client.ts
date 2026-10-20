import { http, jsonPost, ApiError } from './client';
import type { ChargeQuoteDTO } from './administration-types';
import type { ProjectSettings } from './types';
import { safeInteger } from './administration';
import type { MediaTarget } from './project-media';
import { translate } from '../contexts/translate';
export type QuoteInput =
  | {
      revision: number;
      operation:
        | 'asset-preparation'
        | 'episode-index'
        | 'visual'
        | 'single-visual'
        | 'breakdown'
        | 'fillPrompts'
        | 'regeneratePrompts';
      assetId?: string;
      lookId?: string;
      episodeId?: string;
      shotIds?: string[];
      breakdownMode?: import('./storyboard-types').BreakdownMode;
    }
  | {
      revision: number;
      operation: 'media';
      imageType?: import('./types').CharacterImageType;
      aspectRatio?: string;
      modelId: string;
      target: MediaTarget;
      options?: import('./types').MediaOptions;
    };
function checked(v: ChargeQuoteDTO, unit: ChargeQuoteDTO['unit'] = 'characters') {
  if (!v?.basis || v.unit !== unit || !v.rule || typeof v.free !== 'boolean')
    throw new ApiError(
      200,
      'PROGRAM_FAILED',
      translate('计费报价回执不完整，尚未确认价格。'),
    );
  for (const n of [
    v.sourceVersion,
    v.inputRevision,
    v.quantity,
    v.priceVersion,
    v.rule.unitPrice,
    v.originalAmount,
    v.amount,
  ])
    safeInteger(n);
  return v;
}
export const billingApi = {
  async file(fileId: string, settings: ProjectSettings) {
    return checked(
      await http.json<ChargeQuoteDTO>('/api/projects/quote', jsonPost({ fileId, settings })),
    );
  },
  async project(id: string, input: QuoteInput, signal?: AbortSignal) {
    const v = checked(
      await http.json<ChargeQuoteDTO>(
        '/api/projects/' + encodeURIComponent(id) + '/quote',
        { ...jsonPost(input), signal },
      ),
      input.operation === 'media' ? ('assetId' in input.target ? 'image' : 'second') : 'characters',
    );
    if (
      input.operation === 'media' &&
      (v.modelId !== input.modelId ||
        v.inputRevision !== input.revision ||
        (input.options && JSON.stringify(v.mediaOptions) !== JSON.stringify(input.options)))
    )
      throw new ApiError(
        200,
        'PROGRAM_FAILED',
        translate('媒体报价与当前模型或项目不一致。'),
      );
    return v;
  },
};
