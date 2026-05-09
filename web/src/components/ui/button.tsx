import { Button as ButtonPrimitive } from "@base-ui/react/button"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-md border-2 border-border bg-clip-padding text-sm font-bold whitespace-nowrap transition-all duration-100 ease outline-none select-none focus-visible:ring-0 focus-visible:nb-shadow-focus disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground nb-shadow-sm nb-press",
        secondary:
          "bg-secondary text-secondary-foreground nb-shadow-sm nb-press",
        success:
          "bg-[var(--success)] text-[var(--success-foreground)] nb-shadow-sm nb-press",
        outline:
          "border-border bg-card text-foreground nb-shadow-sm nb-press hover:bg-muted",
        ghost:
          "border-transparent bg-transparent text-foreground shadow-none hover:bg-muted",
        destructive:
          "bg-destructive text-destructive-foreground nb-shadow-sm nb-press",
        link: "border-transparent text-foreground underline-offset-4 hover:underline shadow-none",
      },
      size: {
        default: "h-9 gap-1.5 px-5 py-2.5",
        xs: "h-6 gap-1 px-2.5 py-1 text-xs rounded-sm",
        sm: "h-7 gap-1 px-3.5 py-1.5 text-xs",
        lg: "h-10 gap-2 px-7 py-3.5 text-base",
        icon: "size-9",
        "icon-xs": "size-6 rounded-sm",
        "icon-sm": "size-7",
        "icon-lg": "size-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
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
