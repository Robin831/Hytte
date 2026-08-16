import { Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface WorkoutFilterBarProps {
  // The sport keys to offer in the dropdown (the sportIcons keys from the page).
  sports: string[]
  sport: string
  setSport: (s: string) => void
  query: string
  setQuery: (q: string) => void
  onClear: () => void
}

export default function WorkoutFilterBar({
  sports,
  sport,
  setSport,
  query,
  setQuery,
  onClear,
}: WorkoutFilterBarProps) {
  const { t } = useTranslation('training')

  const hasActiveFilters = sport !== '' || query.trim() !== ''

  return (
    <div className="mb-4 space-y-3 bg-gray-800/50 border border-gray-700 rounded-xl p-3 sm:p-4">
      <div className="flex flex-col sm:flex-row gap-3">
        {/* Sport dropdown */}
        <div className="flex-shrink-0">
          <label htmlFor="workout-sport-filter" className="sr-only">
            {t('filters.sportLabel')}
          </label>
          <select
            id="workout-sport-filter"
            value={sport}
            onChange={(e) => setSport(e.target.value)}
            className="w-full sm:w-auto px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-gray-200 focus:outline-none focus:border-orange-400"
          >
            <option value="">{t('filters.allSports')}</option>
            {sports.map((s) => (
              <option key={s} value={s}>
                {t(`filters.sports.${s}`, { defaultValue: s })}
              </option>
            ))}
          </select>
        </div>

        {/* Text search */}
        <div className="relative flex-1">
          <label htmlFor="workout-search-filter" className="sr-only">
            {t('filters.searchLabel')}
          </label>
          <Search
            size={16}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 pointer-events-none"
          />
          <input
            id="workout-search-filter"
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('filters.searchPlaceholder')}
            className="w-full pl-9 pr-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-orange-400"
          />
        </div>

        {/* Clear filters */}
        {hasActiveFilters && (
          <button
            type="button"
            onClick={onClear}
            className="flex-shrink-0 flex items-center justify-center gap-1 px-3 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm text-gray-300 transition-colors"
          >
            <X size={16} />
            {t('filters.clear')}
          </button>
        )}
      </div>
    </div>
  )
}
