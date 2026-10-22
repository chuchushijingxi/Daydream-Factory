import { test, beforeEach, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act } from 'react';
import { JSDOM } from 'jsdom';
import type { Invite, InviteLink, JoinRequest, TeamMember } from '../src/api/spaces-types';

const dom = new JSDOM('<div id="root"></div>', {
  url: 'http://127.0.0.1/demo/app/',
  pretendToBeVisual: true,
});
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLTextAreaElement',
  'HTMLButtonElement',
  'HTMLSelectElement',
  'Element',
  'Node',
  'NodeFilter',
  'DocumentFragment',
  'SVGElement',
  'MutationObserver',
  'Event',
  'MouseEvent',
  'CustomEvent',
  'KeyboardEvent',
  'localStorage',
  'sessionStorage',
])
  Object.defineProperty(globalThis, key, { value: dom.window[key], configurable: true });
globalThis.getComputedStyle = dom.window.getComputedStyle;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
globalThis.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const { createRoot } = await import('react-dom/client');
const { LanguageProvider } = await import('../src/contexts/LanguageContext');
const { sessionBoundary } = await import('../src/api/session-boundary');
const { teamClient } = await import('../src/api/team-client');
const { ApiError } = await import('../src/api/http-error');
const { InvitesPanel } = await import('../src/pages/team/InvitesPanel');
const { TeamResourceTransferPanel } = await import('../src/pages/team/TeamResourceTransferPanel');
const { JoinTeamDialog } = await import('../src/pages/team/JoinTeamDialog');
const original = { ...teamClient };
let root: ReturnType<typeof createRoot> | undefined;
const at = '2026-10-21T00:00:00Z';
const members: TeamMember[] = [
  { userId: 'owner', name: '所有者甲', role: 'owner' },
  { userId: 'recipient', name: '接收成员乙', role: 'member' },
];
const link: InviteLink = {
  id: 'invite',
  spaceId: 'team',
  kind: 'link',
  inviterUserId: 'owner',
  status: 'pending',
  createdAt: at,
  expiresAt: '2027-01-01T00:00:00Z',
  token: 'one-time-secret',
};
const flush = async () => {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
};
const buttons = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].filter(
    (item) =>
      !item.closest('[hidden]') &&
      (item.textContent?.trim() === name || item.getAttribute('aria-label') === name),
  );
async function click(name: string) {
  assert.ok(buttons(name)[0], name);
  await act(async () => buttons(name)[0].click());
  await flush();
}
async function fill(field: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
async function selectInviteMethod(value: string) {
  const trigger = buttons('邀请方式')[0];
  assert.ok(trigger);
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });
  await flush();
  const item = [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].find(node => node.textContent === value)!;
  assert.ok(item);
  await act(async () => item.click());
  await flush();
}
async function selectRecipient() {
  const trigger = buttons('请选择接收成员')[0];
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });
  await flush();
  const item = [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].find(
    (node) => node.textContent === '接收成员乙',
  )!;
  assert.ok(item);
  await act(async () => item.click());
  await flush();
}
async function mount(child: React.ReactNode) {
  root = createRoot(document.getElementById('root')!);
  await act(async () =>
    root!.render(
      <LanguageProvider>
        <section className="team-workspace-page">{child}</section>
      </LanguageProvider>,
    ),
  );
  await flush();
}
beforeEach(async () => {
  if (root) await act(async () => root!.unmount());
  root = undefined;
  sessionBoundary.reset();
  Object.assign(teamClient, original);
  teamClient.members = async () => members;
  teamClient.invites = async () => [];
  teamClient.requests = async () => [];
});
after(async () => {
  if (root) await act(async () => root!.unmount());
  Object.assign(teamClient, original);
  dom.window.close();
});

test('a single invitation dialog preserves email draft between methods and shows the generated token only as a result', async () => {
  let creates = 0;
  teamClient.createLink = async () => { creates++; return link; };
  await mount(<InvitesPanel id="team" blocked={false} />);
  assert.equal(document.querySelector('input[type=email]'), null);
  await click('邀请成员');
  await fill(document.querySelector<HTMLInputElement>('input[type=email]')!, 'draft@example.invalid');
  await selectInviteMethod('邀请链接');
  assert.equal(document.querySelector('input[type=email]'), null);
  assert.equal(document.body.textContent!.includes('邀请对象'), false);
  await click('生成邀请链接');
  assert.equal(creates, 1);
  assert.equal(document.querySelector<HTMLInputElement>('input[aria-label="邀请链接"]')!.value.endsWith('#/invite/one-time-secret'), true);
  await selectInviteMethod('邮箱邀请');
  assert.equal(document.querySelector<HTMLInputElement>('input[type=email]')!.value, 'draft@example.invalid');
  assert.equal(document.body.textContent!.includes('操作已完成'), false);
});

