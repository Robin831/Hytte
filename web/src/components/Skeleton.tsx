import { Skeleton } from './ui/skeleton'

/**
 * Shared skeleton placeholder primitives for in-content loading states.
 *
 * Built on the base `Skeleton` (animate-pulse + gray theme) from `ui/skeleton`.
 * All elements are decorative and inherit `aria-hidden` from the base component;
 * wrap groups in a `role="status" aria-busy="true"` container with an `sr-only`
 * localized label for screen readers (see the homework pages for the pattern).
 */

/** A configurable rectangular placeholder block. Pass width/height/rounding via `className`. */
export function SkeletonBlock({ className }: { className?: string }) {
  return <Skeleton className={className} />
}

/**
 * A list-row placeholder shaped like a homework conversation row:
 * a leading icon circle plus two stacked text lines inside a card.
 */
export function SkeletonRow() {
  return (
    <div className="w-full flex items-center gap-3 px-4 py-3 bg-gray-800 rounded-lg">
      <SkeletonBlock className="w-5 h-5 rounded-full shrink-0" />
      <div className="flex-1 min-w-0 space-y-2">
        <SkeletonBlock className="h-4 w-1/2" />
        <SkeletonBlock className="h-3 w-3/4" />
      </div>
    </div>
  )
}
