import { useTranslation } from 'react-i18next'
import { Coins } from 'lucide-react'
import { Skeleton } from '../ui/skeleton'

export interface CollectionValueCardProps {
  // null when no EUR/NOK rate is available; the card shows "price unavailable".
  totalNok: number | null
  ownedVariantCount: number
  loading?: boolean
  error?: boolean
}

// formatNok mirrors PokemonSet.formatNok: render whole NOK with the active
// locale's grouping. Unlike the per-card helper we keep an explicit zero here —
// an empty collection legitimately worth 0 NOK should read "0 kr", not "—".
function formatNok(amount: number, locale: string): string {
  return amount.toLocaleString(locale, {
    style: 'currency',
    currency: 'NOK',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  })
}

// CollectionValueCard is the portfolio-value summary shown at the top of the
// Pokémon sets page: the total NOK worth of every owned variant plus the count
// of distinct owned variants. It is purely presentational — the page owns the
// fetch and passes loading/error/value state in.
export default function CollectionValueCard({
  totalNok,
  ownedVariantCount,
  loading = false,
  error = false,
}: CollectionValueCardProps) {
  const { t, i18n } = useTranslation('pokemon')

  return (
    <section
      aria-label={t('value.title')}
      data-testid="pokemon-collection-value-card"
      className="flex items-center gap-3 p-4 bg-gray-800/40 border border-gray-800 rounded-lg"
    >
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-600/20 text-amber-300">
        <Coins size={22} aria-hidden="true" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-xs uppercase tracking-wide text-gray-500">{t('value.title')}</p>
        {loading ? (
          <Skeleton className="mt-1 h-7 w-32" />
        ) : error ? (
          <p className="mt-0.5 text-sm text-gray-400" data-testid="pokemon-collection-value-error">
            {t('value.error')}
          </p>
        ) : (
          <>
            <p
              className="mt-0.5 text-2xl font-semibold text-white"
              data-testid="pokemon-collection-value-total"
            >
              {totalNok == null ? t('value.unavailable') : formatNok(totalNok, i18n.language)}
            </p>
            <p className="text-xs text-gray-400">
              {t('value.ownedVariants', { count: ownedVariantCount })}
            </p>
          </>
        )}
      </div>
    </section>
  )
}
