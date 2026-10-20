import { Dialog, DialogContent, DialogDescription, DialogTitle } from '../../components/ui/dialog';
import { Button } from '../../components/ui/button';
import type { SessionAudience } from '../../api/session-audience';
import { useCatalogText } from '../../contexts/useCatalogText';

export function SessionReplacementDialog({
  open,
  audience,
  onOpenChange,
}: {
  open: boolean;
  audience: SessionAudience;
  onOpenChange: (open: boolean) => void;
}) {
  const { c } = useCatalogText();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        variant="confirm"
        className="session-replacement-dialog"
        role="alertdialog"
        hideClose
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          document
            .querySelector<HTMLInputElement>('input[autocomplete="username"]')
            ?.focus({ preventScroll: true });
        }}
      >
        <DialogTitle>{c('账号已在其他位置登录')}</DialogTitle>
        <DialogDescription>
          {c(
            audience === 'operations'
              ? '当前运营后台会话已结束，请重新登录。'
              : '当前工作台会话已结束，请重新登录。',
          )}
        </DialogDescription>
        <div className="session-replacement-actions">
          <Button variant="accent" onClick={() => onOpenChange(false)}>
            {c('重新登录')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
