// 版本被拒或写入结果不明时，先回读再允许下一次提交。
// 这里绝不自动重试变更，也不把网络错误当作已取消的写入。
export function needsProjectReadback(error: unknown): boolean {
  const e = error as {
    status?: number;
    code?: string;
  };
  if (e.code === 'BILLING_BASIS_CHANGED') return false;
  return (
    e.status === 0 ||
    e.status === 404 ||
    e.status === 409 ||
    (e.status !== undefined && e.status >= 500) ||
    e.code === 'PROGRAM_FAILED'
  );
}
export function writeResultUnknown(error: unknown): boolean {
  const e = error as {
    status?: number;
    code?: string;
  };
  return (
    e.status === 0 || (e.status !== undefined && e.status >= 500) || e.code === 'PROGRAM_FAILED'
  );
}
export function projectRequestMessage(error: unknown, translate: (text: string) => string): string {
  const e = error as {
    code?: string;
    message?: string;
  };
  if (e.code === 'BILLING_BASIS_CHANGED')
    return translate('计费范围或价格已变化，请核对更新后的报价再启动。');
  if (e.code === 'CONFLICT') return translate('项目已更新，请读取最新版本后核对。');
  return e.message || translate('请求失败，请稍后重试。');
}

export function authenticationMessage(error: unknown): string {
  const e = error as { code?: string; message?: string };
  const messages: Record<string, string> = {
    MAIL_UNAVAILABLE: '邮件服务暂不可用，暂时无法发送验证码，请稍后重试或联系运营。',
    MAIL_DELIVERY_FAILED: '验证码邮件提交失败，请稍后重试。',
    AUTH_RATE_LIMITED: '操作过于频繁，请稍后重试。',
    INVALID_CREDENTIALS: '账号、密码或验证码不正确。',
    ROLE_FORBIDDEN: '当前账号没有运营权限。',
    INVALID_USERNAME: '用户名须为1至40个字符，不能包含控制字符。',
    CURRENT_PASSWORD_INVALID: '原密码不正确。',
    INVALID_PASSWORD: '密码须为12至72字节。',
    INVALID_AVATAR: '请选择5MB以内的JPEG、PNG或WebP图片。',
    INVALID_IMAGE: '图片无法读取，请选择有效的JPEG、PNG或WebP图片。',
  };
  return (e.code && messages[e.code]) || e.message || '请求失败，请稍后重试。';
}
