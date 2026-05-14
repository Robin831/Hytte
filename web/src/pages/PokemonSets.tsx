import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'
import AddCardPanel from '../components/pokemon/AddCardPanel'
import { formatDate } from '../utils/formatDate'

interface PokemonSet {
  id: string
  name: string
  series: string
  release_date: string
  total_cards: number
  symbol_url: string
  logo_url: string
  owned_count: number
}

// RECENT_ERAS lists the series that stay expanded by default. Any set whose
// `series` does not appear here is hidden behind the "Show older sets" toggle.
const RECENT_ERAS = ['Scarlet & Violet', 'Sword & Shield', 'Sun & Moon'] as const

// eraSlug converts a series name into a safe HTML id fragment so values like
// "Scarlet & Violet" don't break aria-labelledby (ids may not contain spaces
// and ampersands are awkward to reference).
function eraSlug(era: string): string {
  return era
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function formatReleaseDate(raw: string): string {
  // pokemontcg.io releases dates as "YYYY/MM/DD". `new Date('YYYY-MM-DD')`
  // parses as UTC midnight, which becomes the previous day in any negative-UTC
  // timezone, so we construct the Date from local components instead. Fall
  // back to the raw string when parsing fails so we never render
  // "Invalid Date".
  const m = raw.match(/^(\d{4})[/-](\d{1,2})[/-](\d{1,2})$/)
  let d: Date
  if (m) {
    d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  } else {
    d = new Date(raw)
  }
  if (Number.isNaN(d.getTime())) return raw
  try {
    return formatDate(d, { dateStyle: 'medium' })
  } catch {
    return raw
  }
}

function ownershipPercent(owned: number, total: number): number {
  if (total <= 0) return 0
  return Math.round((owned / total) * 100)
}

interface SetTileProps {
  set: PokemonSet
  t: TFunction<'pokemon'>
}

function SetTile({ set, t }: SetTileProps) {
  const percent = ownershipPercent(set.owned_count, set.total_cards)
  return (
    <Link
      to={`/pokemon/sets/${set.id}`}
      className="flex flex-col gap-2 p-3 bg-gray-800/40 border border-gray-800 rounded-lg hover:border-gray-700 hover:bg-gray-800/70 transition-colors"
      aria-label={t('tile.openSet', { name: set.name })}
      data-testid={`set-tile-${set.id}`}
    >
      <div className="h-14 flex items-center justify-center">
        {set.logo_url ? (
          <img
            src={set.logo_url}
            alt=""
            className="max-h-14 max-w-full object-contain"
            loading="lazy"
          />
        ) : (
          <span className="text-xs uppercase tracking-wide text-gray-500">{set.id}</span>
        )}
      </div>
      <div className="min-w-0">
        <p className="text-sm font-medium text-white truncate" title={set.name}>{set.name}</p>
        <p className="text-xs text-gray-500">{formatReleaseDate(set.release_date)}</p>
        <p className="text-xs text-gray-500">
          {t('tile.totalCards', { count: set.total_cards })}
        </p>
        <p className="mt-1 text-xs text-gray-300">
          {t('tile.ownership', {
            owned: set.owned_count,
            total: set.total_cards,
            percent,
          })}
        </p>
      </div>
    </Link>
  )
}

interface SetGridProps {
  sets: PokemonSet[]
  t: TFunction<'pokemon'>
}

function SetGrid({ sets, t }: SetGridProps) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
      {sets.map(set => (
        <SetTile key={set.id} set={set} t={t} />
      ))}
    </div>
  )
}

function SkeletonGrid() {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3" aria-busy="true">
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="p-3 bg-gray-800/40 border border-gray-800 rounded-lg space-y-2">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-3 w-1/2" />
          <Skeleton className="h-3 w-2/3" />
        </div>
      ))}
    </div>
  )
}

