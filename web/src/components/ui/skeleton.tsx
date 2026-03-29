import React from 'react'
import { cn } from '../../lib/utils'

function Skeleton({
  className,
  'aria-hidden': ariaHidden,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledby,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  const computedAriaHidden =
    ariaHidden !== undefined
      ? ariaHidden
      : ariaLabel || ariaLabelledby
        ? undefined
        : true

  return (
    <div
      aria-hidden={computedAriaHidden}
      aria-label={ariaLabel}
      aria-labelledby={ariaLabelledby}
      className={cn('animate-pulse rounded-md bg-gray-700/60', className)}
      {...props}
    />
  )
}

export { Skeleton }
