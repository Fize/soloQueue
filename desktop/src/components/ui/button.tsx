import { Button as ButtonPrimitive } from '@base-ui/react/button'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-md border border-border bg-clip-padding text-sm font-medium whitespace-nowrap transition-all duration-150 cubic-bezier(0, 0, 0, 1) outline-none select-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          'bg-primary text-primary-foreground shadow-sm hover:shadow-md hover:-translate-y-0.5 active:scale-[0.97]',
        secondary:
          'bg-secondary text-secondary-foreground shadow-sm hover:shadow-md hover:-translate-y-0.5 active:scale-[0.97]',
        success:
          'bg-[var(--success)] text-[var(--success-foreground)] shadow-sm hover:shadow-md hover:-translate-y-0.5 active:scale-[0.97]',
        outline:
          'border-border bg-card text-foreground shadow-sm hover:bg-muted hover:shadow-md hover:-translate-y-0.5 active:scale-[0.97]',
        ghost:
          'border-transparent bg-transparent text-foreground shadow-none hover:bg-muted active:scale-[0.98]',
        destructive:
          'bg-destructive text-destructive-foreground shadow-sm hover:shadow-md hover:-translate-y-0.5 active:scale-[0.97]',
        link: 'border-transparent text-foreground underline-offset-4 hover:underline shadow-none',
      },
      size: {
        default: 'h-10 min-h-10 gap-1.5 px-6 py-2.5',
        xs: 'h-7 gap-1 px-3 py-1 text-xs rounded-md',
        sm: 'h-8 gap-1 px-4 py-1.5 text-xs rounded-md',
        lg: 'h-11 gap-2 px-8 py-3.5 text-base rounded-lg',
        icon: 'size-10 min-h-10 min-w-10 rounded-md',
        'icon-xs': 'size-7 rounded-sm',
        'icon-sm': 'size-8 rounded-md',
        'icon-lg': 'size-11 rounded-lg',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
)

function Button({
  className,
  variant = 'default',
  size = 'default',
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
