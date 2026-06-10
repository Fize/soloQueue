'use client'

import { Field } from '@base-ui/react/field'

import { cn } from '@/lib/utils'
import { forwardRef } from 'react'
import type { ComponentPropsWithoutRef } from 'react'

export interface TextareaProps extends Omit<ComponentPropsWithoutRef<'textarea'>, 'size'> {
  label?: string
  error?: string
}

const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { className, label, error, id, ...props },
  ref
) {
  return (
    <Field.Root className="flex flex-col gap-1.5">
      {label && (
        <Field.Label className="text-xs font-medium text-muted-foreground leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
          {label}
        </Field.Label>
      )}
      <textarea
        ref={ref}
        id={id}
        data-slot="textarea"
        className={cn(
          'flex w-full rounded-md border bg-transparent px-3 py-2 text-sm text-foreground transition-colors outline-none',
          'placeholder:text-muted-foreground/50',
          'focus-visible:border-primary focus-visible:ring-2 focus-visible:ring-ring/50',
          'disabled:cursor-not-allowed disabled:opacity-50',
          'resize-y',
          error && 'border-destructive',
          className
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
})

export { Textarea }
