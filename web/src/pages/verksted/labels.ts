import type { TFunction } from 'i18next'
import type { GiftStatus } from './api'

export function statusLabel(t: TFunction<'verksted'>, s: GiftStatus): string {
  switch (s) {
    case 'idea': return t('status.idea')
    case 'planned': return t('status.planned')
    case 'ordered': return t('status.ordered')
    case 'arrived': return t('status.arrived')
    case 'wrapped': return t('status.wrapped')
    case 'given': return t('status.given')
  }
}

/** Tailwind colours for a status chip. */
export const STATUS_STYLE: Record<GiftStatus, string> = {
  idea: 'bg-gray-700 text-gray-200',
  planned: 'bg-sky-900 text-sky-200',
  ordered: 'bg-amber-900 text-amber-200',
  arrived: 'bg-violet-900 text-violet-200',
  wrapped: 'bg-emerald-900 text-emerald-200',
  given: 'bg-rose-900 text-rose-200',
}
