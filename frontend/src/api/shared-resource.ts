import { ApiError, http } from './client';
import { translate } from '../contexts/translate';
export type ListFilter = 'all' | 'mine' | 'shared';
export type ProjectPermissions = {
  spaceId: string;
  ownerId: string;
  creatorId: string;
  access: 1 | 2;
  canShare: boolean;
};
export type Sharing = {
  spaceId: string;
  resourceKind: 'project' | 'canvas';
  resourceId: string;
  shares: { grantee: string; permission: 1 | 2 }[];
  canManage: boolean;
};
export type UploadItem = {
  id: string;
  spaceId: string;
  ownerId: string;
  creatorId: string;
  creatorName: string;
  sharedWithMe: boolean;
  effectiveAccess: 1 | 2;
  filename: string;
  contentType: string;
  sizeBytes: number;
  hasThumbnail: boolean;
  createdAt: string;
};
const invalid = () => new ApiError(200, 'PROGRAM_FAILED', translate('空间资源回执不完整。'));
const projectPath = (id: string) => '/api/projects/' + encodeURIComponent(id);
const sharePath = (space: string, id: string) =>
  '/api/spaces/' + encodeURIComponent(space) + '/shares/project/' + encodeURIComponent(id);
export const sharedResourceApi = {
  async permissions(id: string): Promise<ProjectPermissions> {
    const v = await http.json<ProjectPermissions>(projectPath(id) + '/permissions');
    if (
      !v ||
      ['spaceId', 'ownerId', 'creatorId'].some(
        (key) =>
          typeof v[key as keyof ProjectPermissions] !== 'string' ||
          !v[key as keyof ProjectPermissions],
      ) ||
      ![1, 2].includes(v.access) ||
      typeof v.canShare !== 'boolean'
    )
      throw invalid();
    return v;
  },
  async members(space: string): Promise<{ userId: string; name: string }[]> {
    const rows = await http.json<{ userId: string; name: string }[]>(
      '/api/spaces/' + encodeURIComponent(space) + '/members',
    );
    if (
      !Array.isArray(rows) ||
      rows.some(
        (row) =>
          !row || typeof row.userId !== 'string' || !row.userId || typeof row.name !== 'string',
      )
    )
      throw invalid();
    return rows.map(({ userId, name }) => ({ userId, name }));
  },
  async sharing(space: string, id: string): Promise<Sharing> {
    return checkSharing(await http.json<Sharing>(sharePath(space, id)), space, id);
  },
  async putShare(space: string, id: string, grantee: string, permission: 1 | 2) {
    return checkSharing(
      await http.json<Sharing>(sharePath(space, id), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ grantee, permission }),
      }),
      space,
      id,
    );
  },
  async deleteShare(space: string, id: string, grantee: string) {
    const receipt = await http.json<{ removed: boolean }>(sharePath(space, id), {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ grantee }),
    });
    if (receipt?.removed !== true) throw invalid();
    return this.sharing(space, id);
  },
  async uploads(filter: ListFilter = 'all', cursor?: string) {
    const q = new URLSearchParams({ filter, limit: '24' });
    if (cursor) q.set('cursor', cursor);
    const v = await http.json<{ items: UploadItem[]; nextCursor: string | null }>(
      '/api/uploads?' + q,
    );
    if (
      !v ||
      !Array.isArray(v.items) ||
      (v.nextCursor !== null && typeof v.nextCursor !== 'string')
    )
      throw invalid();
    for (const f of v.items)
      if (
        !f ||
        ['id', 'spaceId', 'ownerId', 'creatorId'].some(
          (key) => typeof f[key as keyof UploadItem] !== 'string' || !f[key as keyof UploadItem],
        ) ||
        typeof f.filename !== 'string' ||
        typeof f.contentType !== 'string' ||
        typeof f.createdAt !== 'string' ||
        !Number.isFinite(Date.parse(f.createdAt)) ||
        typeof f.creatorName !== 'string' ||
        typeof f.sharedWithMe !== 'boolean' ||
        ![1, 2].includes(f.effectiveAccess) ||
        !Number.isSafeInteger(f.sizeBytes) ||
        f.sizeBytes < 0 ||
        typeof f.hasThumbnail !== 'boolean'
      )
        throw invalid();
    return v;
  },
};
function checkSharing(v: Sharing, space: string, id: string) {
  if (
    !v ||
    v.spaceId !== space ||
    v.resourceId !== id ||
    v.resourceKind !== 'project' ||
    v.canManage !== true ||
    !Array.isArray(v.shares) ||
    v.shares.some((s) => !s.grantee || ![1, 2].includes(s.permission))
  )
    throw invalid();
  return v;
}
