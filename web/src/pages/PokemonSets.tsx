import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ArrowRight, ChevronDown, ChevronUp, ScanLine, Trophy } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'
import AddCardPanel from '../components/pokemon/AddCardPanel'
import { formatDate } from '../utils/formatDate'

// PokemonSetsLocationState carries the optional "open AddCardPanel pre-filled
// with this query" hint that the /pokemon/scanned page passes through
// React Router's location state when the user clicks "Enter manually" on a
// no_match row. Once consumed by the panel it is cleared via navigate(..., {
// replace: true }) so refresh / back navigation doesn't re-open the dialog.
interface PokemonSetsLocationState {
  addCardQuery?: string
}

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
  const location = useLocation()
  const navigate = useNavigate()

  // Owned-only filter state is mirrored to ?owned=true in the URL so a reload
  // or a shared link preserves the toggle. The toggle reads its initial value
  // from the current URL on every render of the search string.
  const ownedOnly = useMemo(() => {
    const params = new URLSearchParams(location.search)
    return params.get('owned')?.toLowerCase() === 'true'
  }, [location.search])

  const [sets, setSets] = useState<PokemonSet[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showOlder, setShowOlder] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [unresolvedCount, setUnresolvedCount] = useState(0)

  const locState = location.state as PokemonSetsLocationState | null
  const initialAddCardQuery = locState?.addCardQuery ?? undefined

  const toggleOwnedOnly = useCallback(() => {
    const params = new URLSearchParams(location.search)
    if (ownedOnly) {
      params.delete('owned')
    } else {
      params.set('owned', 'true')
    }
    const next = params.toString()
    navigate({ pathname: location.pathname, search: next ? `?${next}` : '' }, { replace: false, state: location.state })
  }, [ownedOnly, navigate, location.pathname, location.search])

  // After the AddCardPanel consumes its initialQuery we strip the hint from
  // history so a subsequent back/forward navigation doesn't re-open the
  // dialog. `replace: true` swaps the current history entry in place.
  const handleInitialQueryConsumed = useCallback(() => {
    if (locState?.addCardQuery) {
      navigate(location.pathname + location.search, { replace: true, state: null })
    }
  }, [locState?.addCardQuery, navigate, location.pathname, location.search])

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    setAttempt(a => a + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    const limit = 50

    ;(async () => {
      setLoading(true)
      setError('')
      try {
        const allSets: PokemonSet[] = []
        let offset = 0
        while (true) {
          const params = new URLSearchParams({
            limit: String(limit),
            offset: String(offset),
          })
          if (ownedOnly) params.set('owned', 'true')
          const res = await fetch(
            `/api/pokemon/sets?${params.toString()}`,
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
  }, [t, attempt, ownedOnly])

  // Fetch the unresolved scan count once when the page mounts (and after a
  // reload triggered by AddCardPanel) so the banner reflects scans that
  // resolved while the user was elsewhere. The sidebar polls every 30 s; one
  // shot is enough here since clicking through to /pokemon/scanned is the
  // user's next move when count > 0.
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/pokemon/scans/counts', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : { unresolved: 0 }))
      .then((data: { unresolved?: number }) => {
        if (!controller.signal.aborted) setUnresolvedCount(data.unresolved ?? 0)
      })
      .catch(() => { /* badge stays at 0; no-op */ })
    return () => { controller.abort() }
  }, [attempt])

  // The top 3 series ranked by the most-recent release_date in each series are
  // shown expanded; everything else is hidden behind the "Show older sets"
  // toggle. Older series are sorted newest-first by their max release_date,
  // with ties broken alphabetically for determinism.
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
    const erasByLatest: Array<[string, PokemonSet[]]> = [...byEra.entries()]
    erasByLatest.sort((a, b) => {
      const aLatest = a[1][0]?.release_date ?? ''
      const bLatest = b[1][0]?.release_date ?? ''
      if (aLatest < bLatest) return 1
      if (aLatest > bLatest) return -1
      return a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0
    })
    const recentGroups = erasByLatest.slice(0, 3)
    const olderGroups = erasByLatest.slice(3)
    return { recent: recentGroups, older: olderGroups }
  }, [sets])

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <div className="max-w-5xl mx-auto px-4 py-6 space-y-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="text-2xl font-semibold">{t('pageTitle')}</h1>
          <div className="flex items-center gap-3 flex-wrap">
            <label className="inline-flex items-center gap-2 text-sm text-gray-300 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={ownedOnly}
                onChange={toggleOwnedOnly}
                className="h-4 w-4 rounded border-gray-600 bg-gray-800 text-amber-500 focus:ring-amber-500"
                data-testid="pokemon-sets-owned-toggle"
              />
              <span>{t('sets.filterOwnedOnly')}</span>
            </label>
            <Link
              to="/pokemon/top"
              aria-label={t('top.entryLabel')}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs sm:text-sm bg-amber-600/20 hover:bg-amber-600/30 border border-amber-500/40 text-amber-200 rounded transition-colors"
              data-testid="pokemon-top-link"
            >
              <Trophy size={16} aria-hidden="true" />
              <span>{t('top.entryButton')}</span>
            </Link>
          </div>
        </header>

        {unresolvedCount > 0 && (
          <Link
            to="/pokemon/scanned"
            data-testid="pokemon-scanned-banner"
            className="flex items-center justify-between gap-3 px-3 py-2 bg-amber-600/15 hover:bg-amber-600/25 border border-amber-500/40 text-amber-200 rounded transition-colors"
          >
            <span className="inline-flex items-center gap-2 text-sm">
              <ScanLine size={16} aria-hidden="true" />
              <span>{t('scannedBanner.linkText', { count: unresolvedCount })}</span>
            </span>
            <ArrowRight size={16} aria-hidden="true" />
          </Link>
        )}

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

        {!loading && !error && ownedOnly && sets.length === 0 && (
          <p
            data-testid="pokemon-sets-empty-owned"
            className="text-sm text-gray-400 px-3 py-8 text-center"
          >
            {t('sets.emptyOwned')}
          </p>
        )}

        {!loading && !error && !(ownedOnly && sets.length === 0) && (
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
      <AddCardPanel
        onAdded={load}
        initialQuery={initialAddCardQuery}
        onInitialQueryConsumed={handleInitialQueryConsumed}
      />
    </div>
  )
}
