import { useEffect, useState } from 'react';
import { ArrowRight } from 'lucide-react';
import { teamClient } from '../../api/team-client';
import { useTeamRevision } from './useTeamUpdates';
import type { Overview, SpaceRole, TeamView } from '../../api/spaces-types';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { Choice, Feedback, useRead } from '../operations/controls';
import { actionText } from './ActivityPanel';
import { BalanceTiles } from './CreditsPanel';
import { dateOnly, daysUntil, InitialAvatar, numberText, usageTone } from './team-ui';
export { AuditTable } from './ActivityPanel';

export type OverviewRange = Overview['range'];
export type OverviewTarget = 'members' | 'requests' | 'activity' | 'plan';

const dayFormat = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
});
// 服务端只返回有消耗的日期；图表按统计区间补齐空日，柱宽才代表真实时间跨度。
export function calendarDays(start: string, end: string, daily: Overview['daily']) {
  const values = new Map(daily.map((day) => [day.date, day.credits]));
  const days: { date: string; credits: number }[] = [];
  const last = dayFormat.format(new Date(end));
  for (let at = new Date(start).getTime(); days.length < 1000; at += 86400000) {
    const date = dayFormat.format(new Date(at));
    if (days[days.length - 1]?.date !== date) days.push({ date, credits: values.get(date) || 0 });
    if (date >= last) break;
  }
  return days;
}

// “开通以来”对应服务端 range=contract，即团队开通至今。
export function rangeLabel(range: OverviewRange, c: CatalogText) {
  return range === '7d' ? c('近 7 天') : range === '30d' ? c('近 30 天') : c('开通以来');
}

