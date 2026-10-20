// Explicit, isolated fictional browser fixture. Never imported by production.
import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { HashRouter } from 'react-router-dom';
import { LanguageProvider } from '../src/contexts/LanguageContext';
import { SessionProvider } from '../src/state/SessionContext';
import { Application } from '../src/Application';
import { Toaster } from '../src/components/ui/sonner';
import { Button } from '../src/components/ui/button';
import { ProjectRunStatus } from '../src/pages/projects/ProjectRunStatus';
import { createAdministrationFixture, creditStats } from './administration-fixture';
import '../src/main.css';
import '../src/styles/jimeng-controls.css';
import '../src/styles/workspace.css';
import '../src/styles/model-config.css';
import '../src/styles/date-time-picker.css';
import '../src/styles/administration.css';
import '../src/styles/mail-settings.css';
import '../src/styles/account-profile.css';
import '../src/styles/personal-credits.css';
import '../src/styles/notifications.css';
import '../src/styles/interface-controls.css';
import '../src/styles/feedback.css';
const fixture = createAdministrationFixture();
const query = new URLSearchParams(location.search);
if (query.get('user') === 'ordinary')
  fixture.user = {
    identity: 'fixture_ordinary',
    username: '虚构用户',
    email: 'ordinary@example.invalid',
    operationsAllowed: false,
    roleLabel: '普通用户',
  };
if (query.get('user') === 'anonymous') fixture.user = null;
// Explicit visual-only samples: no PG, real account or model request.
if (query.get('credit-records') === '1') {
  fixture.wallet = {
    balance: '164',
    rechargeBalance: '104',
    giftBalance: '60',
    legacyBalance: '0',
  };
}
if (query.get('credit-records') === '1')
  fixture.service.myLedger = async (page) => ({
    ...creditStats,
    summary: {
      acquired: '160',
      consumed: '19',
      refunded: '7',
      netConsumed: '12',
      settledConsumed: '12',
      pending: '0',
    },
    analysis: [{ source: 'language-generation', amount: '12' }],
    items: [
      {
        identity: 'fixture-success-receipt',
        at: '2026-10-17T02:00:00Z',
        direction: 'deduct',
        category: 'mixed',
        rechargeAmount: '2',
        giftAmount: '10',
        legacyAmount: '0',
        amount: '12',
        balance: '164',
        reason: '虚构测试 · 文本生成',
        source: 'language-generation:fixture',
        originalDebit: null,
        actorLabel: 'fixture-user',
      },
      {
        identity: 'fixture-refund-receipt',
        at: '2026-10-17T01:00:00Z',
        direction: 'refund',
        category: 'gift',
        giftAmount: '7',
        rechargeAmount: '0',
        legacyAmount: '0',
        amount: '7',
        balance: '9007199254740990',
        reason: '虚构测试 · 取消返还',
        source: 'fixture-generation',
        originalDebit: 'fixture-deduction',
        actorLabel: 'fixture-system',
      },
      {
        identity: 'fixture-deduction',
        at: '2026-10-16T06:00:00Z',
        direction: 'deduct',
        category: 'gift',
        giftAmount: '7',
        rechargeAmount: '0',
        legacyAmount: '0',
        amount: '7',
        balance: '9007199254740983',
        reason: '虚构测试 · 整剧解析',
        source: 'fixture-generation',
        originalDebit: null,
        actorLabel: 'fixture-user',
      },
      {
        identity: 'fixture-add-receipt',
        at: '2026-10-16T05:00:00Z',
        direction: 'add',
        category: 'recharge',
        rechargeAmount: '100',
        giftAmount: '0',
        legacyAmount: '0',
        amount: '100',
        balance: '9007199254740990',
        reason: '虚构测试 · 积分调整',
        source: 'fixture-adjustment',
        originalDebit: null,
        actorLabel: 'fixture-operator',
      },
    ],
    previous: page ? '1' : null,
    next: page ? null : '2',
    caption: '虚构测试数据 · 共6条 · 第' + (page || '1') + '页',
  });

