import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams, Link } from 'react-router'
import { Flag, Search, CalendarClock, Compass, Plus, Sparkles, Route } from 'lucide-react'
import { useAuth } from '../../auth'
import {
  type Deadline, type RaceEvent, type RaceStatus, type Watch, type WatchState,
  WATCH_STATES, deadlineMoment, deleteWatch, distanceKind, fetchRaces, intlLocale, nextDeadline, pickText, setWatch,
} from './racesApi'
import { RaceRow, StatusPill } from './RaceParts'
import { useRaceFormat } from './useRaceFormat'
import RaceEditor from './RaceEditor'
import { ResearchLogPanel } from './Research'
import { SeasonPlanner } from './SeasonPlanner'
import { type Rates, RatesContext, matchesQuery, usePriceText } from './prices'

type Tab = 'mine' | 'season' | 'discover' | 'deadlines' | 'research'
const TAB_ICONS = { mine: Flag, season: Route, discover: Compass, deadlines: CalendarClock, research: Sparkles }

function parseTab(v: string | null, hasWatches: boolean, admin: boolean): Tab {
  if (v === 'mine' || v === 'season' || v === 'discover' || v === 'deadlines') return v
  if (v === 'research' && admin) return v
  return hasWatches ? 'mine' : 'discover'
}

type StatusFilter = 'all' | RaceStatus
type DistanceFilter = 'all' | 'half' | 'marathon'
type SeriesFilter = 'all' | 'majors' | 'emc'

export default function RacesPage() {
  const { t } = useRaceFormat()
  const { user } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()

  const [events, setEvents] = useState<RaceEvent[]>([])
  const [watches, setWatches] = useState<Watch[]>([])
  const [rates, setRates] = useState<Rates>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState<number | null>(null)
  const [creating, setCreating] = useState(false)

  const load = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchRaces(signal)
    if (signal?.aborted) return
    setEvents(data.events)
    setWatches(data.watches)
    setRates(data.rates ?? {})
    setError('')
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        await load(controller.signal)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.load'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [load, t])

  const reload = useCallback(() => {
    load().catch(() => setError(t('errors.load')))
  }, [load, t])

  const watchByEvent = useMemo(() => new Map(watches.map(w => [w.event_id, w])), [watches])
  const admin = !!user?.is_admin
  const tabs: Tab[] = admin ? ['mine', 'season', 'discover', 'deadlines', 'research'] : ['mine', 'season', 'discover', 'deadlines']
  const tab = parseTab(searchParams.get('tab'), watches.length > 0, admin)

  const changeTab = (next: Tab) => {
    setSearchParams(prev => {
      const p = new URLSearchParams(prev)
      p.set('tab', next)
      return p
    })
  }

  const toggleWatch = useCallback(async (event: RaceEvent) => {
    setBusyId(event.id)
    try {
      if (watchByEvent.has(event.id)) {
        await deleteWatch(event.id)
        setWatches(ws => ws.filter(w => w.event_id !== event.id))
      } else {
        const { watch } = await setWatch(event.id, 'watching', '')
        setWatches(ws => [...ws, watch])
      }
    } catch {
      setError(t('errors.save'))
    } finally {
      setBusyId(null)
    }
  }, [watchByEvent, t])

  return (
    <RatesContext.Provider value={rates}>
    <div className="mx-auto max-w-4xl p-4 md:p-8">
      <header className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold">{t('title')}</h1>
          <p className="mt-1 text-sm text-gray-400">{t('subtitle')}</p>
        </div>
        {user?.is_admin && (
          <button
            type="button"
            onClick={() => setCreating(true)}
            className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer"
          >
            <Plus size={16} /> {t('admin.newRace')}
          </button>
        )}
      </header>

      {creating && (
        <RaceEditor
          onCancel={() => setCreating(false)}
          onSaved={() => { setCreating(false); reload() }}
        />
      )}

      <div role="tablist" aria-label={t('title')} className="sticky top-14 md:top-0 z-20 -mx-4 flex gap-1 overflow-x-auto border-b border-gray-800 bg-gray-900/95 px-4 backdrop-blur-sm md:-mx-8 md:px-8">
        {tabs.map(k => {
          const Icon = TAB_ICONS[k]
          return (
            <button
              key={k}
              role="tab"
              id={`races-tab-${k}`}
              aria-selected={tab === k}
              aria-controls={`races-panel-${k}`}
              onClick={() => changeTab(k)}
              className={`flex shrink-0 items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium transition-colors cursor-pointer ${
                tab === k ? 'border-blue-500 text-white' : 'border-transparent text-gray-400 hover:text-gray-200'
              }`}
            >
              <Icon size={16} />
              {t(`tabs.${k}`)}
              {k === 'mine' && watches.length > 0 && <span className="text-xs tabular-nums text-gray-500">{watches.length}</span>}
            </button>
          )
        })}
      </div>

      {error && <div role="alert" className="mt-4 rounded-lg border border-red-700 bg-red-900/50 p-3 text-sm text-red-200">{error}</div>}

      {loading ? (
        <div className="flex h-32 items-center justify-center" aria-label={t('loading')}>
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-600 border-t-blue-500" />
        </div>
      ) : (
        <div role="tabpanel" id={`races-panel-${tab}`} aria-labelledby={`races-tab-${tab}`} className="pt-4">
          {tab === 'mine' && <MyRaces events={events} watchByEvent={watchByEvent} onDiscover={() => changeTab('discover')} />}
          {tab === 'discover' && (
            <Discover events={events} watchByEvent={watchByEvent} onToggleWatch={toggleWatch} busyId={busyId} />
          )}
          {tab === 'season' && <SeasonPlanner events={events} watches={watches} />}
          {tab === 'deadlines' && <Deadlines events={events} watchByEvent={watchByEvent} />}
          {tab === 'research' && admin && <ResearchLogPanel />}
        </div>
      )}
      <p className="mt-8 text-xs text-gray-500">{t('disclaimer')} {t('nokNote')}</p>
    </div>
    </RatesContext.Provider>
  )
}

