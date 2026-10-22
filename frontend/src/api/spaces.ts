import { ApiError, http, type createHttpClient } from './client';
import { responseFields } from './response-fields';
import type { Invite, SpaceDetail, SpaceSummary } from './spaces-types';
import { translate } from '../contexts/translate';

const malformed = () =>
  new ApiError(200, 'PROGRAM_FAILED', translate('空间响应不完整，请重新读取。'));
export function spaceSummary(raw: SpaceSummary): SpaceSummary {
  const v = responseFields(raw, [
    'id',
    'kind',
    'name',
    'role',
    'memberCount',
    'memberLimit',
    'expiresAt',
    'status',
    'readOnly',
  ]);
  if (
    !v.id ||
    typeof v.id !== 'string' ||
    typeof v.name !== 'string' ||
    !['personal', 'team'].includes(v.kind) ||
    !['owner', 'admin', 'member'].includes(v.role) ||
    !['active', 'expired', 'disabled'].includes(v.status) ||
    typeof v.readOnly !== 'boolean' ||
    !Number.isSafeInteger(v.memberCount) ||
    v.memberCount < 0 ||
    !Number.isSafeInteger(v.memberLimit) ||
    v.memberLimit < 1 ||
    (v.expiresAt !== undefined &&
      (typeof v.expiresAt !== 'string' || !Number.isFinite(Date.parse(v.expiresAt))))
  )
    throw malformed();
  return v;
}
export function createSpacesApi(transport: ReturnType<typeof createHttpClient> = http) {
  return {
    async list(): Promise<SpaceSummary[]> {
      const rows = await transport.json<SpaceSummary[]>('/api/spaces');
      if (!Array.isArray(rows)) throw malformed();
      return rows.map(spaceSummary);
    },
    async detail(id: string): Promise<SpaceDetail> {
      const raw = await transport.json<SpaceDetail>('/api/spaces/' + encodeURIComponent(id));
      const summary = spaceSummary(raw);
      if (summary.id !== id) throw malformed();
      // The shell consumes summary facts only. Team settings reads its own narrow DTO.
      return summary;
    },
    async pendingInvites(): Promise<Invite[]> {
      const rows = await transport.json<Invite[]>('/api/invites/pending');
      if (!Array.isArray(rows)) throw malformed();
      return rows.map((raw) => {
        const v = responseFields(raw, [
          'id',
          'spaceId',
          'teamName',
          'kind',
          'inviteeUserId',
          'createdBy',
          'createdAt',
          'expiresAt',
          'revokedAt',
          'status',
        ]);
        if (
          typeof v.id !== 'string' ||
          !v.id ||
          typeof v.spaceId !== 'string' ||
          !v.spaceId ||
          typeof v.teamName !== 'string' ||
          v.kind !== 'email' ||
          typeof v.createdBy !== 'string' ||
          typeof v.createdAt !== 'string' ||
          !Number.isFinite(Date.parse(v.createdAt)) ||
          typeof v.expiresAt !== 'string' ||
          !Number.isFinite(Date.parse(v.expiresAt)) ||
          typeof v.status !== 'string'
        )
          throw malformed();
        return v;
      });
    },
  };
}
export const spacesApi = createSpacesApi();
export type SpacesApi = ReturnType<typeof createSpacesApi>;
