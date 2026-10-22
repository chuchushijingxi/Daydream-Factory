import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Eye, Lock, PencilLine, type LucideIcon } from 'lucide-react';
import { teamClient } from '../../api/team-client';
import type { SpacePolicy, SpaceRole, TeamView } from '../../api/spaces-types';
import { Button } from '../../components/ui/button';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Field } from '../operations/controls';
import { MutationFeedback, useTeamMutation } from './team-ui';
import { TeamResourceTransferPanel } from './TeamResourceTransferPanel';
import { ContractPanel } from './ContractPanel';
export function SettingsPanel({
  team,
  role = 'admin',
  onSaved,
  contract = false,
}: {
  team: TeamView;
  role?: SpaceRole;
  onSaved: () => void;
  /** /team/:id/contract 深链：打开设置并定位到团队套餐。 */
  contract?: boolean;
}) {
  const { c } = useCatalogText(),
    mutation = useTeamMutation();
  const plan = useRef<HTMLElement>(null);
  useEffect(() => {
    if (contract) plan.current?.scrollIntoView?.({ block: 'start' });
  }, [contract]);
  const [saved, setSaved] = useState(team),
    [readbackDone, setReadbackDone] = useState(false);
  const [name, setName] = useState(team.name),
    [policy, setPolicy] = useState(team.policy),
    [revision, setRevision] = useState(team.revision),
    [copyError, setCopyError] = useState('');
  async function submit(e: FormEvent) {
    e.preventDefault();
    await mutation.run(
      () => teamClient.settings(team.id, { revision, name: name.trim(), policy }),
      (value) => {
        setSaved(value);
        setName(value.name);
        setPolicy(value.policy);
        setRevision(value.revision);
        onSaved();
      },
    );
  }
  async function reread() {
    await mutation.readback(
      () => teamClient.detail(team.id),
      (value) => {
        if (value.team) {
          setRevision(value.team.revision);
          setSaved(value.team);
          setReadbackDone(true);
        }
      },
    );
  }
  const changed = name.trim() !== saved.name || policy !== saved.policy;
  const policies: { value: SpacePolicy; title: string; description: string; icon: LucideIcon }[] = [
    {
      value: 'isolated',
      title: c('隔离'),
      description: c('成员只能看到自己创建的和被共享的内容'),
      icon: Lock,
    },
    {
      value: 'read_all',
      title: c('全员只读'),
      description: c('成员可以查看全部团队内容，但不能修改他人内容'),
      icon: Eye,
    },
    {
      value: 'edit_all',
      title: c('全员可编辑'),
      description: c('成员可以查看并编辑全部团队内容'),
      icon: PencilLine,
    },
  ];
  const policyText = (value: SpacePolicy) => policies.find((item) => item.value === value)?.title;
  const locked = mutation.busy || mutation.needsRead;
  return (
    <div className="team-settings-page">
      {/* 套餐放在设置首位：付费方先看到买到了什么，再做日常设置。 */}
      <section
        ref={plan}
        className="team-card team-plan-card"
        aria-label={c('团队套餐')}
        data-highlight={contract}
      >
        <ContractPanel team={team} role={role} />
      </section>
      <form className="team-card team-settings-card" onSubmit={submit}>
        <section className="team-setting-row" aria-label={c('基本信息')}>
          <header>
            <h2>{c('基本信息')}</h2>
            <p>{c('团队名称会显示在空间切换菜单和成员的邀请中。')}</p>
          </header>
          <div className="team-setting-body team-basic-settings">
            <Field label={c('团队名称')} hint={c('1–40 个字符')}>
              <input
                required
                maxLength={40}
                value={name}
                onChange={(event) => setName(event.target.value)}
                disabled={locked}
              />
            </Field>
            <div className="team-id-section">
              <span className="team-field-label">{c('团队 ID')}</span>
              <div className="team-link-box">
                <input
                  readOnly
                  aria-label={c('团队 ID')}
                  value={team.id}
                  onFocus={(event) => event.target.select()}
                />
                <Button
                  variant="outline"
                  onClick={() => {
                    setCopyError('');
                    if (!navigator.clipboard) {
                      setCopyError(c('复制失败，请手动选择链接复制。'));
                      return;
                    }
                    void navigator.clipboard
                      .writeText(team.id)
                      .catch(() => setCopyError(c('复制失败，请手动选择链接复制。')));
                  }}
                >
                  {c('复制 ID')}
                </Button>
              </div>
              <small className="op-muted">{c('向需要申请加入的用户提供此 ID。')}</small>
              {copyError && (
                <p role="alert" className="op-inline-error">
                  {copyError}
                </p>
              )}
            </div>
          </div>
        </section>
        <section className="team-setting-row" aria-label={c('成员之间的可见性')}>
          <header>
            <h2>{c('成员之间的可见性')}</h2>
            <p>{c('档位立即生效。单资源共享只能放宽权限，不能收紧。')}</p>
          </header>
          <div className="team-setting-body">
            <div className="team-policy-options" role="radiogroup" aria-label={c('成员之间的可见性')}>
              {policies.map((item) => (
                <label key={item.value} className="team-policy-option">
                  <input
                    type="radio"
                    name={'team-policy-' + team.id}
                    value={item.value}
                    checked={policy === item.value}
                    disabled={locked}
                    onChange={() => setPolicy(item.value)}
                  />
                  <item.icon className="team-policy-icon" aria-hidden="true" />
                  <span>
                    <strong>{item.title}</strong>
                    <small>{item.description}</small>
                  </span>
                </label>
              ))}
            </div>
          </div>
        </section>
        <footer className="team-settings-footer">
          <MutationFeedback mutation={mutation} />
          {readbackDone && (
            <dl className="team-contract" aria-label={c('读取最新状态')}>
              <div>
                <dt>{c('团队名称')}</dt>
                <dd>{saved.name}</dd>
              </div>
              <div>
                <dt>{c('成员之间的可见性')}</dt>
                <dd>{policyText(saved.policy)}</dd>
              </div>
            </dl>
          )}
          <div className="op-row-actions">
            {mutation.needsRead && (
              <Button
                variant="outline"
                disabled={mutation.busy}
                onClick={() => {
                  void reread();
                }}
              >
                {c('读取最新状态')}
              </Button>
            )}
            <Button
              type="submit"
              busy={mutation.busy}
              disabled={!changed || !name.trim() || mutation.needsRead}
            >
              {c('保存设置')}
            </Button>
          </div>
        </footer>
      </form>
      <section className="team-card team-settings-card" aria-label={c('资源交接')}>
        <div className="team-setting-row">
          <header>
            <h2>{c('资源交接')}</h2>
            <p>{c('成员离队后，把其全部短剧项目、上传文件和创作资产移交给现有成员。')}</p>
          </header>
          <div className="team-setting-body">
            <TeamResourceTransferPanel id={team.id} onSaved={onSaved} />
          </div>
        </div>
      </section>
    </div>
  );
}
