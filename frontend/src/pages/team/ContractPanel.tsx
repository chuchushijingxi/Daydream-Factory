import { CalendarClock, Sparkles, Users } from 'lucide-react';
import type { ModelOption } from '../../api/types';
import type { SpaceRole, TeamView } from '../../api/spaces-types';
import { CreditStar } from '../../components/ui/credit-star';
import { useCatalogText, type CatalogText } from '../../contexts/useCatalogText';
import { useOptionalWorkspace } from '../../state/WorkspaceContext';
import { RechargeButton } from '../../features/payments/Recharge';
import { dateOnly, daysUntil, numberText, usageTone } from './team-ui';

// 折扣百分比是应付比例：80 → 8 折，85 → 8.5 折；100 及以上即原价。
export function discountText(percent: number, c: CatalogText) {
  if (percent >= 100) return c('原价');
  return c('{value} 折', { value: String(Math.round(percent) / 10), percent: Math.round(percent) });
}

function modelCatalog(models: ReturnType<typeof useOptionalWorkspace>) {
  const rows: { option: ModelOption; kind: 'text' | 'image' | 'video' }[] = [
    ...(models?.models.analysis || []).map((option) => ({ option, kind: 'text' as const })),
    ...(models?.models.image || []).map((option) => ({ option, kind: 'image' as const })),
    ...(models?.models.video || []).map((option) => ({ option, kind: 'video' as const })),
  ];
  return new Map(rows.map((row) => [row.option.id, row]));
}

// 团队套餐是付费方最关心的内容：先给出买到了什么（名额、周期积分、有效期、模型折扣），
// 再用颜色提示名额紧张或临近到期。数据全部来自团队合同，不推算未提供的数字。
export function ContractPanel({ team, role }: { team: TeamView; role: SpaceRole }) {
  const { c } = useCatalogText();
  const catalog = modelCatalog(useOptionalWorkspace());
  if (role !== 'owner' && role !== 'admin') return null;
  const days = daysUntil(team.expiresAt),
    nextGrant = daysUntil(team.nextGrantAt),
    expired = team.status === 'expired' || (days !== null && days <= 0),
    expiring = !expired && days !== null && days <= 7,
    seatsLeft = Math.max(0, team.memberLimit - team.memberCount),
    kindText = { text: c('文本'), image: c('图片'), video: c('视频') };
  return (
    <div className="team-plan team-contract-panel" data-state={expired ? 'expired' : expiring ? 'expiring' : 'active'}>
      <header className="team-plan-head">
        <div className="team-plan-title">
          <h2>{c('团队套餐')}</h2>
          {/* 套餐状态用低调的圆点胶囊，颜色沿用积分金与全局提醒色，不用通用的绿色成功徽标。 */}
          <span
            className="team-plan-status"
            data-tone={
              team.status === 'disabled' || expired ? 'danger' : expiring ? 'warning' : 'active'
            }
          >
            {team.status === 'disabled'
              ? c('已停用')
              : expired
                ? c('已到期')
                : expiring
                  ? c('即将到期')
                  : c('生效中')}
          </span>
        </div>
        <p>{c('套餐由平台开通。续期、扩容或调整折扣请联系运营。')}</p>
        {role === 'owner' && team.status === 'active' ? (
          <RechargeButton className="team-plan-recharge" />
        ) : null}
      </header>
      <div className="team-plan-stats">
        <div className="team-plan-stat">
          <span className="team-plan-stat-label">
            <Users aria-hidden="true" />
            {c('成员名额')}
          </span>
          <strong className="team-plan-stat-value">
            {numberText(team.memberCount)}
            <small>/ {c('{count} 人', { count: numberText(team.memberLimit) })}</small>
          </strong>
          <span
            className="team-meter team-plan-meter"
            data-tone={usageTone(team.memberCount, team.memberLimit)}
            aria-hidden="true"
          >
            <span
              style={{
                width:
                  Math.min(100, (team.memberCount / Math.max(1, team.memberLimit)) * 100) + '%',
              }}
            />
          </span>
          <span className="team-plan-stat-hint">
            {seatsLeft ? c('还可加入 {count} 人', { count: seatsLeft }) : c('名额已满')}
          </span>
        </div>
        <div className="team-plan-stat" data-accent="credit">
          <span className="team-plan-stat-label">
            <CreditStar />
            {c('每 30 天赠送积分')}
          </span>
          <strong className="team-plan-stat-value">
            {team.grantAmount ? numberText(team.grantAmount) : '—'}
            {team.grantAmount ? <small>{c('积分')}</small> : null}
          </strong>
          <span className="team-plan-stat-hint">
            {!team.grantAmount
              ? c('套餐不含周期赠送')
              : nextGrant !== null && nextGrant > 0
                ? c('下次 {date} 自动到账 · {count} 天后', {
                    date: dateOnly(team.nextGrantAt),
                    count: nextGrant,
                  })
                : c('下次 {date} 自动到账', { date: dateOnly(team.nextGrantAt) })}
          </span>
        </div>
        <div className="team-plan-stat" data-warn={expired || expiring}>
          <span className="team-plan-stat-label">
            <CalendarClock aria-hidden="true" />
            {c('套餐有效期')}
          </span>
          <strong className="team-plan-stat-value">
            {expired || days === null ? (
              c('已到期')
            ) : (
              <>
                {numberText(days)}
                <small>{c('天')}</small>
              </>
            )}
          </strong>
          <span className="team-plan-stat-hint">
            {c('有效期至 {date}', { date: dateOnly(team.expiresAt) })}
          </span>
        </div>
      </div>
      <section className="team-plan-discounts" aria-label={c('模型专属折扣')}>
        <h3>
          <Sparkles aria-hidden="true" />
          {c('模型专属折扣')}
        </h3>
        {team.modelDiscounts.length ? (
          <ul>
            {team.modelDiscounts.map((item) => {
              const known = catalog.get(item.modelInstanceId);
              return (
                <li key={item.modelInstanceId}>
                  <span className="team-plan-model">
                    {known?.option.logoUrl ? (
                      <img src={known.option.logoUrl} alt="" />
                    ) : (
                      <span className="team-plan-model-mark" aria-hidden="true">
                        {Array.from(known?.option.name || '?')[0]}
                      </span>
                    )}
                    <span>
                      {/* 目录未加载时只显示模型 ID；目录已加载仍找不到才说明已下线。 */}
                      <strong title={known?.option.name || item.modelInstanceId}>
                        {known?.option.name ||
                          (catalog.size ? c('已下线的模型') : item.modelInstanceId)}
                      </strong>
                      <small>
                        {known
                          ? kindText[known.kind]
                          : catalog.size
                            ? item.modelInstanceId
                            : c('模型')}
                      </small>
                    </span>
                  </span>
                  <span className="team-plan-discount">
                    <strong>{discountText(item.percent, c)}</strong>
                    {item.percent < 100 ? (
                      <small>{c('省 {percent}%', { percent: 100 - item.percent })}</small>
                    ) : null}
                  </span>
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="team-plan-empty">{c('所有模型按原价计费')}</p>
        )}
      </section>
    </div>
  );
}
