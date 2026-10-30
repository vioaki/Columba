import { forwardRef } from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'


const buttonVariants = cva(
  'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-full text-sm font-medium transition-[color,border-color,background-color,box-shadow,transform,filter] duration-200 '
    + 'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background '
    + 'disabled:pointer-events-none disabled:opacity-45 active:translate-y-px [&_svg]:shrink-0 max-rail:min-h-11 max-rail:min-w-11',
  {
    variants: {
      variant: {
        default:
          'border border-[color:var(--seg-key-border)] bg-card text-secondary-foreground [box-shadow:var(--seg-key-shadow)] hover:border-rose hover:text-rose',
        primary:
          'bg-[var(--btn-primary)] text-[color:var(--btn-primary-ink)] [box-shadow:var(--btn-primary-shadow)] hover:bg-[var(--btn-primary-hover)] hover:text-[color:var(--btn-primary-ink)]',
        ghost:
          'border border-transparent text-muted-foreground hover:bg-wash hover:text-rose',
        danger:
          'border border-transparent text-muted-foreground hover:border-transparent hover:bg-[color-mix(in_srgb,var(--ember)_8%,transparent)] hover:text-ember',
        outline: 'border border-border bg-transparent hover:bg-wash hover:text-rose hover:border-rose',
        destructive: 'bg-destructive text-destructive-foreground shadow-sm hover:brightness-110',
      },
      size: {
        default: 'h-8 px-4 text-[13px] [&_svg]:size-[15px]',
        sm: 'h-7 px-3 text-xs [&_svg]:size-3.5',
        lg: 'h-10 px-6 text-sm [&_svg]:size-4',
        icon: 'h-8 w-8 [&_svg]:size-[15px]',
        iconSm: 'h-7 w-7 [&_svg]:size-3.5',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
  },
)
Button.displayName = 'Button'
