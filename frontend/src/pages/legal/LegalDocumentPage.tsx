import { useContext, useLayoutEffect } from 'react';
import { paymentApi } from '../../api/payment-client';
import type { PaymentAPI } from '../../api/payment-types';
import { sessionBoundary } from '../../api/session-boundary';
import { SessionOptionalContext } from '../../state/SessionContext';
import { useCatalogText } from '../../contexts/useCatalogText';
import { useRead } from '../operations/controls';

export type LegalDocument = 'terms' | 'privacy' | 'recharge';

export const legalPaths: Record<LegalDocument, string> = {
  terms: '/legal/terms',
  privacy: '/legal/privacy',
  recharge: '/legal/recharge',
};
// 旧的 /payments/agreement 链接（已发出的订单页、书签）仍落到同一份独立充值协议。
export function legalDocumentAt(pathname: string): LegalDocument | null {
  if (pathname === '/payments/agreement') return 'recharge';
  const match = (Object.keys(legalPaths) as LegalDocument[]).find(
    (kind) => legalPaths[kind] === pathname,
  );
  return match || null;
}

// 正文为开源模板的示例条款，部署方应替换为运营与法务确认的版本。
const documents: Record<LegalDocument, { title: string; sections: [string, string][] }> = {
  terms: {
    title: '用户服务协议',
    sections: [
      [
        '协议范围',
        '本协议适用于你注册和使用本平台提供的短剧创作服务。开始使用即表示你已阅读并同意本协议。',
      ],
      [
        '账号与安全',
        '请使用真实、合法的信息注册，并妥善保管账号和密码。因账号保管不当造成的损失由你自行承担。',
      ],
      [
        '内容规范',
        '你上传的剧本、素材以及生成的内容须遵守法律法规，不得侵犯他人的知识产权、肖像权或其他合法权益。',
      ],
      [
        '生成内容的使用',
        '你对自己上传的原始内容保留权利。在遵守本协议及相关模型服务条款的前提下，你可以使用平台为你生成的图片、视频与文本。',
      ],
      [
        '服务变更与终止',
        '平台可能根据运营需要调整或暂停部分服务，并尽量提前通知。你违反本协议的，平台可以限制或终止向你提供服务。',
      ],
      [
        '免责与联系',
        '生成结果由模型自动产生，可能存在不准确之处，请在发布前自行核对。如有疑问请联系客服。',
      ],
    ],
  },
  privacy: {
    title: '隐私政策',
    sections: [
      [
        '我们收集的信息',
        '注册时提供的账号与邮箱；使用服务时上传的剧本、素材及生成记录；用于保障服务安全的登录日志与设备信息。',
      ],
      [
        '信息的使用',
        '这些信息仅用于提供和改进创作服务、计算积分消耗、保障账号安全以及处理你的反馈。',
      ],
      [
        '第三方模型服务',
        '生成图片、视频或文本时，相关提示词与素材会发送给对应的模型服务商处理，仅用于完成本次生成。',
      ],
      [
        '存储与保护',
        '我们采取合理的技术措施保护你的信息，并只在实现上述目的所需的期限内保存。',
      ],
      [
        '你的权利',
        '你可以查看和修改个人资料，也可以联系客服申请导出或删除账号数据。',
      ],
      ['政策更新', '本政策更新后会在本页公布新版本，重大变更会另行通知。'],
    ],
  },
  recharge: {
    title: '积分充值协议',
    sections: [
      [
        '积分性质与使用',
        '积分是本平台内的虚拟服务额度，仅用于平台提供的服务，不属于存款或货币，不可提现、转让或兑现。充值积分与充值赠送积分不过期。',
      ],
      [
        '充值金额与到账',
        '充值采用固定档位，页面明确展示实际支付金额、充值积分和赠送积分。请在付款前确认目标空间、档位与渠道，已下订单按下单时展示的金额和积分执行。',
      ],
      [
        '团队空间归属',
        '充入团队空间的积分归该团队使用，不能转回个人空间。请确认目标空间后再付款。',
      ],
      [
        '退款规则',
        '充值原则上不退款。确需退款请联系客服，由运营按未使用的充值积分审核，按原支付渠道退回；已使用部分不可退，赠送积分按规则同比例扣回。系统不提供用户自助退款。',
      ],
      [
        '发票与未成年人',
        '需要发票请联系客服并提供订单号，发票在线下开具，系统不保存开票信息。未成年人充值须事先取得监护人同意。',
      ],
      [
        '协议同意与版本',
        '首次充值须勾选同意当前版本协议，系统记录同意版本与时间；协议更新后须重新同意。每笔订单保存其使用的协议版本。',
      ],
    ],
  },
};

// 独立文档页：在新标签页打开，不挂工作台外壳、侧栏、顶栏或任何平台入口，未登录也可阅读。
export function LegalDocumentPage({
  document: kind,
  api = paymentApi,
}: {
  document: LegalDocument;
  api?: Pick<PaymentAPI, 'options'>;
}) {
  const { c } = useCatalogText();
  const session = useContext(SessionOptionalContext);
  const doc = documents[kind];
  const title = c(doc.title);
  // 布局副作用先于外层品牌标题的普通副作用执行：先标记本页自带标题，品牌读取完成后也不会覆盖。
  useLayoutEffect(() => {
    const root = window.document.documentElement,
      previous = window.document.title;
    root.dataset.pageTitle = 'legal';
    window.document.title = title;
    return () => {
      delete root.dataset.pageTitle;
      window.document.title = previous;
    };
  }, [title]);
  // 充值协议版本只在已登录时读取；读不到不影响正文阅读。
  const version = useRead(
    (signal) => api.options(signal),
    [api, session?.service, session?.user?.identity, sessionBoundary.epoch],
    kind === 'recharge' && !!session?.user,
  );
  return (
    <main className="legal-page">
      <article className="legal-document" aria-labelledby="legal-document-title">
        <h1 id="legal-document-title">{title}</h1>
        <p className="legal-document-note">
          {c('本页为研发占位稿，正式正文须由运营方与法务提供。')}
        </p>
        {kind === 'recharge' && version.value ? (
          <p className="legal-document-version">
            {c('当前协议版本：{version}', { version: version.value.agreement.version })}
          </p>
        ) : null}
        {doc.sections.map(([heading, body]) => (
          <section key={heading}>
            <h2>{c(heading)}</h2>
            <p>{c(body)}</p>
          </section>
        ))}
      </article>
    </main>
  );
}
