'use client'

import { Field } from '@base-ui/react/field'
import { Input as InputPrimitive } from '@base-ui/react/input'

import { cn } from '@/lib/utils'
import type { ComponentPropsWithoutRef } from 'react'

export interface InputProps extends Omit<ComponentPropsWithoutRef<'input'>, 'size'> {
  label?: string
  error?: string
}

function Input({ className, label, error, id, ...props }: InputProps) {
  return (
    <Field.Root className={cn('flex flex-col gap-1.5', className)}>
      {label && (
        <Field.Label className="text-xs font-medium text-muted-foreground leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
          {label}
        </Field.Label>
      )}
      <InputPrimitive
        id={id}
        data-slot="input"
        className={cn(
          'flex h-8 w-full rounded-md border bg-transparent px-3 py-1 text-sm text-foreground transition-colors outline-none',
          'placeholder:text-muted-foreground/50',
          'focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50',
          'disabled:cursor-not-allowed disabled:opacity-50',
          error && 'border-destructive'
        )}
        {...props}
      />
      {error && (
        <Field.Error match={true} className="text-[10px] text-destructive">
          {error}
        </Field.Error>
      )}
    </Field.Root>
  )
}

export { Input }
