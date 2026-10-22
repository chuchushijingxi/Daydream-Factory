import { useState } from 'react';
import { teamCreditsApi } from '../../api/team-credits';
import { useCreditRead } from '../../features/credits/useCreditRead';
import { useTeamCredits } from '../../features/credits/PersonalCredits';
import { useCatalogText } from '../../contexts/useCatalogText';
import { Button } from '../../components/ui/button';
import { Choice, Feedback } from '../operations/controls';
import { dateTime, formatInteger } from '../credits/credit-format';

// The team workspace consumes the existing ledger contract; /credits keeps its own page.
export function TeamCreditRecords({ consumed = false }: { consumed?: boolean }) {
  const { c } = useCatalogText();
  const credits = useTeamCredits();
  const [page, setPage] = useState(1),
    [direction, setDirection] = useState('all'),
    [range, setRange] = useState('all');
  const read = useCreditRead(
    () => teamCreditsApi.ledger(false, page, consumed ? 'consumed' : direction, range),
    [page, direction, range, consumed, credits?.revision],
  );
  const names = { add: c('新增'), deduct: c('扣减'), refund: c('返还'), expire: c('到期回收') };
  return (
    <section
      className="team-card team-credit-records"
      aria-label={consumed ? c('消耗记录') : c('积分明细')}
    >
      <div className="team-card-head">
        <h2>{consumed ? c('消耗记录') : c('积分明细')}</h2>
        <div className="team-record-toolbar">
        {!consumed && (
          <Choice
            label={c('记录类型')}
            value={direction}
            options={[
              { value: 'all', label: c('全部') },
              { value: 'acquired', label: c('已获取') },
              { value: 'consumed', label: c('已消耗') },
            ]}
            onChange={(value) => {
              setDirection(value);
              setPage(1);
            }}
          />
        )}
        <Choice
          label={c('积分记录时间')}
          value={range}
          options={[
            { value: 'all', label: c('全部时间') },
            { value: '7', label: c('最近 7 天') },
            { value: '30', label: c('最近 30 天') },
          ]}
          onChange={(value) => {
            setRange(value);
            setPage(1);
          }}
        />
        </div>
      </div>
      <Feedback busy={read.busy} error={read.error} retry={read.refresh} />
      {read.value && (
        <>
          {read.value.items.length ? (
            <div
              className="op-table-scroll"
              tabIndex={0}
              aria-label={consumed ? c('消耗记录') : c('积分明细')}
            >
              <table className="op-table">
                <thead>
                  <tr>
                    <th>{c('时间')}</th>
                    <th>{c('类型')}</th>
                    <th>{c('积分')}</th>
                    <th>{c('变更后余额')}</th>
                    <th>{c('原因')}</th>
                  </tr>
                </thead>
                <tbody>
                  {read.value.items.map((item) => (
                    <tr key={item.id}>
                      <td>{dateTime(item.createdAt)}</td>
                      <td>{names[item.kind]}</td>
                      <td>
                        {['deduct', 'expire'].includes(item.kind) ? '-' : '+'}
                        {formatInteger(item.amount)}
                      </td>
                      <td>{formatInteger(item.balance!)}</td>
                      <td>{item.reason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="op-muted">{c('暂无积分记录')}</p>
          )}
          <div className="team-record-pagination">
            <Button
              variant="outline"
              disabled={read.busy || page <= 1}
              onClick={() => setPage((old) => old - 1)}
            >
              {c('上一页')}
            </Button>
            <span className="op-muted">{c('第 {page} 页', { page })}</span>
            <Button
              variant="outline"
              disabled={read.busy || page * read.value.pageSize >= read.value.total}
              onClick={() => setPage((old) => old + 1)}
            >
              {c('下一页')}
            </Button>
          </div>
        </>
      )}
    </section>
  );
}
