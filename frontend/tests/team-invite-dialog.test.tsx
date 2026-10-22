import { test, beforeEach, after } from 'node:test';
import assert from 'node:assert/strict';
import React, { act, useState } from 'react';
import { JSDOM } from 'jsdom';
import type { Invite, InviteLink, JoinRequest } from '../src/api/spaces-types';

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
const original = { ...teamClient };
let root: ReturnType<typeof createRoot> | undefined;
const at = '2026-10-21T00:00:00Z';
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
  const button = buttons(name)[0];
  assert.ok(button, name);
  await act(async () => {
    button.focus();
    button.click();
  });
  await flush();
}
async function fill(value: string) {
  const field = document.querySelector<HTMLInputElement>('[role=dialog] input[type=email]')!;
  assert.ok(field);
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(field, value);
    field.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
async function method(name: string) {
  const trigger = buttons('邀请方式')[0];
  assert.ok(trigger);
  await act(async () => {
    trigger.focus();
    trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  });
  await flush();
  const item = [...document.querySelectorAll<HTMLElement>('[role=menuitemradio]')].find(
    (node) => node.textContent === name,
  )!;
  assert.ok(item, name);
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
function Controlled() {
  const [open, setOpen] = useState(false),
    [hidden, setHidden] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)}>外部邀请入口</button>
      <button onClick={() => setHidden((value) => !value)}>切换历史区域</button>
      <div hidden={hidden}>
        <InvitesPanel
          id="team"
          blocked={false}
          open={open}
          onOpenChange={setOpen}
        />
      </div>
    </>
  );
}
beforeEach(async () => {
  if (root) await act(async () => root!.unmount());
  root = undefined;
  sessionBoundary.reset();
  Object.assign(teamClient, original);
  teamClient.invites = async () => [];
  teamClient.requests = async () => [];
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: async () => {} },
    configurable: true,
  });
});
after(async () => {
  if (root) await act(async () => root!.unmount());
  Object.assign(teamClient, original);
  dom.window.close();
});

test('independent invitation is a single dialog with only the selected method, no permanent cards or audience field', async () => {
  await mount(<InvitesPanel id="team" blocked={false} />);
  assert.equal(document.querySelector('[role=dialog]'), null);
  assert.equal(document.querySelector('.team-invite-methods'), null);
  assert.equal(document.body.textContent!.includes('邀请对象'), false);
  await click('邀请成员');
  assert.ok(document.querySelector('[role=dialog] input[type=email]'));
  assert.equal(buttons('生成邀请链接').length, 0);
  await method('邀请链接');
  assert.equal(document.querySelector('[role=dialog] input[type=email]'), null);
  assert.equal(buttons('生成邀请链接').length, 1);
  assert.equal(document.querySelector('select'), null);
  await method('邮箱邀请');
  assert.ok(document.querySelector('[role=dialog] input[type=email]'));
});

test('controlled invitation opens from a single external action even with history hidden and preserves its parent controller', async () => {
  await mount(<Controlled />);
  assert.equal(buttons('邀请成员').length, 0);
  await click('切换历史区域');
  await click('外部邀请入口');
  await fill('draft@example.invalid');
  await click('取消');
  assert.equal(document.querySelector('[role=dialog]'), null);
  await click('外部邀请入口');
  assert.equal(
    document.querySelector<HTMLInputElement>('[role=dialog] input[type=email]')!.value,
    'draft@example.invalid',
  );
});

test('generated link stays in the dialog, can be copied locally, and closing never sends or fabricates another invitation', async () => {
  let writes = 0;
  const copied: string[] = [];
  teamClient.createLink = async () => {
    writes++;
    return link;
  };
  Object.defineProperty(navigator, 'clipboard', {
    value: {
      writeText: async (value: string) => {
        copied.push(value);
      },
    },
    configurable: true,
  });
  await mount(<InvitesPanel id="team" blocked={false} />);
  await click('邀请成员');
  await method('邀请链接');
  await click('生成邀请链接');
  const dialog = document.querySelector('[role=dialog]')!;
  const field = dialog.querySelector<HTMLInputElement>('input[aria-label="邀请链接"]')!;
  assert.ok(field.value.endsWith('#/invite/one-time-secret'));
  assert.equal(document.body.textContent!.includes('操作已完成'), false);
  await click('复制链接');
  assert.deepEqual(copied, [field.value]);
  await click('取消');
  assert.equal(document.querySelector('input[aria-label="邀请链接"]'), null);
  await click('邀请成员');
  assert.ok(document.querySelector('input[aria-label="邀请链接"]'));
  assert.equal(writes, 1);
});

