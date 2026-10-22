import { http, jsonPost, type createHttpClient } from './client';
import { watchInvalidations } from './invalidation-stream';
import type { ModelConfiguration } from './types';
import type {
  SpaceDetail,
  TeamView,
  TeamInput,
  TeamPatch,
  SettingsInput,
  TeamMember,
  Invite,
  InviteLink,
  InvitePreview,
  JoinRequest,
  OwnershipTransfer,
  Overview,
  AuditPage,
  TransferInput,
  TransferReceipt,
  AllocationInput,
  AllocationEvent,
  Page,
  SpaceSummary,
} from './spaces-types';
import { safeInteger } from './administration';
import { responseFields } from './response-fields';

const path = (id: string) => '/api/spaces/' + encodeURIComponent(id);
const segment = encodeURIComponent;
export function createTeamClient(transport: ReturnType<typeof createHttpClient> = http) {
  const checked = <T>(value: T): T => {
    const visit = (item: unknown) => {
      if (typeof item === 'number') safeInteger(item);
      else if (Array.isArray(item)) item.forEach(visit);
      else if (item && typeof item === 'object') Object.values(item).forEach(visit);
    };
    visit(value);
    return value;
  };
  const get = <T>(url: string, signal?: AbortSignal) =>
    transport.json<T>(url, { signal }).then(checked);
  const post = <T>(url: string, value: unknown = {}) =>
    transport.json<T>(url, jsonPost(value)).then(checked);
  const write = <T>(url: string, method: string, value?: unknown) =>
    transport
      .json<T>(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        ...(value === undefined ? {} : { body: JSON.stringify(value) }),
      })
      .then(checked);
  return {
    detail: (id: string, signal?: AbortSignal) => get<SpaceDetail>(path(id), signal),
    // 成员、邀请与加入申请变化的失效提示；收到后各面板重读权威 GET。
    watch: (id: string, onChange: () => void, signal: AbortSignal) =>
      watchInvalidations(transport, path(id) + '/events', 'team', onChange, signal),
    members: (id: string, signal?: AbortSignal) => get<TeamMember[]>(path(id) + '/members', signal),
    overview: (id: string, range: '7d' | '30d' | 'contract', signal?: AbortSignal) =>
      get<Overview>(path(id) + '/overview?range=' + range, signal),
    audit: (id: string, cursor?: number, signal?: AbortSignal) =>
      get<AuditPage>(
        path(id) +
          '/audit?' +
          new URLSearchParams({
            limit: '30',
            ...(cursor === undefined ? {} : { cursor: String(cursor) }),
          }),
        signal,
      ),
    settings: (id: string, input: SettingsInput) => write<TeamView>(path(id), 'PATCH', input),
    role: (id: string, user: string, role: 'admin' | 'member') =>
      post(path(id) + '/members/' + segment(user) + '/role', { role }),
    remove: (id: string, user: string) => write(path(id) + '/members/' + segment(user), 'DELETE'),
    leave: (id: string) => post(path(id) + '/leave'),
    allocate: (id: string, user: string, kind: 'allocate' | 'reclaim', input: AllocationInput) =>
      post<AllocationEvent>(path(id) + '/members/' + segment(user) + '/allocation', {
        kind,
        ...input,
      }),
    beginOwnership: (id: string, userId: string) =>
      post<OwnershipTransfer>(path(id) + '/ownership-transfers', { userId }),
    acceptOwnership: (id: string, transfer: string) =>
      post(path(id) + '/ownership-transfers/' + segment(transfer) + '/accept'),
    transfer: (id: string, input: TransferInput) =>
      post<TransferReceipt[]>(path(id) + '/transfers', input),
    invites: (id: string, signal?: AbortSignal) => get<Invite[]>(path(id) + '/invites', signal),
    createLink: (id: string) => post<InviteLink>(path(id) + '/invite-links'),
    revokeLink: (id: string, inviteId: string) =>
      write(path(id) + '/invite-links', 'DELETE', { inviteId }),
    inviteEmail: (id: string, email: string) => post<Invite>(path(id) + '/invites', { email }),
    pending: (signal?: AbortSignal) => get<Invite[]>('/api/invites/pending', signal),
    acceptInvite: (id: string) => post<SpaceSummary>('/api/invites/' + segment(id) + '/accept'),
    declineInvite: (id: string) => post('/api/invites/' + segment(id) + '/decline'),
    preview: (token: string, signal?: AbortSignal) =>
      get<InvitePreview>('/api/invite-links/' + segment(token), signal),
    acceptLink: (token: string) =>
      post<SpaceSummary>('/api/invite-links/' + segment(token) + '/accept'),
    lookup: (id: string, signal?: AbortSignal) =>
      get<{ name: string }>('/api/teams/lookup?' + new URLSearchParams({ id }), signal),
    join: (id: string, message: string) =>
      post<JoinRequest>(path(id) + '/join-requests', { message }),
    requests: (id: string, signal?: AbortSignal) =>
      get<JoinRequest[]>(path(id) + '/join-requests', signal),
    decideRequest: (id: string, request: string, approve: boolean) =>
      post(path(id) + '/join-requests/' + segment(request) + (approve ? '/approve' : '/reject')),
    teams: (q: string, page = 1, signal?: AbortSignal) =>
      get<Page<TeamView>>(
        '/api/operations/teams?' + new URLSearchParams({ q, page: String(page), pageSize: '20' }),
        signal,
      ),
    operationsTeam: (id: string, signal?: AbortSignal) =>
      get<TeamView>('/api/operations/teams/' + segment(id), signal),
    openTeam: (input: TeamInput) => post<TeamView>('/api/operations/teams', input),
    patchTeam: (id: string, input: TeamPatch) =>
      write<TeamView>('/api/operations/teams/' + segment(id), 'PATCH', input),
    async models(signal?: AbortSignal) {
      const value = await transport.json<ModelConfiguration>('/api/model-config', { signal });
      return value.connections.flatMap((connection) =>
        connection.models.map((model) => ({
          id: model.id,
          name: model.name + ' · ' + connection.name,
        })),
      );
    },
    async ownBalance(signal?: AbortSignal) {
      const value = responseFields(
        await get<{ spaceId: string; role: string; balance: number }>(
          '/api/credits/balance',
          signal,
        ),
        ['spaceId', 'role', 'balance'],
      );
      safeInteger(value.balance);
      return value;
    },
  };
}
export const teamClient = createTeamClient();
