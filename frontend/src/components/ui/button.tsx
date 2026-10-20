import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from './utils';
import { Spinner } from './spinner';
const buttonVariants = cva('jm-button', {
  variants: {
    variant: {
      default: 'jm-button--primary',
      accent: 'jm-button--accent',
      success: 'jm-button--success',
      warning: 'jm-button--warning',
      destructive: 'jm-button--danger',
      outline: 'jm-button--outline',
      secondary: 'jm-button--secondary',
      ghost: 'jm-button--ghost',
      link: 'jm-button--link',
    },
    size: {
      default: 'jm-button--md',
      sm: 'jm-button--sm',
      lg: 'jm-button--lg',
      icon: 'jm-button--icon',
    },
  },
  defaultVariants: {
    variant: 'default',
    size: 'default',
  },
});
const Button = React.forwardRef<
  HTMLButtonElement,
  React.ComponentProps<'button'> &
    VariantProps<typeof buttonVariants> & {
      asChild?: boolean;
      busy?: boolean;
    }
>(
  (
    { className, variant, size, asChild = false, busy = false, disabled, type, children, ...props },
    ref,
  ) => {
    const Comp = asChild ? Slot : 'button';
    return (
      <Comp
        {...props}
        type={asChild ? type : type || 'button'}
        ref={ref}
        data-slot="button"
        className={cn(
          buttonVariants({
            variant,
            size,
            className,
          }),
        )}
        disabled={disabled || busy}
        aria-busy={busy || undefined}
      >
        {busy && !asChild ? (
          <>
            <span className="jm-button-copy">{children}</span>
            <Spinner button />
          </>
        ) : (
          children
        )}
      </Comp>
    );
  },
);
Button.displayName = 'Button';
export { Button, buttonVariants };
