import { ApiError, http, jsonPost, type createHttpClient } from './client';
import { responseFields } from './response-fields';
import { translate } from '../contexts/translate';

export type PlatformSettings = {
  revision: number;
  name: string;
  sidebarDescription: string;
  logoId: string;
  logoUrl: string;
};
export type PlatformSettingsInput = Omit<PlatformSettings, 'logoUrl'>;
export type PlatformLogo = Pick<PlatformSettings, 'logoId' | 'logoUrl'>;
const hash = /^[a-f0-9]{64}$/;
const textValid = (value: unknown, min: number, max: number): value is string =>
  typeof value === 'string' && value === value.trim() &&
  [...value].length >= min && [...value].length <= max && !/[\u0000-\u001f\u007f-\u009f]/u.test(value);

function logo(value: PlatformLogo, allowDefault = true): PlatformLogo {
  value = responseFields(value, ['logoId', 'logoUrl']);
  if (!(allowDefault && value.logoId === '' && value.logoUrl === '') &&
    !(typeof value.logoId === 'string' && hash.test(value.logoId) &&
      value.logoUrl === '/api/platform-logos/' + value.logoId))
    throw new ApiError(200, 'PROGRAM_FAILED', translate('Logo 回执不完整，请重新读取。'));
  return value;
}
function settings(value: PlatformSettings): PlatformSettings {
  value = responseFields(value, ['revision', 'name', 'sidebarDescription', 'logoId', 'logoUrl']);
  if (!Number.isSafeInteger(value.revision) || value.revision < 0 ||
    !textValid(value.name, 1, 48) || !textValid(value.sidebarDescription, 0, 120))
    throw new ApiError(200, 'PROGRAM_FAILED', translate('平台设置回执不完整，请重新读取。'));
  logo(value);
  return value;
}
export function createPlatformApi(transport: ReturnType<typeof createHttpClient>) {
  return {
    async publicSettings(signal?: AbortSignal) {
      return settings(await transport.json<PlatformSettings>('/api/platform-settings', {
        authentication: 'public', signal,
      }));
    },
    async settings() {
      return settings(await transport.json<PlatformSettings>('/api/operations/platform-settings'));
    },
    async save(input: PlatformSettingsInput) {
      const command = {
        revision: input.revision, name: input.name.trim(),
        sidebarDescription: input.sidebarDescription.trim(), logoId: input.logoId,
      };
      if (!Number.isSafeInteger(command.revision) || command.revision < 0 ||
        command.revision >= Number.MAX_SAFE_INTEGER || !textValid(command.name, 1, 48) ||
        !textValid(command.sidebarDescription, 0, 120) ||
        !(command.logoId === '' || hash.test(command.logoId)))
        throw new ApiError(
          400,
          'PLATFORM_SETTINGS_INVALID',
          translate('请核对平台名称、侧栏描述和 Logo。'),
        );
      const saved = settings(await transport.json<PlatformSettings>(
        '/api/operations/platform-settings', jsonPost(command),
      ));
      if (BigInt(saved.revision) !== BigInt(command.revision) + 1n ||
        saved.name !== command.name || saved.sidebarDescription !== command.sidebarDescription ||
        saved.logoId !== command.logoId)
        throw new ApiError(
          200,
          'PROGRAM_FAILED',
          translate('保存回执不一致，请先读取最新配置核对。'),
        );
      return saved;
    },
    async upload(file: File) {
      if (!['image/png', 'image/jpeg'].includes(file.type) || file.size > 2 * 1024 * 1024 || !file.size)
        throw new ApiError(
          400,
          'INVALID_PLATFORM_LOGO',
          translate('请选择 2MB 以内的 PNG 或 JPEG 图片。'),
        );
      return logo(await transport.json<PlatformLogo>('/api/operations/platform-logos', {
        method: 'POST', headers: { 'Content-Type': file.type }, body: file,
      }), false);
    },
  };
}
export const platformApi = createPlatformApi(http);
export type PlatformApi = ReturnType<typeof createPlatformApi>;
