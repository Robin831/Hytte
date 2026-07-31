import { useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import type { ParseKeys } from 'i18next'
import { useTranslation } from 'react-i18next'
import { CalendarDays, ChefHat, Link2, Loader2, Plus, Search, Star } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import { matchesSearch, useCookAgain, useRecipes } from '../hooks/useRecipes'
import { CUISINE_TAGS, OCCASION_TAGS, SEASON_TAGS, type Recipe } from '../types/recipes'

/**
 * Recipe list: tag filters, rating and last-cooked per card, a "cook again"
 * row and the entry points for creating or importing a recipe.
 *
 * The list is fetched once, unfiltered, and both the search box and the tag
 * chips narrow it in the browser — no request is made while the user types or
 * toggles a chip. Tags could go out as `tag_all` params, but that endpoint
 * requires *every* selected tag to be present, which cannot express "Italian
 * or Thai"; the rule here is OR within a dimension, AND across dimensions.
 * Search has no server-side equivalent at all, because recipe text is
 * encrypted at rest.
 */

/** The three tag dimensions the filter chips expose. */
const DIMENSIONS = ['cuisine', 'season', 'occasion'] as const

type Dimension = (typeof DIMENSIONS)[number]

type FilterState = Record<Dimension, string[]>

const EMPTY_FILTERS: FilterState = { cuisine: [], season: [], occasion: [] }

/** The vocabulary each dimension draws from — see `types/recipes.ts`. */
const DIMENSION_TAGS: Record<Dimension, readonly string[]> = {
  cuisine: CUISINE_TAGS,
  season: SEASON_TAGS,
  occasion: OCCASION_TAGS,
}

/**
 * Reverse index from a vocabulary value to its dimension. Recipe tags are
 * free-form, so a tag that is not in any vocabulary has no translation and is
 * shown as the user typed it.
 */
const TAG_DIMENSION: Record<string, Dimension> = Object.fromEntries(
  DIMENSIONS.flatMap(dimension =>
    DIMENSION_TAGS[dimension].map(tag => [tag, dimension] as [string, Dimension]),
  ),
)

/**
 * Every vocabulary value doubles as an i18n key under
 * `recipes:filters.<dimension>Values.*`, so the key is composed rather than
 * enumerated. The cast is needed because the composed string is wider than the
 * literal union `t` accepts; only vocabulary values ever reach it.
 */
function tagLabelKey(dimension: Dimension, value: string): ParseKeys<'recipes'> {
  return `filters.${dimension}Values.${value}` as ParseKeys<'recipes'>
}

/**
 * The values worth offering for a dimension: vocabulary order (so chips do not
 * jump around as recipes come and go), narrowed to tags at least one recipe
 * actually carries.
 */
function filterValues(recipes: Recipe[], dimension: Dimension): string[] {
  const present = new Set(recipes.flatMap(recipe => recipe.tags))
  return DIMENSION_TAGS[dimension].filter(tag => present.has(tag))
}

/**
 * A recipe passes when every dimension with a selection matches at least one of
 * its selected values. An empty selection means the dimension is ignored.
 */
function matchesFilters(recipe: Recipe, filters: FilterState): boolean {
  return DIMENSIONS.every(dimension => {
    const selected = filters[dimension]
    if (selected.length === 0) return true
    return selected.some(value => recipe.tags.includes(value))
  })
}

/** True when at least one dimension has a selection. */
function hasActiveFilters(filters: FilterState): boolean {
  return DIMENSIONS.some(dimension => filters[dimension].length > 0)
}

/** Read-only five-star display; unrated recipes render five empty stars. */
function RatingStars({ value }: { value: number | null }) {
  const { t } = useTranslation('recipes')
  const label = value == null ? t('rating.none') : t('rating.value', { value })

  return (
    <span role="img" aria-label={label} className="flex shrink-0 items-center gap-0.5">
      {[1, 2, 3, 4, 5].map(star => (
        <Star
          key={star}
          size={16}
          aria-hidden="true"
          className={
            value != null && star <= value ? 'text-amber-400 fill-amber-400' : 'text-gray-600'
          }
        />
      ))}
    </span>
  )
}

/** One recipe card: title, rating, last-cooked date, yield and tags. */
function RecipeCard({ recipe }: { recipe: Recipe }) {
  const { t } = useTranslation('recipes')

  const lastCooked = recipe.last_cooked_at
    ? t('list.lastCooked', {
        date: formatDate(recipe.last_cooked_at, {
          year: 'numeric',
          month: 'short',
          day: 'numeric',
        }),
      })
    : t('list.neverCooked')

  return (
    <Link
      to={`/recipes/${recipe.id}`}
      aria-label={`${t('list.open')}: ${recipe.title}`}
      className="block bg-gray-800 hover:bg-gray-700 rounded-lg p-4 transition-colors"
    >
      <div className="flex items-start justify-between gap-3">
        <h3 className="font-medium break-words min-w-0">{recipe.title}</h3>
        <RatingStars value={recipe.rating} />
      </div>
      <p className="mt-1 text-sm text-gray-400">{lastCooked}</p>
      {recipe.servings > 0 && (
        <p className="text-sm text-gray-500">{t('list.servings', { count: recipe.servings })}</p>
      )}
      {recipe.tags.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1">
          {recipe.tags.map(tag => {
            const dimension = TAG_DIMENSION[tag]
            return (
              <span
                key={tag}
                className="px-2 py-0.5 text-xs rounded-full bg-gray-700 text-gray-300"
              >
                {dimension ? t(tagLabelKey(dimension, tag)) : tag}
              </span>
            )
          })}
        </div>
      )}
    </Link>
  )
}

