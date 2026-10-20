import { useCatalogText } from '../../contexts/useCatalogText';
import * as React from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { XIcon } from 'lucide-react';
import { Button } from './button';
import { cn } from './utils';
const DialogOpenContext = React.createContext(false);
export function Dialog({
  open,
  defaultOpen = false,
  onOpenChange,
  children,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Root>) {
  const [uncontrolledOpen, setUncontrolledOpen] = React.useState(defaultOpen);
  const visible = open ?? uncontrolledOpen;
  return (
    <DialogOpenContext.Provider value={visible}>
      <DialogPrimitive.Root
        {...props}
        open={visible}
        onOpenChange={(value) => {
          if (open === undefined) setUncontrolledOpen(value);
          onOpenChange?.(value);
        }}
      >
        {children}
      </DialogPrimitive.Root>
    </DialogOpenContext.Provider>
  );
}
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;
export const DialogPortal = DialogPrimitive.Portal;
export const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay ref={ref} className={cn('jm-dialog-mask', className)} {...props} />
));
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;
type DialogContentProps = React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
  variant?: 'form' | 'confirm' | 'settings' | 'picker' | 'panel' | 'preview' | 'drawer';
  hideClose?: boolean;
  closeDisabled?: boolean;
  overlayClassName?: string;
};
export const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  DialogContentProps
>(
  (
    {
      className,
      children,
      variant = 'form',
      hideClose,
      closeDisabled,
      overlayClassName,
      onOpenAutoFocus,
      onCloseAutoFocus,
      onEscapeKeyDown,
      onPointerDownOutside,
      ...props
    },
    ref,
  ) => {
    const { c } = useCatalogText();
    const open = React.useContext(DialogOpenContext);
    // Parents often clear their selection as they close. Keep the last visible
    // content and role until Radix finishes the matching exit animation.
    const displayed = React.useRef({ variant, className, children });
    if (open) displayed.current = { variant, className, children };
    const view = displayed.current;
    const previousFocus = React.useRef<HTMLElement | null>(null);
    return (
      <DialogPortal>
        <DialogOverlay className={overlayClassName} data-dialog-kind={view.variant} />
        <DialogPrimitive.Content
          ref={ref}
          className={cn('jm-dialog-content', view.className)}
          role={view.variant === 'confirm' ? 'alertdialog' : 'dialog'}
          data-dialog-kind={view.variant}
          {...props}
          onEscapeKeyDown={(event) => {
            onEscapeKeyDown?.(event);
            if (closeDisabled) event.preventDefault();
          }}
          onPointerDownOutside={(event) => {
            if (event.target instanceof Element && event.target.closest('.jm-toaster')) {
              event.preventDefault();
              return;
            }
            onPointerDownOutside?.(event);
            if (closeDisabled) event.preventDefault();
          }}
          onOpenAutoFocus={(event) => {
            previousFocus.current = document.activeElement as HTMLElement;
            onOpenAutoFocus?.(event);
          }}
          onCloseAutoFocus={(event) => {
            onCloseAutoFocus?.(event);
            if (
              !event.defaultPrevented &&
              previousFocus.current?.isConnected &&
              previousFocus.current !== document.body
            ) {
              event.preventDefault();
              previousFocus.current.focus({ preventScroll: true });
            }
          }}
        >
          {view.children}
          {!hideClose && (
            <DialogPrimitive.Close asChild>
              <Button
                variant="ghost"
                className="jm-button--close jm-dialog-close"
                disabled={closeDisabled}
                aria-label={c('关闭')}
              >
                <XIcon />
              </Button>
            </DialogPrimitive.Close>
          )}
        </DialogPrimitive.Content>
      </DialogPortal>
    );
  },
);
DialogContent.displayName = DialogPrimitive.Content.displayName;
export const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('jm-dialog-title', className)} {...props} />
));
DialogTitle.displayName = DialogPrimitive.Title.displayName;
export const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn('jm-dialog-description', className)}
    {...props}
  />
));
DialogDescription.displayName = DialogPrimitive.Description.displayName;
export function DialogFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      className={cn(
        'jm-dialog-actions flex flex-col-reverse gap-2 sm:flex-row sm:justify-end',
        className,
      )}
      {...props}
    />
  );
}