export default function PokemonSets() {
  const { t } = useTranslation('pokemon')

  const [sets, setSets] = useState<PokemonSet[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showOlder, setShowOlder] = useState(false)
  const [attempt, setAttempt] = useState(0)

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    setAttempt(a => a + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const limit = 50

    ;(async () => {
      try {
        const allSets: PokemonSet[] = []
        let offset = 0
        while (true) {
          const res = await fetch(
            `/api/pokemon/sets?limit=${limit}&offset=${offset}`,
            { credentials: 'include', signal: controller.signal },
          )
          if (!res.ok) throw new Error(t('errors.failedToLoad'))
          const data: { sets?: PokemonSet[] } = await res.json()
          const page = data.sets ?? []
          allSets.push(...page)
          if (page.length < limit) break
          offset += limit
        }
        setSets(allSets)
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [t, attempt])

  // groupedRecent and groupedOlder preserve the canonical order of RECENT_ERAS
  // and sort older series newest-first by the most-recent release_date in each
  // series. This keeps the page deterministic regardless of API ordering.
  const { recent, older } = useMemo(() => {
    const byEra = new Map<string, PokemonSet[]>()
    for (const s of sets) {
      const list = byEra.get(s.series)
      if (list) list.push(s)
      else byEra.set(s.series, [s])
    }
    for (const list of byEra.values()) {
      list.sort((a, b) => (a.release_date < b.release_date ? 1 : a.release_date > b.release_date ? -1 : 0))
    }
    const recentGroups: Array<[string, PokemonSet[]]> = []
    for (const era of RECENT_ERAS) {
      const list = byEra.get(era)
      if (list && list.length > 0) recentGroups.push([era, list])
    }
    const olderGroups: Array<[string, PokemonSet[]]> = []
    for (const [era, list] of byEra.entries()) {
      if ((RECENT_ERAS as readonly string[]).includes(era)) continue
      olderGroups.push([era, list])
    }
    olderGroups.sort((a, b) => {
      const aLatest = a[1][0]?.release_date ?? ''
      const bLatest = b[1][0]?.release_date ?? ''
      if (aLatest < bLatest) return 1
      if (aLatest > bLatest) return -1
      return a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0
    })
    return { recent: recentGroups, older: olderGroups }
  }, [sets])

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        <header>
          <h1 className="text-2xl font-semibold">{t('pageTitle')}</h1>
        </header>

        {error && (
          <div role="alert" className="px-3 py-2 bg-red-900/40 border border-red-800 text-red-300 text-sm rounded flex items-center justify-between gap-3">
            <span>{error}</span>
            <button
              type="button"
              onClick={load}
              className="px-2 py-1 text-xs bg-red-800/60 hover:bg-red-700 text-white rounded transition-colors cursor-pointer"
            >
              {t('retry')}
            </button>
          </div>
        )}

        {loading && !error && <SkeletonGrid />}

        {!loading && !error && (
          <>
            {recent.map(([era, list]) => {
              const slug = eraSlug(era)
              return (
                <section key={era} aria-labelledby={`era-${slug}`} className="space-y-3">
                  <h2 id={`era-${slug}`} className="text-lg font-semibold text-gray-200">{era}</h2>
                  <SetGrid sets={list} t={t} />
                </section>
              )
            })}

            {older.length > 0 && (
              <section className="space-y-3 pt-2 border-t border-gray-800">
                <button
                  type="button"
                  onClick={() => setShowOlder(v => !v)}
                  aria-expanded={showOlder}
                  className="flex items-center gap-2 text-sm text-gray-400 hover:text-white transition-colors cursor-pointer"
                >
                  {showOlder ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
                  <span>{showOlder ? t('hideOlder') : t('showOlder')}</span>
                </button>

                {showOlder && older.map(([era, list]) => {
                  const slug = eraSlug(era)
                  return (
                    <section key={era} aria-labelledby={`era-${slug}`} className="space-y-3">
                      <h2 id={`era-${slug}`} className="text-lg font-semibold text-gray-200">{era}</h2>
                      <SetGrid sets={list} t={t} />
                    </section>
                  )
                })}
              </section>
            )}
          </>
        )}
      </div>
      <AddCardPanel onAdded={load} />
    </div>
  )
}