export default function RecipesPage() {
  const { t } = useTranslation('recipes')
  const navigate = useNavigate()

  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<FilterState>(EMPTY_FILTERS)
  const [importOpen, setImportOpen] = useState(false)
  const [importUrl, setImportUrl] = useState('')
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')

  // Fetched without any filter arguments, so the effect never re-runs while the
  // user types or toggles a chip, and the chip vocabulary below is derived from
  // the full list rather than from an already-narrowed one.
  const { recipes, loading, error } = useRecipes()
  const cookAgain = useCookAgain()

  const filtered = useMemo(
    () =>
      recipes.filter(recipe => matchesFilters(recipe, filters) && matchesSearch(recipe, search)),
    [recipes, filters, search],
  )

  const filtersActive = hasActiveFilters(filters)
  // The suggestions are ranked over every recipe, so showing them beside a
  // narrowed list would contradict the active filters.
  const showCookAgain = !filtersActive && search.trim() === '' && cookAgain.recipes.length > 0

  function toggleTag(dimension: Dimension, value: string) {
    setFilters(prev => {
      const selected = prev[dimension]
      return {
        ...prev,
        [dimension]: selected.includes(value)
          ? selected.filter(tag => tag !== value)
          : [...selected, value],
      }
    })
  }

  async function submitImport(e: FormEvent) {
    e.preventDefault()
    const url = importUrl.trim()
    if (!url || importing) return

    setImporting(true)
    setImportError('')
    try {
      const res = await fetch('/api/recipes/import', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToImport'))
      }
      const data = await res.json()
      // The import endpoint writes nothing — it returns a parsed recipe for the
      // user to review. Hand it to the detail page in create mode, which is
      // where it gets edited and saved through POST /api/recipes.
      navigate('/recipes/new', { state: { importedRecipe: data.recipe, importUrl: url } })
    } catch (err) {
      setImportError(err instanceof Error ? err.message : t('errors.failedToImport'))
    } finally {
      setImporting(false)
    }
  }

  return (
    <div className="max-w-4xl mx-auto px-4 py-6">
      <div className="flex flex-wrap items-start justify-between gap-3 mb-4">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold">{t('list.title')}</h1>
          <p className="text-sm text-gray-400">{t('list.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            to="/recipes/planner"
            className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors text-sm"
          >
            <CalendarDays size={16} aria-hidden="true" />
            {t('planner.open')}
          </Link>
          <button
            type="button"
            onClick={() => {
              setImportOpen(open => !open)
              setImportError('')
            }}
            aria-expanded={importOpen}
            className="flex items-center gap-2 px-3 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
          >
            <Link2 size={16} aria-hidden="true" />
            {t('list.import')}
          </button>
          <button
            type="button"
            onClick={() => navigate('/recipes/new')}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer text-sm"
          >
            <Plus size={16} aria-hidden="true" />
            {t('list.create')}
          </button>
        </div>
      </div>

      {importOpen && (
        <form onSubmit={submitImport} className="mb-4 bg-gray-800 rounded-lg p-3">
          <div className="flex flex-col sm:flex-row gap-2">
            <input
              type="url"
              value={importUrl}
              onChange={e => setImportUrl(e.target.value)}
              aria-label={t('list.import')}
              placeholder={t('list.importPlaceholder')}
              className="flex-1 min-w-0 px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm"
            />
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={importing || importUrl.trim() === ''}
                className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer text-sm disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {importing && <Loader2 size={16} aria-hidden="true" className="animate-spin" />}
                {importing ? t('list.importing') : t('list.importSubmit')}
              </button>
              <button
                type="button"
                onClick={() => {
                  setImportOpen(false)
                  setImportError('')
                }}
                className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg transition-colors cursor-pointer text-sm"
              >
                {t('list.importCancel')}
              </button>
            </div>
          </div>
          {importError && <p className="mt-2 text-sm text-red-300">{importError}</p>}
        </form>
      )}

      <div className="relative mb-4">
        <Search
          size={16}
          aria-hidden="true"
          className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500"
        />
        <input
          type="search"
          value={search}
          onChange={e => setSearch(e.target.value)}
          aria-label={t('list.search')}
          placeholder={t('list.searchPlaceholder')}
          className="w-full pl-9 pr-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm"
        />
      </div>

      {/* Each dimension is its own chip row that scrolls sideways instead of
          wrapping, so a long vocabulary cannot push the page wider than a
          375px viewport. */}
      <section aria-label={t('filters.label')} className="mb-6 space-y-2">
        {DIMENSIONS.map(dimension => {
          const values = filterValues(recipes, dimension)
          if (values.length === 0) return null
          return (
            <div key={dimension} className="flex items-center gap-2 min-w-0">
              <span className="w-20 shrink-0 text-xs uppercase tracking-wide text-gray-500">
                {t(`filters.${dimension}`)}
              </span>
              <div
                role="group"
                aria-label={t(`filters.${dimension}`)}
                className="flex flex-nowrap gap-2 overflow-x-auto py-1"
              >
                {values.map(value => {
                  const active = filters[dimension].includes(value)
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={active}
                      onClick={() => toggleTag(dimension, value)}
                      className={`shrink-0 px-3 py-1 rounded-full text-sm transition-colors cursor-pointer ${
                        active
                          ? 'bg-blue-600 text-white'
                          : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
                      }`}
                    >
                      {t(tagLabelKey(dimension, value))}
                    </button>
                  )
                })}
              </div>
            </div>
          )
        })}
        {filtersActive && (
          <button
            type="button"
            onClick={() => setFilters(EMPTY_FILTERS)}
            className="text-sm text-blue-400 hover:text-blue-300 cursor-pointer"
          >
            {t('filters.clear')}
          </button>
        )}
      </section>

      {error && (
        <div className="mb-4 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {showCookAgain && (
        <section aria-labelledby="cook-again-heading" className="mb-6">
          <h2 id="cook-again-heading" className="text-lg font-semibold">
            {t('list.cookAgain')}
          </h2>
          <p className="mb-2 text-sm text-gray-400">{t('list.cookAgainSubtitle')}</p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {cookAgain.recipes.map(recipe => (
              <RecipeCard key={recipe.id} recipe={recipe} />
            ))}
          </div>
        </section>
      )}

      {loading ? (
        <p role="status" aria-busy="true" className="text-gray-400">
          {t('list.loading')}
        </p>
      ) : filtered.length === 0 ? (
        <div className="text-center text-gray-500 py-12">
          <ChefHat size={48} aria-hidden="true" className="mx-auto mb-4 opacity-30" />
          {recipes.length === 0 && !filtersActive ? (
            <>
              <p className="text-lg">{t('empty.title')}</p>
              <p className="text-sm mt-1">{t('empty.description')}</p>
              <button
                type="button"
                onClick={() => navigate('/recipes/new')}
                className="mt-4 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer text-sm"
              >
                {t('empty.action')}
              </button>
            </>
          ) : (
            <>
              <p className="text-lg">{t('empty.filtered')}</p>
              <button
                type="button"
                onClick={() => {
                  setFilters(EMPTY_FILTERS)
                  setSearch('')
                }}
                className="mt-4 px-4 py-2 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors cursor-pointer text-sm"
              >
                {t('empty.filteredAction')}
              </button>
            </>
          )}
        </div>
      ) : (
        <section aria-label={t('list.title')}>
          <p className="mb-2 text-sm text-gray-400">
            {t('list.resultCount', { count: filtered.length })}
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {filtered.map(recipe => (
              <RecipeCard key={recipe.id} recipe={recipe} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
