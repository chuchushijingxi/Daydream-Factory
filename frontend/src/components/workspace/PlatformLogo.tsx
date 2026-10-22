import type { PlatformSettings } from '../../api/platform-client';
import { usePlatform } from '../../state/PlatformContext';

// 未上传自定义标识时显示模板自带的 Daydream Factory 标识；运营上传后以上传的为准。
export const defaultPlatformLogo = '/demo/app/images/brand-logo.png';

export function PlatformLogo({ className = '', settings: supplied }: {
  className?: string;
  settings?: Pick<PlatformSettings, 'logoUrl'> | null;
}) {
  const platform = usePlatform();
  const settings = supplied === undefined ? platform.settings : supplied;
  return (
    <span className={'workspace-brand-mark ' + className} data-custom={!!settings} aria-hidden>
      {settings ? <img src={settings.logoUrl || defaultPlatformLogo} alt="" /> : null}
    </span>
  );
}
