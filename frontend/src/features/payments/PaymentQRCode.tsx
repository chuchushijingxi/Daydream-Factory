import { useMemo } from 'react';
import qrcode from 'qrcode-generator';
import { useCatalogText } from '../../contexts/useCatalogText';

// Byte模式默认截断非ASCII字符；明确使用UTF-8，保持渠道返回的链接逐字节一致。
qrcode.stringToBytes = (value) => Array.from(new TextEncoder().encode(value));

export function qrPath(value: string) {
  if (!value || value.length > 4096) throw new Error('QR_CONTENT_INVALID');
  const code = qrcode(0, 'M');
  code.addData(value, 'Byte');
  code.make();
  const count = code.getModuleCount();
  let path = '';
  for (let row = 0; row < count; row++)
    for (let col = 0; col < count; col++) {
      if (code.isDark(row, col)) path += `M${col + 4} ${row + 4}h1v1h-1z`;
    }
  return { size: count + 8, path };
}
export function PaymentQRCode({ value }: { value: string }) {
  const { c } = useCatalogText();
  const result = useMemo(() => {
    try {
      return qrPath(value);
    } catch {
      return null;
    }
  }, [value]);
  if (!result)
    return (
      <p className="payment-error" role="alert">
        {c('二维码生成失败，请关闭此订单后重新下单。')}
      </p>
    );
  return (
    <svg
      className="payment-qr"
      viewBox={`0 0 ${result.size} ${result.size}`}
      role="img"
      aria-label={c('支付二维码')}
      shapeRendering="crispEdges"
    >
      <rect width={result.size} height={result.size} fill="#fff" />
      <path d={result.path} fill="#000" />
    </svg>
  );
}