function MyRaces({ events, watchByEvent, onDiscover }: {
  events: RaceEvent[]
  watchByEvent: Map<number, Watch>
  onDiscover: () => void
}) {
  const { t, deadlineWhen, daysLeft, lang } = useRaceFormat()
  const mine = events.filter(e => watchByEvent.has(e.id))

  if (mine.length === 0) {
    return (
      <div className="rounded-lg border border-gray-800 p-6 text-center">
        <p className="font-medium">{t('mine.empty')}</p>
        <p className="mt-1 text-sm text-gray-400">{t('mine.emptyHint')}</p>
        <button type="button" onClick={onDiscover} className="mt-4 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer">
          {t('tabs.discover')}
        </button>
      </div>
    )
  }

  const lanes = WATCH_STATES
    .map(state => ({ state, items: mine.filter(e => watchByEvent.get(e.id)?.state === state) }))
    .filter(l => l.items.length > 0)

  return (
    <div className="space-y-6">
      {lanes.map(({ state, items }) => (
        <section key={state} aria-labelledby={`lane-${state}`}>
          <h2 id={`lane-${state}`} className="mb-2 text-sm font-semibold uppercase tracking-wide text-gray-400">
            {t(`watchState.${state as WatchState}`)} <span className="tabular-nums text-gray-600">{items.length}</span>
          </h2>
          <ul className="grid gap-2 sm:grid-cols-2">
            {items.map(e => {
              const next = nextDeadline(e)
              return (
                <li key={e.id}>
                  <Link to={`/races/${e.id}`} className="block rounded-lg border border-gray-800 bg-gray-800/40 p-3 hover:border-gray-600">
                    <div className="flex items-start justify-between gap-2">
                      <span className="font-semibold leading-tight">{e.name}</span>
                      <StatusPill status={e.status} />
                    </div>
                    <p className="mt-0.5 text-sm text-gray-400">
                      {pickText(e.texts, lang)?.place} · {new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }).format(new Date(e.race_date + 'T12:00:00Z'))}
                    </p>
                    <p className="mt-2 text-sm">
                      {next ? (
                        <>
                          <span className="font-medium text-amber-300">{t(`kind.${next.kind}`)}</span>{' '}
                          <span className="text-gray-300">{deadlineWhen(next)}</span>{' '}
                          <span className="text-xs text-gray-500">({daysLeft(next.due_date)})</span>
                        </>
                      ) : (
                        <span className="text-gray-500">{t('mine.noDeadline')}</span>
                      )}
                    </p>
                  </Link>
                </li>
              )
            })}
          </ul>
        </section>
      ))}
    </div>
  )
}