export function OverviewPanel({
  id,
  team,
  role,
  pendingRequests,
  onRequestCount,
  onOpen,
}: {
  id: string;
  team: TeamView;
  role: SpaceRole;
  pendingRequests: number | null;
  onRequestCount: (count: number) => void;
  onOpen: (target: OverviewTarget) => void;
}) {
  const { c } = useCatalogText();
  const [range, setRange] = useState<OverviewRange>('30d');
  const read = useRead((signal) => teamClient.overview(id, range, signal), [id, range]);
  const requests = useRead((signal) => teamClient.requests(id, signal), [id]);
  useTeamRevision(() => {
    read.reload();
    requests.reload();
  });
  useEffect(() => {
    if (requests.value) onRequestCount(requests.value.length);
  }, [requests.value]);
  const value = read.value;
  const days = value ? calendarDays(value.start, value.end, value.daily) : [];
  const total = days.reduce((sum, day) => sum + day.credits, 0);
  const peak = Math.max(1, ...days.map((day) => day.credits));
  const members = [...(value?.members || [])].sort((a, b) => b.credits - a.credits);
  const topUsage = Math.max(1, ...members.map((member) => member.credits));
  const names = Object.fromEntries(members.map((member) => [member.userId, member.name]));
  const remaining = daysUntil(team.expiresAt);
  const pending = pendingRequests ?? requests.value?.length ?? 0;
  return (
    <div className="team-dashboard">
      <BalanceTiles id={id} role={role}>
        <div className="team-tile">
          <span className="team-tile-label">
            {c('{range}消耗', { range: rangeLabel(range, c) })}
          </span>
          <strong className="team-tile-value">{value ? numberText(total) : '—'}</strong>
          <span className="team-tile-hint">
            {value && total > 0
              ? c('日均 {count}', { count: numberText(Math.round(total / days.length)) })
              : c('暂无消耗')}
          </span>
        </div>
      </BalanceTiles>
      <div className="team-dashboard-grid">
        <div className="team-dashboard-main">
          <section className="team-card team-trend-card" aria-label={c('积分消耗趋势')}>
            <div className="team-card-head">
              <div>
                <h2>{c('积分消耗趋势')}</h2>
                {value && (
                  <p className="team-card-sub">
                    {dateOnly(value.start)} — {dateOnly(value.end)}
                  </p>
                )}
              </div>
              <Choice
                label={c('统计范围')}
                value={range}
                options={[
                  { value: '7d', label: c('近 7 天') },
                  { value: '30d', label: c('近 30 天') },
                  { value: 'contract', label: c('开通以来') },
                ]}
                onChange={(next) => setRange(next as OverviewRange)}
              />
            </div>
            <Feedback busy={read.busy && !value} error={read.error} retry={read.refresh} />
            {value && (
              <div className="team-trend-chart">
                <div className="team-trend-scale" aria-hidden="true">
                  <span>{numberText(total === 0 ? 0 : peak)}</span>
                  <span>0</span>
                </div>
                <div className="team-trend-plot">
                  <div
                    className="team-trend"
                    role="img"
                    aria-label={c('{range}共消耗 {count} 积分', {
                      range: rangeLabel(range, c),
                      count: numberText(total),
                    })}
                    data-empty={total === 0}
                  >
                    {days.map((day) => (
                      <div
                        className="team-trend-column"
                        key={day.date}
                        title={day.date + ' · ' + numberText(day.credits)}
                      >
                        <div
                          className="team-trend-bar"
                          style={{ height: (day.credits / peak) * 100 + '%' }}
                        />
                      </div>
                    ))}
                    {total === 0 && <p className="team-trend-empty">{c('这段时间还没有消耗')}</p>}
                  </div>
                  <div className="team-trend-axis" aria-hidden="true">
                    <span>{days[0]?.date.slice(5)}</span>
                    <span>{days[days.length - 1]?.date.slice(5)}</span>
                  </div>
                </div>
              </div>
            )}
          </section>
          <section className="team-card" aria-label={c('成员用量')}>
            <div className="team-card-head">
              <div>
                <h2>{c('成员用量')}</h2>
                <p className="team-card-sub">{rangeLabel(range, c)}</p>
              </div>
              <Button variant="outline" size="sm" onClick={() => onOpen('members')}>
                {c('管理成员')}
              </Button>
            </div>
            {value && (
              <div className="op-table-scroll" tabIndex={0} aria-label={c('成员用量')}>
                <table className="op-table team-usage-table">
                  <thead>
                    <tr>
                      <th>{c('成员')}</th>
                      <th>{c('消耗积分')}</th>
                      <th>{c('图片')}</th>
                      <th>{c('视频')}</th>
                      <th>{c('项目')}</th>
                      <th>{c('名下可用积分')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {members.map((member) => (
                      <tr key={member.userId}>
                        <td>
                          <span className="team-person">
                            <InitialAvatar name={member.name} />
                            <span>{member.name}</span>
                          </span>
                        </td>
                        <td>
                          <span className="team-usage">
                            <span className="team-usage-value">{numberText(member.credits)}</span>
                            <span className="team-usage-track" aria-hidden="true">
                              <span style={{ width: (member.credits / topUsage) * 100 + '%' }} />
                            </span>
                          </span>
                        </td>
                        <td>{numberText(member.images)}</td>
                        <td>{numberText(member.videos)}</td>
                        <td>{numberText(member.projects)}</td>
                        <td>{numberText(member.holderBalance)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </div>
        <aside className="team-dashboard-side">
          <section className="team-card team-side-card" aria-label={c('团队套餐')}>
            <div className="team-card-head">
              <h2>{c('团队套餐')}</h2>
              <Button variant="ghost" size="sm" onClick={() => onOpen('plan')}>
                {c('详情')}
                <ArrowRight aria-hidden="true" />
              </Button>
            </div>
            <dl className="team-facts">
              <div>
                <dt>{c('有效期至')}</dt>
                <dd>
                  {dateOnly(team.expiresAt)}
                  {remaining !== null && remaining > 0 && (
                    <small data-warn={remaining <= 7}>
                      {c('剩余 {count} 天', { count: remaining })}
                    </small>
                  )}
                </dd>
              </div>
              <div>
                <dt>{c('每 30 天发放')}</dt>
                <dd>{c('{count} 积分', { count: numberText(team.grantAmount) })}</dd>
              </div>
              <div>
                <dt>{c('下次发放')}</dt>
                <dd>{dateOnly(team.nextGrantAt)}</dd>
              </div>
              <div>
                <dt>{c('成员名额')}</dt>
                <dd>
                  {c('{count} / {limit} 人', {
                    count: team.memberCount,
                    limit: team.memberLimit,
                  })}
                  <span
                    className="team-meter"
                    data-tone={usageTone(team.memberCount, team.memberLimit)}
                    aria-hidden="true"
                  >
                    <span
                      style={{
                        width:
                          Math.min(100, (team.memberCount / Math.max(1, team.memberLimit)) * 100) +
                          '%',
                      }}
                    />
                  </span>
                </dd>
              </div>
              <div>
                <dt>{c('模型折扣')}</dt>
                <dd>
                  {team.modelDiscounts.length
                    ? c('{count} 个模型享折扣', { count: team.modelDiscounts.length })
                    : c('所有模型原价')}
                </dd>
              </div>
            </dl>
          </section>
          <section className="team-card team-side-card" aria-label={c('待处理')}>
            <div className="team-card-head">
              <h2>{c('待处理')}</h2>
            </div>
            <button
              type="button"
              className="team-todo"
              data-active={pending > 0}
              onClick={() => onOpen('requests')}
            >
              <span>
                <strong>{c('加入申请')}</strong>
                <small>
                  {pending > 0
                    ? c('{count} 人等待审批', { count: pending })
                    : c('暂无新的加入申请')}
                </small>
              </span>
              <span className="team-todo-count">{pending}</span>
            </button>
          </section>
          <section className="team-card team-side-card" aria-label={c('最近动态')}>
            <div className="team-card-head">
              <h2>{c('最近动态')}</h2>
              <Button variant="ghost" size="sm" onClick={() => onOpen('activity')}>
                {c('全部')}
                <ArrowRight aria-hidden="true" />
              </Button>
            </div>
            {value &&
              (value.recent.length ? (
                <ol className="team-recent">
                  {value.recent.slice(0, 5).map((item) => (
                    <li key={item.id}>
                      <span className="team-recent-dot" aria-hidden="true" />
                      <span className="team-recent-copy">
                        <span>
                          <strong>
                            {item.actorKind === 'operator'
                              ? c('运营人员')
                              : names[item.actorId] || item.actorId}
                          </strong>{' '}
                          {actionText(item.action, c)}
                          {item.target && names[item.target] ? ' · ' + names[item.target] : ''}
                        </span>
                        <time dateTime={item.createdAt}>{dateOnly(item.createdAt)}</time>
                      </span>
                    </li>
                  ))}
                </ol>
              ) : (
                <p className="op-muted">{c('暂无动态')}</p>
              ))}
          </section>
        </aside>
      </div>
    </div>
  );
}
