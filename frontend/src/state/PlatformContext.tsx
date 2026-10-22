import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { platformApi, type PlatformApi, type PlatformSettings } from '../api/platform-client';
import { useSession } from './SessionContext';
import { Button } from '../components/ui/button';
import { useCatalogText } from '../contexts/useCatalogText';

const Context = createContext<{
  settings: PlatformSettings | null;
  api: PlatformApi;
  adopt(value: PlatformSettings): void;
}>({ settings: null, api: platformApi, adopt() {} });
export const usePlatform = () => useContext(Context);

export function PlatformProvider({ children, api = platformApi }: {
  children: ReactNode;
  api?: PlatformApi;
}) {
  const { c } = useCatalogText();
  const { epoch } = useSession();
  const [settings, setSettings] = useState<PlatformSettings | null>(null);
  const [failed, setFailed] = useState(false), [retry, setRetry] = useState(0);
  const adopt = useCallback((value: PlatformSettings) => {
    // 焦点回读可能早于保存发出，旧版本不能覆盖已确认的新品牌。
    setSettings((old) => !old || value.revision >= old.revision ? value : old);
    setFailed(false);
  }, []);
  useEffect(() => {
    let active = true;
    let pending: AbortController | null = null;
    const read = async () => {
      if (pending || document.visibilityState === 'hidden') return;
      const controller = new AbortController();
      pending = controller;
      try {
        const value = await api.publicSettings(controller.signal);
        if (active) { adopt(value); setFailed(false); }
      } catch {
        // 品牌读取失败不能撤销登录，也不能以默认值冒充实际配置。
        if (active && !controller.signal.aborted) setFailed(true);
      } finally {
        if (pending === controller) pending = null;
      }
    };
    const visible = () => { if (document.visibilityState !== 'hidden') void read(); };
    void read();
    window.addEventListener('focus', visible);
    document.addEventListener('visibilitychange', visible);
    return () => {
      active = false;
      pending?.abort();
      window.removeEventListener('focus', visible);
      document.removeEventListener('visibilitychange', visible);
    };
  }, [api, adopt, epoch, retry]);
  useEffect(() => {
    // 独立文档页（协议、政策）自带标签页标题，品牌名不覆盖它。
    if (settings && !document.documentElement.dataset.pageTitle) document.title = settings.name;
  }, [settings]);
  return <Context.Provider value={{ settings, api, adopt }}>
    {children}
    {failed ? <div className="platform-read-error" role="alert">
      <span>{c('平台信息暂不可用')}</span>
      <Button size="sm" variant="ghost" onClick={() => { setFailed(false); setRetry((old) => old + 1); }}>
        {c('重新读取')}
      </Button>
    </div> : null}
  </Context.Provider>;
}