test('invitation unknown keeps draft and mode locked while explicit readback fails, then unlocks without resending', async () => {
  let writes = 0;
  let recheck: Promise<Invite[]> | undefined;
  let reject!: (error: Error) => void;
  teamClient.invites = async () => recheck ?? [];
  teamClient.createLink = async () => { writes++; throw new ApiError(0, 'NETWORK', '提交未确认'); };
  await mount(<InvitesPanel id="team" blocked={false} />);
  await click('邀请成员');
  await fill(document.querySelector<HTMLInputElement>('input[type=email]')!, 'draft@example.invalid');
  await selectInviteMethod('邀请链接');
  await click('生成邀请链接');
  assert.equal(buttons('邀请方式')[0].disabled, true);
  assert.equal(buttons('生成邀请链接')[0].disabled, true);
  recheck = new Promise((_resolve, fail) => { reject = fail; });
  await click('读取最新状态');
  assert.equal(buttons('读取最新状态')[0].disabled, true);
  await act(async () => reject(new ApiError(0, 'NETWORK', '回读失败')));
  await flush();
  assert.equal(buttons('邀请方式')[0].disabled, true);
  assert.match(document.body.textContent!, /回读失败/);
  recheck = undefined;
  await click('读取最新状态');
  assert.equal(buttons('邀请方式')[0].disabled, false);
  await selectInviteMethod('邮箱邀请');
  assert.equal(document.querySelector<HTMLInputElement>('input[type=email]')!.value, 'draft@example.invalid');
  assert.equal(writes, 1);
});

test('successful join approval removes the old request and never nests a blocked readback or repeats the write', async () => {
  let rows: JoinRequest[] = [
    {
      id: 'request',
      spaceId: 'team',
      userId: 'applicant',
      userName: '申请人甲',
      message: '申请加入',
      status: 'pending',
      createdAt: at,
    },
  ];
  let writes = 0,
    reads = 0,
    changed = 0;
  teamClient.requests = async () => {
    reads++;
    return rows;
  };
  teamClient.decideRequest = async () => {
    writes++;
    rows = [];
    return {};
  };
  await mount(
    <InvitesPanel
      id="team"
      blocked={false}
      onChanged={() => {
        changed++;
      }}
    />,
  );
  await click('加入申请');
  assert.match(document.body.textContent!, /申请人甲/);
  await click('批准加入');
  assert.equal(writes, 1);
  assert.equal(changed, 1);
  assert.equal(reads, 2);
  assert.equal(document.body.textContent!.includes('申请人甲'), false);
  assert.equal(document.body.textContent!.includes('操作已完成'), false);
});

test('resource handoff is all-only, rejects current members as the former owner, and preserves partial receipts after refresh', async () => {
  const commands: unknown[] = [];
  let changed = 0;
  teamClient.transfer = async (_id, input) => {
    commands.push(input);
    return [
      { kind: 'project', id: 'project-a', transferred: true },
      { kind: 'media', id: 'media-b', transferred: false, errorCode: 'NOT_FOUND' },
    ];
  };
  await mount(
    <TeamResourceTransferPanel
      id="team"
      onSaved={() => {
        changed++;
      }}
    />,
  );
  const field = document.querySelector<HTMLInputElement>('.team-resource-transfer input')!;
  assert.equal(document.querySelector('input[type=checkbox]'), null);
  assert.equal(document.querySelector('[aria-label="资源 ID"]'), null);
  assert.equal(document.querySelector('select'), null);
  await fill(field, 'owner');
  await selectRecipient();
  await click('核对并移交');
  assert.match(document.body.textContent!, /只能移交已经离队/);
  assert.equal(commands.length, 0);
  await fill(field, 'former');
  await click('核对并移交');
  assert.match(document.querySelector('[role=alertdialog]')!.textContent!, /全部资源/);
  await click('确认');
  assert.deepEqual(commands, [
    { fromUserId: 'former', toUserId: 'recipient', all: true, resources: [] },
  ]);
  assert.equal(changed, 1);
  assert.match(document.body.textContent!, /逐项移交结果/);
  assert.match(document.body.textContent!, /已移交/);
  assert.match(document.body.textContent!, /未移交：NOT_FOUND/);
  assert.match(document.body.textContent!, /保留原创建人/);
});

