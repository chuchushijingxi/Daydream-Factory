import { responseFields } from './response-fields';
import { ApiError, jsonPost, type createHttpClient } from './client';
import type { SmtpSettingsDTO, SmtpUpdateDTO } from './administration-types';
import { translate } from '../contexts/translate';

function settings(value: SmtpSettingsDTO): SmtpSettingsDTO {
  value = responseFields(value, [
    'revision', 'enabled', 'address', 'username', 'from', 'tls', 'passwordConfigured',
  ]);
  if (
    !Number.isSafeInteger(value.revision) ||
    value.revision < 0 ||
    typeof value.enabled !== 'boolean' ||
    typeof value.passwordConfigured !== 'boolean' ||
    !['address', 'username', 'from'].every(
      (key) => typeof value[key as keyof SmtpSettingsDTO] === 'string',
    ) ||
    !['starttls', 'tls'].includes(value.tls)
  )
    throw new ApiError(
      200,
      'PROGRAM_FAILED',
      translate('邮件配置回执不完整，请重新读取。'),
    );
  return value;
}
export function createSmtpApi(transport: ReturnType<typeof createHttpClient>) {
  return {
    async smtp() {
      return settings(await transport.json<SmtpSettingsDTO>('/api/operations/smtp'));
    },
    async saveSmtp(input: SmtpUpdateDTO) {
      if (!Number.isSafeInteger(input.revision) || input.revision < 0)
        throw new ApiError(
          400,
          'MAIL_CONFIGURATION_INVALID',
          translate('请先读取最新配置后重试。'),
        );
      const saved = settings(
        await transport.json<SmtpSettingsDTO>(
          '/api/operations/smtp',
          jsonPost({
            revision: input.revision,
            enabled: input.enabled,
            address: input.address,
            username: input.username,
            from: input.from,
            tls: input.tls,
            ...(input.password ? { password: input.password } : {}),
            clearPassword: input.clearPassword === true,
          }),
        ),
      );
      if (
        !Number.isSafeInteger(input.revision) ||
        input.revision < 0 ||
        BigInt(saved.revision) !== BigInt(input.revision) + 1n ||
        saved.enabled !== input.enabled ||
        (input.clearPassword && saved.passwordConfigured) ||
        (input.password && !saved.passwordConfigured)
      )
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('邮件配置保存回执不一致，尚未确认结果，请先回读。'),
        );
      return saved;
    },
  };
}
