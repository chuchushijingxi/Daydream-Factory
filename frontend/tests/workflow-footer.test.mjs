import test from 'node:test';
import assert from 'node:assert/strict';
import { createStoryboardFixture } from './storyboard-fixture.mjs';
import { workflow } from '../src/domain/project-workflow.ts';
import { scriptParsing } from '../src/domain/script-parsing.ts';
import { assetFooterState, scriptFooterState } from '../src/features/project/workflow-footer-state.ts';

const status = { readOnly: false, configured: true, capabilities: { storyboard: true } };
test('asset footer allows ready/read-only browsing and names missing content instead of generic advice', () => {
  const fixture = createStoryboardFixture(), project = fixture.project, index = fixture.index();
  let state = assetFooterState(project, status, index, '');
  assert.equal(state.disabled, false);
  assert.equal(state.tip, '资产已就绪，可选择分集');
  state = assetFooterState(project, { ...status, readOnly: true }, index, '');
  assert.equal(state.disabled, false);
  assert.match(state.tip, /只读查看分集/);
  project.visualPrompts[0].stale = true;
  state = assetFooterState(project, status, index, '');
  assert.equal(state.disabled, true);
  assert.match(state.tip, /图片提示词待完善/);
  assert.equal(state.values.count, 1);
});
test('asset footer never advertises a next step for empty, stale, loading, running or unknown inputs', () => {
  for (const kind of ['empty', 'stale', 'loading', 'running', 'unknown']) {
    const fixture = createStoryboardFixture(), project = fixture.project;
    let index = fixture.index();
    if (kind === 'empty') project.assets = [];
    if (kind === 'stale') index = { ...index, sourceVersion: 0 };
    if (kind === 'loading') index = null;
    if (kind === 'running') project.runs.push({ id: 'busy', contract: 'storyboard-agent-v1', status: 'running', sourceVersion: 1 });
    if (kind === 'unknown') project.runs.push({ id: 'unknown', contract: 'visual-prompt-agent-v1', status: 'failed', errorCode: 'MODEL_RESULT_UNKNOWN', sourceVersion: 1 });
    const state = assetFooterState(project, status, index, '');
    assert.equal(state.disabled, true, kind);
    assert.doesNotMatch(state.tip, /可选择分集/, kind);
    assert.ok(state.tip.length > 0, kind);
  }
});
test('script footer presents actionable prerequisites and keeps completed browsing independent of model availability', () => {
  const fixture = createStoryboardFixture();
  fixture.setScenario('parse-ready');
  const project = fixture.project;
  const task = { project, flow: workflow.state(project), readonly: false, pending: 0, sending: false,
    readback: false, taskAvailable: true, selectionMissing: false, modelMissing: false,
    indexing: false, blocked: false, quote: { blocked: false } };
  const progress = scriptParsing(project, fixture.index(), true);
  assert.equal(scriptFooterState(task, progress, false).tip, '剧本已就绪，下一步开始解析');
  const missing = scriptFooterState({ ...task, selectionMissing: true, blocked: true }, progress, false);
  assert.equal(missing.disabled, true);
  assert.equal(missing.tip, '请先选择分析模型，再开始解析');
  const unknown = scriptFooterState({ ...task, readback: true, blocked: true }, progress, false);
  assert.equal(unknown.disabled, true);
  assert.match(unknown.tip, /结果待确认/);
  assert.match(scriptFooterState({ ...task, blocked: true }, progress, false, '读取失败').tip, /读取失败/);
  fixture.setScenario('parse-complete');
  const completed = scriptFooterState({ ...task, flow: workflow.state(project), selectionMissing: true, blocked: true },
    scriptParsing(project, fixture.index(), true), false);
  assert.equal(completed.disabled, false);
  assert.equal(completed.tip, '解析完成，可查看资产');
});