function Discover({ events, watchByEvent, onToggleWatch, busyId }: {
  events: RaceEvent[]
  watchByEvent: Map<number, Watch>
  onToggleWatch: (e: RaceEvent) => void
  busyId: number | null
}) {
  const { t, lang } = useRaceFormat()
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [distance, setDistance] = useState<DistanceFilter>('all')
  const [series, setSeries] = useState<SeriesFilter>('all')
  const [directOnly, setDirectOnly] = useState(false)
  const [upcomingOnly, setUpcomingOnly] = useState(true)

  const today = new Date().toISOString().slice(0, 10)

  // Everything except the status filter, so the status chips show live counts.
  const base = useMemo(() => {
    return events.filter(e => {
      if (upcomingOnly && e.race_date < today) return false
      if (distance !== 'all' && distanceKind(e.distance_m) !== distance) return false
      if (series !== 'all' && !e.series.includes(series)) return false
      if (directOnly && e.travel !== 'direct') return false
      return matchesQuery(e, query, distanceKind(e.distance_m))
    })
  }, [events, query, distance, series, directOnly, upcomingOnly, today])

  const counts = useMemo(() => {
    const c: Record<StatusFilter, number> = { all: base.length, open: 0, later: 0, closed: 0 }
    base.forEach(e => { c[e.status]++ })
    return c
  }, [base])

  const shown = status === 'all' ? base : base.filter(e => e.status === status)

  const months = useMemo(() => {
    const groups: { key: string; label: string; items: RaceEvent[] }[] = []
    const fmt = new Intl.DateTimeFormat(intlLocale(lang), { month: 'long', year: 'numeric', timeZone: 'UTC' })
    for (const e of shown) {
      const key = e.race_date.slice(0, 7)
      let g = groups[groups.length - 1]
      if (!g || g.key !== key) {
        g = { key, label: fmt.format(new Date(key + '-15T12:00:00Z')), items: [] }
        groups.push(g)
      }
      g.items.push(e)
    }
    return groups
  }, [shown, lang])

  const chip = (pressed: boolean) =>
    `rounded-full border px-3 py-1.5 text-sm font-medium transition-colors cursor-pointer ${
      pressed ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-700 bg-gray-800 text-gray-200 hover:bg-gray-700'
    }`

  return (
    <div>
      <div className="relative mb-3">
        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" aria-hidden="true" />
        <input
          type="search"
          value={query}
          onChange={e => setQuery(e.target.value)}
          placeholder={t('filters.search')}
          aria-label={t('filters.search')}
          className="w-full rounded-lg border border-gray-700 bg-gray-800 py-2 pl-9 pr-3 text-sm placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      <div className="flex flex-wrap gap-2" role="group" aria-label={t('filters.status')}>
        {(['all', 'open', 'later', 'closed'] as StatusFilter[]).map(s => (
          <button key={s} type="button" aria-pressed={status === s} onClick={() => setStatus(s)} className={chip(status === s)}>
            {s === 'all' ? t('filters.all') : t(`status.${s}`)}
            <span className="ml-1.5 text-xs tabular-nums opacity-75">{counts[s]}</span>
          </button>
        ))}
      </div>
      <div className="mt-2 flex flex-wrap gap-2" role="group" aria-label={t('filters.more')}>
        {(['all', 'half', 'marathon'] as DistanceFilter[]).map(d => (
          <button key={d} type="button" aria-pressed={distance === d} onClick={() => setDistance(d)} className={chip(distance === d)}>
            {t(`distance.${d}`)}
          </button>
        ))}
        {(['majors', 'emc'] as const).map(s => (
          <button key={s} type="button" aria-pressed={series === s} onClick={() => setSeries(series === s ? 'all' : s)} className={chip(series === s)}>
            {t(`series.${s}`)}
          </button>
        ))}
        <button type="button" aria-pressed={directOnly} onClick={() => setDirectOnly(v => !v)} className={chip(directOnly)}>
          {t('filters.directOnly')}
        </button>
        <button type="button" aria-pressed={upcomingOnly} onClick={() => setUpcomingOnly(v => !v)} className={chip(upcomingOnly)}>
          {t('filters.upcomingOnly')}
        </button>
      </div>

      {months.length === 0 ? (
        <p className="mt-6 border-t border-gray-800 py-4 text-gray-500">{t('filters.empty')}</p>
      ) : (
        months.map(g => (
          <section key={g.key} className="mt-6" aria-labelledby={`month-${g.key}`}>
            <h2 id={`month-${g.key}`} className="text-xl font-bold capitalize">
              {g.label} <span className="text-sm font-normal tabular-nums text-gray-500">{g.items.length}</span>
            </h2>
            {g.items.map(e => (
              <RaceRow key={e.id} event={e} watch={watchByEvent.get(e.id)} onToggleWatch={onToggleWatch} busy={busyId === e.id} />
            ))}
          </section>
        ))
      )}
    </div>
  )
}

