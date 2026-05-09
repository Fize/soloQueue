import { cn } from "@/lib/utils"
import type { InputHTMLAttributes } from "react"

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
}

function Input({ className, label, error, id, ...props }: InputProps) {
  const inputId = id || label?.toLowerCase().replace(/\s+/g, "-")

  return (
    <div className="flex flex-col gap-1.5">
      {label && (
        <label
          htmlFor={inputId}
          className="text-xs font-medium text-muted-foreground"
        >
          {label}
        </label>
      )}
      <input
        id={inputId}
        data-slot="input"
        className={cn(
          "flex h-8 w-full rounded-md border-2 border-[#EEEEEE] bg-transparent px-3 py-1 text-sm text-foreground transition-colors outline-none placeholder:text-muted-foreground/50 focus-visible:border-primary focus-visible:nb-shadow-focus disabled:cursor-not-allowed disabled:opacity-50",
          error && "border-destructive",
          className,
        )}
        {...props}
      />
      {error && <p className="text-[10px] text-destructive">{error}</p>}
    </div>
  )
}

export { Input }