if (!location.hash) location.hash = '#/operations/usage';
// Only local fictional metadata and CAS drafts; no real API/PG/model is contacted.
let registry = {
  revision: 1,
  editable: true,
  connections: [
    {
      id: 'fixture_mock',
      name: '本地 Mock',
      kind: 'builtin',
      protocol: 'mock-media',
      baseUrl: '',
      credentialConfigured: true,
      editable: true,
      reason: '',
      models: [
        {
          id: 'mock-image',
          logoId: '',
          description: '隔离夹具的虚构图片模型',
          modelId: 'mock-image',
          alias: '',
          category: 'image',
          enabled: true,
          contextWindow: 0,
          maxOutputTokens: 0,
          pricing: null,
          creditPrice: { unitPrice: 3, freeEnabled: false, freeStart: null, freeEnd: null },
          name: '虚构图片模型',
          provider: 'mock',
          logoUrl: '',
          defaultLogoUrl: '',
          available: true,
          reason: '',
          referenced: false,
        },
      ],
    },
  ],
};
const notificationReads = new Map<string, string>();
const notificationMessages = Array.from({ length: 23 }, (_, i) => ({
  id: 'fixture_notice_' + i,
  title:
    i === 0
      ? '虚构通知 · 积分功能更新'
      : i === 1
        ? '虚构通知 · 长标题换行与阅读路径检查'
        : '虚构系统消息 ' + (i + 1),
  body:
    i === 0
      ? '## 积分功能更新\n\n这是一封**虚构站内信**，只用于界面验证。\n\n- 充值与赠送分别展示\n- 查看已获取和已消耗\n- 原编号回读，避免重复记账\n\n> 此处没有真实投递和模型调用。\n\n[安全示例链接](https://example.com)\n\n![仅展示图片说明](https://example.com/no-request.png)'
      : '虚构通知正文 ' + (i + 1),
  audience: 'all',
  recipientUserId: '',
  recipientName: '',
  recipientEmail: '',
  authorId: 'fixture_operator',
  authorName: '虚构发布人',
  withdrawnAt: null,
  status: 'published',
  revision: 2,
  recipientCount: 2,
  createdAt: '2026-10-17T01:00:00Z',
  publishedAt: '2026-10-17T01:00:00Z',
}));
const notificationCommands = new Map<string, string>();
function notificationFixture(url: string, options: RequestInit) {
  const parsed = new URL(url, location.origin),
    path = parsed.pathname,
    body = options.body ? JSON.parse(String(options.body)) : {};
  const asItem = (v: (typeof notificationMessages)[number]) => ({
    id: v.id,
    title: v.title,
    summary:
      v.id === 'fixture_notice_0'
        ? '积分功能更新：充值与赠送分别展示，支持查看获取和消耗记录。'
        : '虚构通知摘要 ' + v.id.split('_').at(-1),
    publishedAt: v.publishedAt,
    readAt: notificationReads.get(v.id) || null,
  });
  const unread = () =>
    notificationMessages.filter((v) => v.status === 'published' && !notificationReads.has(v.id))
      .length;
  if (path === '/api/notifications/unread') return Response.json({ unread: unread() });
  if (path === '/api/notifications/read-all') {
    const updated = unread();
    notificationMessages
      .filter((v) => v.status === 'published')
      .forEach((v) => notificationReads.set(v.id, new Date().toISOString()));
    return Response.json({ updated, unread: 0 });
  }
  if (path === '/api/notifications') {
    const page = Number(parsed.searchParams.get('page') || 1),
      status = parsed.searchParams.get('status') || 'all',
      items = notificationMessages
        .filter((v) => v.status === 'published')
        .map(asItem)
        .filter((v) => status === 'all' || (status === 'read' ? !!v.readAt : !v.readAt));
    return Response.json({
      items: items.slice((page - 1) * 20, page * 20),
      total: items.length,
      page,
      pageSize: 20,
      unread: unread(),
    });
  }
  if (path.startsWith('/api/notifications/')) {
    const id = path.split('/')[3],
      value = notificationMessages.find((v) => v.id === id);
    if (!value)
      return Response.json({ code: 'NOT_FOUND', message: '虚构通知不存在' }, { status: 404 });
    if (path.endsWith('/read')) {
      if (!notificationReads.has(id)) notificationReads.set(id, new Date().toISOString());
      return Response.json({ id, readAt: notificationReads.get(id) });
    }
    return Response.json({ ...asItem(value), body: value.body });
  }
  if (path.startsWith('/api/operations/notification-commands/')) {
    const value = notificationMessages.find(
      (v) => v.id === notificationCommands.get(decodeURIComponent(path.split('/').at(-1)!)),
    );
    return value
      ? Response.json(value)
      : Response.json({ code: 'NOT_FOUND', message: '虚构命令尚未保存' }, { status: 404 });
  }
  if (path === '/api/operations/notifications') {
    if (options.method === 'POST') {
      const value = {
        ...notificationMessages[0],
        ...body,
        id: 'fixture_notice_draft_' + notificationMessages.length,
        status: 'draft',
        revision: 1,
        recipientCount: 0,
        publishedAt: null,
      } as unknown as (typeof notificationMessages)[number];
      delete (value as { commandId?: string }).commandId;
      notificationMessages.unshift(value);
      notificationCommands.set(body.commandId, value.id);
      return Response.json(value, { status: 201 });
    }
    const page = Number(parsed.searchParams.get('page') || 1);
    return Response.json({
      items: notificationMessages.slice((page - 1) * 20, page * 20),
      total: notificationMessages.length,
      page,
      pageSize: 20,
      unread: 0,
    });
  }
  if (path.startsWith('/api/operations/notifications/')) {
    const id = path.split('/')[4],
      index = notificationMessages.findIndex((v) => v.id === id);
    if (index < 0)
      return Response.json({ code: 'NOT_FOUND', message: '虚构通知不存在' }, { status: 404 });
    let value = notificationMessages[index];
    if (options.method === 'POST') {
      if (body.revision !== value.revision || value.status !== 'draft')
        return Response.json(
          { code: 'NOTIFICATION_CONFLICT', message: '虚构版本冲突' },
          { status: 409 },
        );
      value = path.endsWith('/publish')
        ? {
            ...value,
            status: 'published',
            revision: value.revision + 1,
            recipientCount: 2,
            publishedAt: new Date().toISOString(),
          }
        : { ...value, ...body, revision: value.revision + 1 };
      notificationMessages[index] = value;
    }
    return Response.json(value);
  }
  return null;
}
globalThis.fetch = async (url, options = {}) => {
  if (query.get('notifications') === '1') {
    const response = notificationFixture(String(url), options);
    if (response) return response;
  }
  if (url === '/api/status')
    return Response.json({
      readOnly: false,
      configured: false,
      capabilities: { modelConfiguration: true },
    });
  if (url === '/api/model-config') {
    if (options.method === 'POST') {
      const body = JSON.parse(String(options.body));
      if (body.revision !== registry.revision)
        return Response.json({ code: 'CONFLICT', message: '虚构版本冲突' }, { status: 409 });
      registry = {
        ...registry,
        revision: registry.revision + 1,
        connections: [
          {
            ...registry.connections[0],
            ...body.connection,
            models: body.connection.models.map((m: any) => ({
              ...registry.connections[0].models[0],
              ...m,
            })),
          },
        ],
      };
    }
    return Response.json(registry);
  }
  if (url === '/api/models')
    return Response.json({ analysis: [], image: [], video: [], catalog: [] });
  if (url === '/api/projects') return Response.json([]);
  if (url === '/api/visual-styles') return Response.json({ styles: [] });
  return Response.json(
    { code: 'FIXTURE_UNSUPPORTED', message: '隔离夹具未覆盖此操作。' },
    { status: 503 },
  );
};
function SemanticFixture() {
  const [dark, setDark] = useState(false);
  return (
    <main
      style={{
        minHeight: '100vh',
        padding: 24,
        background: 'var(--jm-workspace)',
        color: 'var(--jm-text)',
      }}
    >
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          marginBottom: 24,
        }}
      >
        <h1 style={{ fontSize: 18 }}>工作台状态与操作 · 隔离颜色验证</h1>
        <Button
          variant="outline"
          onClick={() => {
            setDark(!dark);
            document.documentElement.classList.toggle('dark', !dark);
          }}
        >
          切换测试主题
        </Button>
      </header>
      <p style={{ fontSize: 12, color: 'var(--jm-muted)', marginBottom: 24 }}>
        全部虚构，不连接PG、不启动任务；下面复用正式工作台状态和按钮组件。
      </p>
      <section
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit,minmax(200px,1fr))',
          gap: 16,
        }}
      >
        {(['running', 'completed', 'failed', 'cancelled'] as const).map((status) => (
          <div
            key={status}
            style={{
              height: 96,
              background: 'var(--jm-panel)',
              border: '1px solid var(--jm-border)',
              borderRadius: 12,
              position: 'relative',
              padding: 20,
              fontSize: 13,
            }}
          >
            虚构项目
            <ProjectRunStatus
              run={{ id: status, contract: 'catalog-agent-v3', status, stage: 'extract' }}
            />
          </div>
        ))}
      </section>
      <section style={{ marginTop: 28, display: 'flex', flexWrap: 'wrap', gap: 12 }}>
        <Button variant="accent">开始解析</Button>
        <Button variant="accent">重新生成提示词</Button>
        <Button variant="success">保存镜头</Button>
        <Button variant="success">采用图片</Button>
        <Button variant="warning">取消任务</Button>
        <Button variant="destructive">删除镜头</Button>
        <Button variant="outline">查看过程</Button>
        <Button variant="accent" disabled>
          生成暂不可用
        </Button>
      </section>
      <section style={{ marginTop: 24, display: 'flex', gap: 12, flexWrap: 'wrap' }}>
        <span className="jm-status" data-tone="warning">
          结果待确认
        </span>
        <span className="jm-status" data-tone="warning">
          未保存草稿
        </span>
        <span className="jm-status" data-tone="success">
          已保存
        </span>
        <span className="jm-status" data-tone="neutral">
          待处理
        </span>
      </section>
    </main>
  );
}
createRoot(document.getElementById('root')!).render(
  <LanguageProvider>
    {query.get('semantics') === '1' ? (
      <SemanticFixture />
    ) : (
      <HashRouter>
        <SessionProvider service={fixture.service}>
          <Application />
          <Toaster />
          <span className="fixture-banner">隔离夹具 · 全部虚构 · 无真实接口/模型调用</span>
        </SessionProvider>
      </HashRouter>
    )}
  </LanguageProvider>,
);
