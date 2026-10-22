import { useLocation, useNavigate } from 'react-router-dom';
import { paymentApi, type PaymentAPI } from '../../api/payment-client';
import { ViewTabs } from '../../components/ui/view-tabs';
import { useCatalogText } from '../../contexts/useCatalogText';
import { OperationsPaymentOrdersPage } from './OperationsPaymentOrdersPage';
import { OperationsPaymentAnomaliesPage } from './OperationsPaymentAnomaliesPage';

export function OperationsPaymentsPage({ api = paymentApi }: { api?: PaymentAPI }) {
  const { c } = useCatalogText();
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const tab = pathname === '/operations/payments/anomalies' ? 'anomalies' : 'orders';
  return (
    <section className="op-page op-payment-page">
      <header className="op-page-heading">
        <div>
          <h1>{c('支付管理')}</h1>
          <p>
            {tab === 'orders'
              ? c('按渠道事实核查到账和退款，每项操作保留原因与操作人。')
              : c('异常仅支持重新查单与标记已核查，不提供手工加积分。')}
          </p>
        </div>
      </header>
      <ViewTabs
        items={[
          { value: 'orders', label: c('支付订单') },
          { value: 'anomalies', label: c('支付异常') },
        ]}
        value={tab}
        onChange={(value) => {
          if (value !== tab) void navigate('/operations/payments/' + value);
        }}
        label={c('支付管理')}
        prefix="operations-payment-tab-"
        panelId="operations-payment-panel"
      />
      <div
        role="tabpanel"
        id="operations-payment-panel"
        aria-labelledby={'operations-payment-tab-' + tab}
      >
        {tab === 'orders' ? (
          <OperationsPaymentOrdersPage api={api} embedded />
        ) : (
          <OperationsPaymentAnomaliesPage api={api} embedded />
        )}
      </div>
    </section>
  );
}
