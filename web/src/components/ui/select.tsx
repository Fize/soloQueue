import { cn } from "@/lib/utils"
import type { SelectHTMLAttributes } from "react"

export interface SelectOption {
  value: string
  label: string
}

export interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, "onChange"> {
  options: SelectOption[]
  label?: string
  onChange?: (value: string) => void
}

function Select({ className, options, label, onChange, id, ...props }: SelectProps) {
  const selectId = id || label?.toLowerCase().replace(/\s+/g, "-")

  return (
    <div className="flex flex-col gap-1.5">
      {label && (
        <label
          htmlFor={selectId}
          className="text-xs font-medium text-muted-foreground"
        >
          {label}
        </label>
      )}
      <select
        id={selectId}
        data-slot="select"
        className={cn(
          "flex h-8 w-full rounded-md border bg-transparent px-3 py-1 text-sm text-foreground transition-colors outline-none focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
        onChange={(e) => onChange?.(e.target.value)}
        {...props}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
    </div>
  )
}

export { Select }
