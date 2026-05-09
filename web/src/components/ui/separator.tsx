import { Separator as SeparatorPrimitive } from "@base-ui/react/separator"

import { cn } from "@/lib/utils"

function Separator({
  className,
  orientation = "horizontal",
  ...props
}: SeparatorPrimitive.Props) {
  return (
    <SeparatorPrimitive
      data-slot="separator"
      orientation={orientation}
      className={cn(
        "shrink-0 data-horizontal:h-[2px] data-horizontal:w-full data-vertical:w-[2px] data-vertical:self-stretch bg-[#EEEEEE]",
        className
      )}
      {...props}
    />
  )
}

export { Separator }