function Deadlines({ events, watchByEvent }: { events: RaceEvent[]; watchByEvent: Map<number, Watch> }) {
  const { t, lang, deadlineWhen, daysLeft } = useRaceFormat()
  const priced = usePriceText(lang)
  const [onlyMine, setOnlyMine] = useState(watchByEvent.size > 0)
  // Captured once per visit: deadlines passing while the page is open can wait for the next load.
  const [now] = useState(() => Date.now())

  const items = useMemo(() => {
    const list: { d: Deadline; e: RaceEvent; header: boolean }[] = []
    for (const e of events) {
      if (onlyMine && !watchByEvent.has(e.id)) continue
      for (const d of e.deadlines) {
        if (deadlineMoment(d) >= now) list.push({ d, e, header: false })
      }
    }
    list.sort((a, b) => deadlineMoment(a.d) - deadlineMoment(b.d))
    // A month heading goes above the first deadline of each month.
    list.forEach((item, i) => {
      item.header = i === 0 || item.d.due_date.slice(0, 7) !== list[i - 1].d.due_date.slice(0, 7)
    })
    return list
  }, [events, watchByEvent, onlyMine, now])

  const fmtMonth = new Intl.DateTimeFormat(intlLocale(lang), { month: 'long', year: 'numeric', timeZone: 'UTC' })

  return (
    <div>
      <div className="flex flex-wrap gap-2" role="group" aria-label={t('deadlines.scope')}>
        {[true, false].map(v => (
          <button
            key={String(v)}
            type="button"
            aria-pressed={onlyMine === v}
            onClick={() => setOnlyMine(v)}
            className={`rounded-full border px-3 py-1.5 text-sm font-medium cursor-pointer ${
              onlyMine === v ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-700 bg-gray-800 text-gray-200 hover:bg-gray-700'
            }`}
          >
            {v ? t('deadlines.onlyMine') : t('deadlines.all')}
          </button>
        ))}
      </div>

      {items.length === 0 ? (
        <p className="mt-6 border-t border-gray-800 py-4 text-gray-500">{onlyMine ? t('deadlines.emptyMine') : t('deadlines.empty')}</p>
      ) : (
        <ul className="mt-4">
          {items.map(({ d, e, header }) => {
            const month = d.due_date.slice(0, 7)
            const what = pickText(d.texts, lang)?.what
            return (
              <li key={d.id}>
                {header && (
                  <h2 className="mt-6 mb-1 text-lg font-bold capitalize first:mt-0">{fmtMonth.format(new Date(month + '-15T12:00:00Z'))}</h2>
                )}
                <div className="grid grid-cols-1 gap-1 border-t border-gray-800 py-3 sm:grid-cols-[11rem_minmax(0,1fr)] sm:gap-4">
                  <div className="text-sm">
                    <span className="font-semibold tabular-nums">{deadlineWhen(d)}</span>
                    <span className="block text-xs text-gray-500">
                      {daysLeft(d.due_date)}
                      {d.expected && <span className="ml-2 font-semibold uppercase tracking-wide text-amber-400">{t('expected')}</span>}
                    </span>
                  </div>
                  <div className="min-w-0 text-sm">
                    <span className="font-medium text-amber-300">{t(`kind.${d.kind}`)}</span>{' · '}
                    <Link to={`/races/${e.id}`} className="font-semibold hover:text-blue-300">{e.name}</Link>
                    {what && <p className="mt-0.5 text-gray-300">{priced(what, e)}</p>}
                  </div>
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
