// Wire DTOs shared by space, team and operations consumers. The server owns roles.
export type SpaceKind = 'personal' | 'team';
export type SpaceRole = 'owner' | 'admin' | 'member';
export type SpaceStatus = 'active' | 'expired' | 'disabled';
export type SpacePolicy = 'isolated' | 'read_all' | 'edit_all';
export type ResourceAccess = 0 | 1 | 2;
export type ListFilter = 'all' | 'mine' | 'shared';
export type Page<T> = {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
};
export type SpaceSummary = {
  id: string;
  kind: SpaceKind;
  name: string;
  role: SpaceRole;
  memberCount: number;
  memberLimit: number;
  expiresAt?: string;
  status: SpaceStatus;
  readOnly: boolean;
};
export type ModelDiscount = { modelInstanceId: string; percent: number };
export type TeamView = {
  id: string;
  name: string;
  ownerId: string;
  ownerName: string;
  ownerEmail?: string;
  memberCount: number;
  memberLimit: number;
  expiresAt: string;
  disabled: boolean;
  grantAmount: number;
  policy: SpacePolicy;
  openedAt: string;
  nextGrantAt: string;
  revision: number;
  status: SpaceStatus;
  balance: number;
  modelDiscounts: ModelDiscount[];
};
export type SpaceDetail = SpaceSummary & { team?: TeamView };
export type TeamInput = {
  ownerId: string;
  name: string;
  memberLimit: number;
  expiresAt: string;
  grantAmount: number;
  modelDiscounts: ModelDiscount[];
};
export type TeamPatch = {
  revision: number;
  expiresAt?: string;
  memberLimit?: number;
  grantAmount?: number;
  ownerId?: string;
  disabled?: boolean;
  modelDiscounts?: ModelDiscount[];
};
export type SettingsInput = {
  revision: number;
  name: string;
  policy: SpacePolicy;
};
export type TeamMember = {
  userId: string;
  name: string;
  email?: string;
  role: SpaceRole;
  joinedAt?: string;
};
export type Member = TeamMember;
export type Invite = {
  id: string;
  spaceId: string;
  teamName: string;
  kind: 'link' | 'email';
  inviteeUserId?: string;
  createdBy: string;
  createdAt: string;
  expiresAt: string;
  revokedAt?: string;
  status: string;
};
export type InviteLink = Invite & { token: string };
export type InvitePreview = {
  id: string;
  spaceId: string;
  teamName: string;
  expiresAt: string;
};
export type JoinRequest = {
  id: string;
  spaceId: string;
  userId: string;
  userName: string;
  message: string;
  status: string;
  decidedBy?: string;
  createdAt: string;
  decidedAt?: string;
};
export type OwnershipTransfer = {
  id: string;
  spaceId: string;
  fromOwnerId: string;
  toUserId: string;
  status: string;
  createdAt: string;
  acceptedAt?: string;
};
export type AuditEvent = {
  id: number;
  spaceId: string;
  actorId: string;
  actorKind: 'user' | 'operator';
  action: string;
  target: string;
  detail: unknown;
  createdAt: string;
};
export type AuditPage = { items: AuditEvent[]; nextCursor?: number };
export type MemberStatistics = {
  userId: string;
  name: string;
  holderBalance: number;
  credits: number;
  images: number;
  videos: number;
  projects: number;
};
export type Overview = {
  team: TeamView;
  range: '7d' | '30d' | 'contract';
  start: string;
  end: string;
  allocated: number;
  unallocated: number;
  daily: { date: string; credits: number }[];
  members: MemberStatistics[];
  recent: AuditEvent[];
};
export type ResourceRef = { kind: string; id: string };
export type TransferInput = {
  fromUserId: string;
  toUserId: string;
  all: boolean;
  resources: ResourceRef[];
};
export type TransferReceipt = ResourceRef & {
  transferred: boolean;
  errorCode?: string;
};
export type AllocationInput = {
  commandId: string;
  amount?: number;
  all?: boolean;
};
export type AllocationEvent = {
  id: string;
  operationId: string;
  accountId: string;
  holderId: string;
  kind: 'allocate' | 'reclaim' | 'release';
  amount: number;
  balance: number;
  actorId: string;
  createdAt: string;
};
export type SharingView = {
  spaceId: string;
  resourceKind: 'project' | 'canvas';
  resourceId: string;
  shares: { grantee: string; permission: 1 | 2 }[];
  canManage: boolean;
};
export type ProjectPermissions = {
  spaceId: string;
  ownerId: string;
  creatorId: string;
  access: ResourceAccess;
  canShare: boolean;
};
export type ProjectEditing = {
  userId: string;
  username: string;
  expiresAt: string | null;
  owned: boolean;
};
