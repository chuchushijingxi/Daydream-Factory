// Isolated UI verification. No API, account, upload or model calls.
import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { LanguageProvider } from '../src/contexts/LanguageContext';
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogTrigger,
  DialogFooter,
} from '../src/components/ui/dialog';
import { Button } from '../src/components/ui/button';
import { Toaster, toast } from '../src/components/ui/sonner';
import { ControlTooltip } from '../src/components/ui/control-tooltip';
import '../src/main.css';
import '../src/styles/jimeng-controls.css';
import '../src/styles/workspace.css';
import '../src/styles/model-config.css';
import '../src/styles/account-profile.css';
import '../src/styles/notifications.css';
import '../src/styles/feedback.css';
const query = new URLSearchParams(location.search);
if (query.get('theme') === 'dark') document.documentElement.classList.add('dark');
if (query.get('motion') === 'reduce') {
  const style = document.createElement('style');
  style.textContent =
    '*,*:before,*:after{animation-duration:1ms!important;transition-duration:1ms!important}';
  document.head.append(style);
}
function Example() {
  const [kind, setKind] = useState<'form' | 'settings' | 'confirm' | 'preview' | 'drawer' | null>(
      null,
    ),
    [nested, setNested] = useState(false),
    [draft, setDraft] = useState('保留的输入'),
    [busy, setBusy] = useState(false),
    [failure, setFailure] = useState(''),
    [retry, setRetry] = useState(false);
  const close = () => {
    if (!busy) setKind(null);
  };
  const confirm = () => {
    if (busy) return;
    setBusy(true);
    setTimeout(() => {
      setBusy(false);
      if (!retry) {
        setFailure('测试失败，当前对象与输入保留，可重试。');
        setRetry(true);
      } else {
        setKind(null);
        toast.success('测试确认完成');
      }
    }, 500);
  };
  return (
    <main
      style={{
        minHeight: '100dvh',
        background: 'var(--jm-workspace)',
        color: 'var(--jm-text)',
        padding: 32,
      }}
    >
      <h1 style={{ fontSize: 20, marginBottom: 12 }}>统一反馈与弹窗 · 隔离检查</h1>
      <p style={{ fontSize: 13, color: 'var(--jm-muted)', marginBottom: 24 }}>
        全部为测试数据，不连接账号、数据库或模型。
      </p>
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', maxWidth: 960 }}>
        <Button onClick={() => document.documentElement.classList.toggle('dark')}>切换主题</Button>
        <Button
          variant="outline"
          onClick={() => {
            setKind('confirm');
            setTimeout(() => setKind(null), 50);
            setTimeout(() => setKind('confirm'), 150);
          }}
        >
          快速重开确认
        </Button>
        <Button onClick={() => toast.success('保存成功', { id: 'same-save' })}>成功提示</Button>
        <Button onClick={() => toast.info('提示信息')}>信息提示</Button>
        <Button onClick={() => toast.warning('请检查所选内容')}>警告提示</Button>
        <Button onClick={() => toast.error('操作未完成，请重新读取后重试。')}>错误提示</Button>
        {(['form', 'settings', 'confirm', 'preview', 'drawer'] as const).map((v, i) => (
          <Button
            key={v}
            variant="outline"
            onClick={() => {
              setFailure('');
              setRetry(false);
              setKind(v);
            }}
          >
            {['表单弹窗', '设置弹窗', '确认弹窗', '图片预览', '消息抽屉'][i]}
          </Button>
        ))}
        <ControlTooltip label="这是禁用操作的完整原因，长提示在窄屏也保持可读。" disabled>
          <Button disabled>不可用操作</Button>
        </ControlTooltip>
      </div>
      <Dialog
        open={kind !== null}
        onOpenChange={(v) => {
          if (!v) close();
        }}
      >
        <DialogContent
          variant={kind || 'form'}
          closeDisabled={busy}
          className={kind === 'drawer' ? 'notification-drawer' : undefined}
        >
          <DialogTitle className={kind === 'preview' ? 'sr-only' : undefined}>
            {kind === 'confirm'
              ? '确认操作？'
              : kind === 'settings'
                ? '设置'
                : kind === 'drawer'
                  ? '消息中心'
                  : kind === 'preview'
                    ? '测试图片预览'
                    : '编辑资料'}
          </DialogTitle>
          <DialogDescription
            className={kind === 'preview' || kind === 'drawer' ? 'sr-only' : undefined}
          >
            {kind === 'confirm'
              ? '这是隔离的本地测试操作，失败后保留当前对象。'
              : '使用同一套弹窗组件，输入和业务状态保留。'}
          </DialogDescription>
          {kind === 'confirm' ? (
            <>
              {failure ? (
                <p role="alert" className="jm-inline-error">
                  {failure}
                </p>
              ) : null}
              <DialogFooter>
                <Button variant="outline" disabled={busy} onClick={close}>
                  取消
                </Button>
                <Button variant="destructive" busy={busy} onClick={confirm}>
                  {retry ? '重试确认' : '确认'}
                </Button>
              </DialogFooter>
            </>
          ) : kind === 'preview' ? (
            <img
              alt="本地测试图片"
              src={
                'data:image/svg+xml,' +
                encodeURIComponent(
                  '<svg xmlns="http://www.w3.org/2000/svg" width="960" height="540"><rect width="960" height="540" fill="#e5e5e5"/><text x="480" y="285" fill="#444" text-anchor="middle" font-size="32">测试图片 · 无外发</text></svg>',
                )
              }
            />
          ) : kind === 'drawer' ? (
            <div className="notification-list">
              <p style={{ marginTop: 24 }}>虚构通知内容。</p>
            </div>
          ) : (
            <>
              <label style={{ display: 'grid', gap: 8, marginTop: 24 }}>
                名称
                <input
                  className="jm-field-input"
                  aria-label="测试名称"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                />
              </label>
              <Dialog open={nested} onOpenChange={setNested}>
                <DialogTrigger asChild>
                  <Button variant="outline" style={{ marginTop: 24 }}>
                    选择素材
                  </Button>
                </DialogTrigger>
                <DialogContent variant="picker">
                  <DialogTitle>选择参考素材</DialogTitle>
                  <DialogDescription>嵌套层关闭后，父层输入保持。</DialogDescription>
                  <div style={{ minHeight: 160, paddingTop: 24 }}>测试素材 A</div>
                  <DialogFooter>
                    <Button onClick={() => setNested(false)}>确认选择</Button>
                  </DialogFooter>
                </DialogContent>
              </Dialog>
              <DialogFooter style={{ marginTop: 24 }}>
                <Button variant="outline" onClick={close}>
                  取消
                </Button>
                <Button
                  onClick={() => {
                    toast.success('本地输入已保留');
                    setKind(null);
                  }}
                >
                  完成
                </Button>
                <Button onClick={() => toast.error('弹窗内错误提示，可关闭。')}>测试错误</Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>
      <Toaster />
    </main>
  );
}
createRoot(document.getElementById('root')!).render(
  <LanguageProvider>
    <Example />
  </LanguageProvider>,
);
