import test from 'node:test';
import assert from 'node:assert/strict';
import { createHttpClient } from '../src/api/client.ts';
import { createPlatformApi } from '../src/api/platform-client.ts';
import { SessionBoundary } from '../src/api/session-boundary.ts';

const initial = { revision: 0, name: '短剧 Agent', sidebarDescription: '创作工作台', logoId: '', logoUrl: '' };
const logoId = 'a'.repeat(64), logoUrl = '/api/platform-logos/' + logoId;
function setup(reply) {
  const calls = [], boundary = new SessionBoundary();
  const api = createPlatformApi(createHttpClient(async (url, options) => {
    calls.push({ url, options });
    return reply(url, options);
  }, boundary));
  return { api, calls, boundary };
}
test('platform uses the existing public/protected Cookie transport and projects only owned fields', async () => {
  const { api, calls } = setup(() => Response.json({ ...initial, unrelated: 'excluded' }));
  assert.deepEqual(await api.publicSettings(), initial);
  assert.deepEqual(await api.settings(), initial);
  assert.deepEqual(calls.map((call) => call.url), ['/api/platform-settings', '/api/operations/platform-settings']);
  assert.ok(calls.every(({ options }) => options.credentials === 'same-origin' && options.cache === 'no-store'));
  assert.ok(calls.every(({ options }) => new Headers(options.headers).has('X-Drama-Audience')));
});
test('a public brand failure cannot revoke an authenticated session; protected failures still do', async () => {
  const { api, boundary } = setup(() => Response.json({ code: 'AUTH_REQUIRED' }, { status: 401 }));
  const before = boundary.epoch;
  await assert.rejects(api.publicSettings(), { code: 'AUTH_REQUIRED' });
  assert.equal(boundary.epoch, before);
  await assert.rejects(api.settings(), { code: 'AUTH_REQUIRED' });
  assert.equal(boundary.epoch, before + 1);
});
test('save sends the three fields atomically, trims boundaries, and adopts only a matching CAS receipt', async () => {
  const { api, calls } = setup(() => Response.json({ ...initial, revision: 1, name: '测试平台', logoId, logoUrl }));
  const saved = await api.save({ ...initial, name: ' 测试平台 ', logoId });
  assert.equal(saved.revision, 1);
  assert.deepEqual(JSON.parse(calls[0].options.body), {
    revision: 0, name: '测试平台', sidebarDescription: '创作工作台', logoId,
  });
  assert.equal(calls[0].options.method, 'POST');
  for (const mismatch of [{ revision: 0 }, { revision: 2 }, { name: '另一个' }, { logoId: '', logoUrl: '' }]) {
    const { api: bad } = setup(() => Response.json({ ...saved, ...mismatch }));
    await assert.rejects(bad.save({ ...initial, name: '测试平台', logoId }), { code: 'PROGRAM_FAILED' });
  }
});
test('invalid settings and cross-module Logo URLs are rejected, never silently defaulted', async () => {
  for (const fields of [
    { revision: Number.MAX_SAFE_INTEGER + 1 }, { revision: -1 }, { name: '' },
    { name: 'a'.repeat(49) }, { name: 'a\nb' }, { sidebarDescription: 'a'.repeat(121) },
    { logoId, logoUrl: '/api/model-logos/' + logoId }, { logoId: '', logoUrl },
  ]) {
    const { api } = setup(() => Response.json({ ...initial, ...fields }));
    await assert.rejects(api.publicSettings(), { code: 'PROGRAM_FAILED' });
  }
  const { api, calls } = setup(() => Response.json(initial));
  await assert.rejects(api.save({ ...initial, revision: Number.MAX_SAFE_INTEGER }), { code: 'PLATFORM_SETTINGS_INVALID' });
  assert.equal(calls.length, 0);
});
test('upload sends raw bytes without adopting settings; invalid files never leave the client', async () => {
  const file = new File(['png'], 'logo.png', { type: 'image/png' });
  const { api, calls } = setup(() => Response.json({ logoId, logoUrl }));
  assert.deepEqual(await api.upload(file), { logoId, logoUrl });
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, '/api/operations/platform-logos');
  assert.equal(calls[0].options.body, file);
  assert.equal(new Headers(calls[0].options.headers).get('Content-Type'), 'image/png');
  await assert.rejects(api.upload(new File(['svg'], 'logo.svg', { type: 'image/svg+xml' })), { code: 'INVALID_PLATFORM_LOGO' });
  await assert.rejects(api.upload(new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'large.png', { type: 'image/png' })), { code: 'INVALID_PLATFORM_LOGO' });
  assert.equal(calls.length, 1);
});
test('unknown or conflicting saves are sent once and stale public results are fenced', async () => {
  for (const status of [409, 503]) {
    const { api, calls } = setup(() => Response.json({ code: status === 409 ? 'PLATFORM_SETTINGS_CONFLICT' : 'PLATFORM_SETTINGS_SAVE_UNKNOWN' }, { status }));
    await assert.rejects(api.save(initial));
    assert.equal(calls.length, 1);
  }
  let release;
  const { api, boundary } = setup(() => new Promise((resolve) => { release = resolve; }));
  const pending = api.publicSettings();
  boundary.reset();
  release(Response.json(initial));
  await assert.rejects(pending, { code: 'SESSION_CHANGED' });
});
