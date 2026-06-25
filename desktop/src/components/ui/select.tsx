'use client'

import { Select as SelectPrimitive } from '@base-ui/react/select'

import { cn } from '@/lib/utils'
import { CheckIcon, ChevronDownIcon } from 'lucide-react'

export interface SelectOption {
  value: string
  label: string
}

export interface SelectProps {
  options: SelectOption[]
  label?: string
  value?: string | null
  onChange?: (value: string) => void
  disabled?: boolean
  placeholder?: string
  className?: string
  id?: string
}

function Select({
  className,
  options,
  label,
  value,
  onChange,
  disabled,
  placeholder,
  id,
}: SelectProps) {
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      {label && (
        <label
          id={id}
          className="text-xs font-medium text-muted-foreground leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
        >
          {label}
        </label>
      )}
      <SelectPrimitive.Root
        value={value ?? null}
        onValueChange={(v: string | null) => onChange?.(v ?? '')}
        disabled={disabled}
      >
        <SelectPrimitive.Trigger
          data-slot="select-trigger"
          className={cn(
            'group flex h-8 w-full items-center justify-between gap-2 rounded-md border border-border bg-transparent px-3 py-1 text-sm text-foreground transition-colors outline-none',
            'focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50',
            'disabled:cursor-not-allowed disabled:opacity-50',
            'data-open:border-primary data-open:ring-2 data-open:ring-ring/50'
          )}
        >
          <SelectPrimitive.Value
            placeholder={placeholder || 'Select...'}
            className="flex-1 truncate text-left text-sm"
          >
            {(v: string | null) => {
              const selected = options.find((o) => o.value === v)
              return selected?.label || placeholder || 'Select...'
            }}
          </SelectPrimitive.Value>
          <SelectPrimitive.Icon className="shrink-0 text-muted-foreground transition-transform duration-200 data-open:rotate-180">
            <ChevronDownIcon className="size-4" />
          </SelectPrimitive.Icon>
        </SelectPrimitive.Trigger>
        <SelectPrimitive.Portal>
          <SelectPrimitive.Positioner
            side="bottom"
            align="start"
            sideOffset={4}
            className="isolate z-50"
          >
            <SelectPrimitive.Popup
              data-slot="select-popup"
              className="origin-(--transform-origin) min-w-[var(--anchor-width)] rounded-md border border-border bg-popover p-1 text-sm text-popover-foreground shadow-lg data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95"
            >
              <SelectPrimitive.List data-slot="select-list" className="flex flex-col gap-0.5">
                {options.map((opt) => (
                  <SelectPrimitive.Item
                    key={opt.value}
                    value={opt.value}
                    className="relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 outline-none data-highlighted:bg-muted data-disabled:pointer-events-none data-disabled:opacity-50 data-selected:bg-primary/10 data-selected:text-primary"
                  >
                    <SelectPrimitive.ItemIndicator className="flex shrink-0 items-center justify-center">
                      <CheckIcon className="size-4" />
                    </SelectPrimitive.ItemIndicator>
                    <SelectPrimitive.ItemText>{opt.label}</SelectPrimitive.ItemText>
                  </SelectPrimitive.Item>
                ))}
              </SelectPrimitive.List>
            </SelectPrimitive.Popup>
          </SelectPrimitive.Positioner>
        </SelectPrimitive.Portal>
      </SelectPrimitive.Root>
    </div>
  )
}

export { Select }