test('unknown resource handoff locks inputs across dismissing confirmation and only successful readback allows the same explicit all-only retry', async () => {
  const commands: unknown[] = [];
  let recheck: Promise<TeamMember[]> | undefined;
  let reject!: (error: Error) => void;
  teamClient.members = async () => recheck ?? members;
  teamClient.transfer = async (_id, input) => {
    commands.push(input);
    if (commands.length === 1) throw new ApiError(0, 'NETWORK', '提交未确认');
    return [];
  };
  await mount(<TeamResourceTransferPanel id="team" onSaved={() => {}} />);
  const field = document.querySelector<HTMLInputElement>('.team-resource-transfer input')!;
  await fill(field, 'former');
  await selectRecipient();
  await click('核对并移交');
  await click('确认');
  assert.equal(field.disabled, true);
  await click('取消');
  assert.equal(field.value, 'former');
  assert.equal(buttons('核对并移交')[0].disabled, true);
  assert.equal(buttons('请选择接收成员')[0].disabled, true);
  assert.equal(commands.length, 1);
  recheck = new Promise((_resolve, fail) => {
    reject = fail;
  });
  await click('读取最新状态');
  await act(async () => reject(new ApiError(0, 'NETWORK', '回读失败')));
  await flush();
  assert.equal(field.disabled, true);
  assert.equal(commands.length, 1);
  recheck = undefined;
  await click('读取最新状态');
  assert.equal(field.disabled, false);
  assert.equal(field.value, 'former');
  assert.equal(commands.length, 1);
  await click('核对并移交');
  await click('确认');
  assert.equal(commands.length, 2);
  assert.deepEqual(commands[1], commands[0]);
  assert.match(document.body.textContent!, /没有找到可移交的资源/);
});

test('join dialog sends the request in one submit and echoes the resolved team name', async () => {
  const calls: string[] = [];
  teamClient.lookup = async (id) => {
    calls.push('lookup:' + id);
    if (id === 'missing') throw new ApiError(404, 'NOT_FOUND', '');
    return { name: '验收团队' };
  };
  teamClient.join = async (id, message) => {
    calls.push('join:' + id + ':' + message);
    return {} as JoinRequest;
  };
  await mount(<JoinTeamDialog open onOpenChange={() => {}} />);
  assert.equal(buttons('加入团队').length, 0, 'no separate lookup step');
  const id = document.querySelector<HTMLInputElement>('.team-join-dialog input')!;
  await fill(id, 'missing');
  await click('发送加入申请');
  assert.deepEqual(calls, ['lookup:missing']);
  assert.match(document.body.textContent!, /未找到该团队，请核对团队 ID。/);
  assert.equal(document.body.textContent!.includes('NOT_FOUND'), false);
  await fill(id, ' team ');
  const note = document.querySelector<HTMLTextAreaElement>('.team-join-dialog textarea')!;
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(note, '你好');
    note.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await click('发送加入申请');
  assert.deepEqual(calls.slice(1), ['lookup:team', 'join:team:你好']);
  assert.match(document.body.textContent!, /已向「验收团队」发送加入申请/);
  assert.equal(document.body.textContent!.includes('操作已完成'), false);
  assert.equal(document.querySelector('.team-join-dialog form'), null);
});

test('a request already handled elsewhere refreshes the list instead of locking the panel', async () => {
  let rows: JoinRequest[] = [
    {
      id: 'request',
      spaceId: 'team',
      userId: 'applicant',
      userName: '申请人乙',
      message: '',
      status: 'pending',
      createdAt: at,
    },
  ];
  let writes = 0;
  teamClient.requests = async () => rows;
  teamClient.decideRequest = async () => {
    writes++;
    rows = [];
    throw new ApiError(409, 'TEAM_REQUEST_CONFLICT', 'TEAM_REQUEST_CONFLICT: 加入申请已处理');
  };
  await mount(<InvitesPanel id="team" blocked={false} />);
  await click('加入申请');
  await click('批准加入');
  assert.equal(writes, 1);
  const text = document.body.textContent!;
  assert.equal(text.includes('申请人乙'), false, 'handled request leaves the pending list');
  assert.equal(text.includes('TEAM_REQUEST_CONFLICT'), false);
  assert.equal(text.includes('提交结果或版本未确认'), false);
  assert.equal(buttons('核对未确认操作').length, 0);
});
