import { cn } from '@/lib/utils'
import type { HTMLAttributes } from 'react'

export function Container({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('mx-auto w-full max-w-7xl px-6', className)} {...props}>
      {children}
    </div>
  )
}
