import { type HTMLAttributes, forwardRef } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

const glassCardVariants = cva(
  // Base: rounded-xl, backdrop-blur-sm, border, transition, padding
  'rounded-xl border backdrop-blur-sm transition-all duration-200',
  {
    variants: {
      variant: {
        default: [
          'bg-card/80 border-border shadow-sm',
          'hover:-translate-y-0.5 hover:shadow-md hover:border-border/80',
        ].join(' '),
        active: [
          'bg-card/80 border-l-2 border-l-primary border-border shadow-sm',
          'hover:-translate-y-0.5 hover:shadow-md',
        ].join(' '),
        error: ['bg-card/80 border-l-2 border-l-destructive border-border shadow-sm'].join(' '),
        ghost: [
          'border-2 border-dashed border-border/50 bg-muted/20',
          'hover:bg-muted/30 hover:border-border/70',
        ].join(' '),
        flat: 'bg-card border-border shadow-none',
      },
      size: {
        default: 'p-4',
        sm: 'p-3',
        lg: 'p-5',
        none: '',
      },
      interactive: {
        true: 'cursor-pointer',
        false: '',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
      interactive: false,
    },
  }
)

// NOTE: On touch devices, hover lift transforms are disabled via the
// `.glass-card` CSS class which uses `@media (hover: none)` to prevent
// unwanted hover effects on tap. See index.css for the media query rule.

interface GlassCardProps
  extends HTMLAttributes<HTMLDivElement>, VariantProps<typeof glassCardVariants> {}

const GlassCard = forwardRef<HTMLDivElement, GlassCardProps>(
  ({ className, variant, size, interactive, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(glassCardVariants({ variant, size, interactive }), className)}
      {...props}
    />
  )
)
GlassCard.displayName = 'GlassCard'

export { GlassCard, glassCardVariants }
