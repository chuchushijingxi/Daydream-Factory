import test from 'node:test';
import assert from 'node:assert/strict';
import { workflow } from '../src/domain/project-workflow.ts';
const { state, tasks, prompt, contracts } = workflow;
const run = (id, contract, status = 'completed', sourceVersion = 1) => ({
  id,
  contract,
  status,
  sourceVersion,
  targets: [{ assetId: 'c' }],
});
const p = () => ({
  source: { version: 1 },
  assets: [{ id: 'c', looks: [] }],
  runs: [run('p3', contracts.extraction), run('visual', contracts.visual)],
  visualPrompts: [
    { target: { assetId: 'c' }, sourceVersion: 1, runId: 'visual', stale: false, text: '完整正文' },
  ],
});
test('current P3 follows contract/source/order; later tasks and old success never replace it', () => {
  const value = p();
  value.runs.push(
    run('old', contracts.extraction, 'completed', 0),
    run('latest', contracts.extraction, 'failed'),
    run('outline', contracts.outline),
  );
  assert.equal(tasks(value).extraction.id, 'latest');
  assert.equal(state(value).label, '项目失败');
  assert.equal(state(value).editable, false);
});
test('P3 completion opens editing; missing or invalidated prose does not close it', () => {
  const value = p();
  assert.equal(state(value).editable, true);
  value.assets[0].looks.push({ id: 'look' });
  assert.equal(state(value).editable, true);
  assert.equal(state(value).label, '资产内容待完善');
  assert.equal(state(value).generatedCount, 1);
  value.catalogEditedByUser = true;
  value.visualPrompts = [];
  assert.equal(state(value).editable, true);
});
test('unfinished P3 or any running task blocks writes; terminal scoped visual tasks do not', () => {
  for (const status of ['running', 'failed', 'cancelled']) {
    const value = p();
    value.runs[0].status = status;
    assert.equal(state(value).editable, false);
  }
  const value = p();
  value.runs.pop();
  assert.equal(state(value).editable, true);
  for (const status of ['running', 'failed', 'cancelled']) {
    value.runs[1] = run('new', contracts.visual, status);
    assert.equal(state(value).editable, status !== 'running');
  }
});
test('empty failure reason wins over completed P3 and partial prose; look ownership is exact', () => {
  const value = p();
  value.runs[1].status = 'failed';
  value.runs[1].targets = [];
  value.runs[1].error = '没有可生成视觉提示词的资产。';
  assert.equal(state(value).label, '项目失败');
  assert.match(state(value).reason, /没有可生成/);
  assert.equal(prompt(value, { assetId: 'c', lookId: 'l' }), null);
  assert.equal(prompt(value, { assetId: 'other' }), null);
});

test('one preparation run spans facts and design without premature completion', () => {
  const value = p();
  value.runs = [{ ...run('prepare', contracts.extraction, 'running'), stage: 'visualPrompt' }];
  value.visualPrompts[0].runId = 'prepare';
  assert.equal(state(value).label, '正在完善并核对资产');
  assert.equal(state(value).editable, false);
  assert.equal(state(value).generatedCount, 0);
  value.runs[0].status = 'completed';
  assert.equal(state(value).label, '');
  assert.equal(state(value).editable, true);
  assert.equal(state(value).generatedCount, 1);
  value.runs[0].status = 'failed';
  value.runs[0].error = '虚构视觉错误';
  assert.equal(state(value).editable, true);
  assert.equal(state(value).label, '资产生成失败');
  assert.equal(state(value).generatedCount, 0);
  value.visualPrompts[0].origin = 'user';
  value.visualPrompts[0].runId = '';
  assert.equal(state(value).generatedCount, 1);
  value.visualPrompts[0].stale = true;
  assert.equal(state(value).generatedCount, 0);
});