test('email unknown survives actual dialog close, history switches and reopen; failed readback cannot unlock method or draft', async () => {
  const commands: string[] = [];
  let recheck: Promise<Invite[]> | undefined;
  let reject!: (error: Error) => void;
  teamClient.invites = async () => recheck ?? [];
  teamClient.inviteEmail = async (_id, email) => {
    commands.push(email);
    if (commands.length === 1) throw new ApiError(0, 'NETWORK', '提交未确认');
    return { ...link, kind: 'email' };
  };
  await mount(<InvitesPanel id="team" blocked={false} />);
  await click('邀请成员');
  await fill('draft@example.invalid');
  await click('发送邀请');
  assert.equal(buttons('邀请方式')[0].disabled, true);
  assert.equal(document.querySelector<HTMLInputElement>('input[type=email]')!.disabled, true);
  await click('取消');
  await click('加入申请');
  await click('邀请记录');
  assert.deepEqual(commands, ['draft@example.invalid']);
  await click('核对未确认操作');
  assert.equal(
    document.querySelector<HTMLInputElement>('input[type=email]')!.value,
    'draft@example.invalid',
  );
  assert.equal(buttons('邀请方式')[0].disabled, true);
  recheck = new Promise((_resolve, fail) => {
    reject = fail;
  });
  await click('读取最新状态');
  assert.equal(buttons('读取最新状态')[0].disabled, true);
  await act(async () => reject(new ApiError(0, 'NETWORK', '回读失败')));
  await flush();
  assert.equal(buttons('发送邀请')[0].disabled, true);
  assert.equal(buttons('邀请方式')[0].disabled, true);
  assert.match(document.querySelector('[role=dialog]')!.textContent!, /回读失败/);
  assert.equal(commands.length, 1);
  recheck = undefined;
  await click('读取最新状态');
  assert.equal(buttons('邀请方式')[0].disabled, false);
  assert.equal(commands.length, 1);
  await click('发送邀请');
  assert.deepEqual(commands, ['draft@example.invalid', 'draft@example.invalid']);
  assert.equal(document.querySelector('[role=dialog]'), null);
});

test('link unknown cannot switch method or regenerate before readback, including across close and reopen', async () => {
  let writes = 0;
  teamClient.createLink = async () => {
    writes++;
    if (writes === 1) throw new ApiError(0, 'NETWORK', '提交未确认');
    return link;
  };
  await mount(<InvitesPanel id="team" blocked={false} />);
  await click('邀请成员');
  await method('邀请链接');
  await click('生成邀请链接');
  assert.equal(buttons('邀请方式')[0].disabled, true);
  assert.equal(buttons('生成邀请链接')[0].disabled, true);
  await click('取消');
  await click('邀请成员');
  assert.equal(buttons('生成邀请链接')[0].disabled, true);
  assert.equal(document.querySelector('input[aria-label="邀请链接"]'), null);
  await click('读取最新状态');
  assert.equal(writes, 1);
  await click('生成邀请链接');
  assert.equal(writes, 2);
  assert.ok(document.querySelector('input[aria-label="邀请链接"]'));
});

test('history separates join approvals from invite records, keeping both mounted while approval refreshes once', async () => {
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
  let reads = 0,
    writes = 0,
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
  assert.equal(buttons('批准加入').length, 1);
  await click('批准加入');
  assert.equal(writes, 1);
  assert.equal(changed, 1);
  assert.equal(reads, 2);
  assert.equal(document.body.textContent!.includes('申请人甲'), false);
  const requests = document.querySelector('.team-request-list')!;
  await click('邀请记录');
  assert.ok(requests.closest('[hidden]'));
  assert.equal(document.querySelector('.team-invitation-records')!.closest('[hidden]'), null);
  await click('加入申请');
  assert.equal(document.querySelector('.team-request-list'), requests);
  assert.equal(requests.closest('[hidden]'), null);
  assert.equal(document.body.textContent!.includes('操作已完成'), false);
});
