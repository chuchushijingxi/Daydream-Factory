import type { Project } from '../api/types';
import type { StoryboardIndexView } from '../api/storyboard-types';
import { workflow } from './project-workflow';
import { indexReady } from './storyboard';

export function scriptParsing(
  project: Project,
  index: StoryboardIndexView | null,
  storyboard: boolean,
) {
  const flow = workflow.state(project),
    catalog = flow.extraction,
    visual = flow.visual;
  const indexRun =
    index?.latestIndexRun?.sourceVersion === project.source.version
      ? index.latestIndexRun
      : [...project.runs]
          .reverse()
          .find(
            (r) =>
              r.contract === 'episode-index-agent-v1' && r.sourceVersion === project.source.version,
          );
  const hasIndex = storyboard && !!(index?.index || indexRun);
  const complete =
    workflow.catalogReady(project) &&
    project.assets.length > 0 &&
    workflow.pendingTargets(project).length === 0 &&
    (!visual || visual.status === 'completed') &&
    catalog?.status === 'completed' &&
    (!storyboard || !!index) &&
    (!hasIndex || indexReady(index, project.source.version));
  const steps = [
    ...(hasIndex
      ? [
          {
            key: 'episodeIndex',
            label: '识别分集',
            done: indexReady(index, project.source.version),
          },
        ]
      : []),
    {
      key: 'extract',
      label: '提取资产',
      done: !!catalog && (catalog.stage !== 'extract' || catalog.status === 'completed'),
    },
    { key: 'correct', label: '补漏纠错', done: workflow.catalogReady(project) },
    { key: 'visualPrompt', label: '图片提示词与核对', done: complete },
  ];
  const finished = steps.filter((s) => s.done).length;
  const run = hasIndex && !steps[0].done ? indexRun : visual || catalog;
  const active = run?.status === 'running';
  const stopped = run && ['failed', 'cancelled'].includes(run.status);
  const percent = Math.round((finished / steps.length) * 100);
  const targets = visual?.targets || workflow.targets(project);
  const saved = targets.filter((target) => {
    const p = workflow.prompt(project, target);
    return !!p && !!p.text.trim() && !p.stale && p.sourceVersion === project.source.version;
  }).length;
  return {
    flow,
    complete,
    steps,
    finished,
    percent,
    active,
    stopped,
    run,
    hasIndex,
    saved,
    targetCount: targets.length,
  };
}
