import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, ExternalLink, Loader2, Search, Star } from 'lucide-react'
import {
  type FlightOption, type HotelOption, type PriceView, type Quote, type Trip, fetchPrices, formatDate, intlLocale, searchPrices, toLang, txt,
} from './tripsApi'

const POLL_MS = 3000

function useNok() {
  const { i18n } = useTranslation('trips')
  const fmt = new Intl.NumberFormat(intlLocale(toLang(i18n.language)), { style: 'currency', currency: 'NOK', maximumFractionDigits: 0 })
  return (n: number | null | undefined) => (n == null ? '—' : fmt.format(n))
}

function LevelBadge({ q }: { q?: Quote }) {
  const { t } = useTranslation('trips')
  const nok = useNok()
  if (!q?.price_level) return null
  const cls = q.price_level === 'low' ? 'bg-green-900/60 text-green-200' : q.price_level === 'high' ? 'bg-red-900/60 text-red-200' : 'bg-gray-700 text-gray-200'
  const range = q.typical_low != null && q.typical_high != null ? t('prices.typicalRange', { low: nok(q.typical_low), high: nok(q.typical_high) }) : undefined
  return <span title={range} className={`ml-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase ${cls}`}>{q.price_level === 'low' || q.price_level === 'typical' || q.price_level === 'high' ? t(`prices.level.${q.price_level}`) : q.price_level}</span>
}

function options<T>(q?: Quote): T[] {
  if (!q || !Array.isArray(q.options)) return []
  return q.options as T[]
}

interface PriceRow { start: string; end: string; flights?: Quote; hotels?: Quote }

/** Flights plus hotel, when both are priced (or there is no hotel search). */
function rowTotal(r: PriceRow): number | null {
  return r.flights?.cheapest != null && (r.hotels?.cheapest != null || !r.hotels) ? r.flights.cheapest + (r.hotels?.cheapest ?? 0) : null
}

function hm(min: number) {
  return `${Math.floor(min / 60)}:${String(min % 60).padStart(2, '0')}`
}

/** Flight and hotel prices for the trip's dates (Google via SerpApi). */
export function TripPrices({ trip }: { trip: Trip }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const nok = useNok()
  const [view, setView] = useState<PriceView | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [version, setVersion] = useState(0)
  const pending = !!view?.quotes.some(q => q.status === 'pending')

  useEffect(() => {
    const controller = new AbortController()
    const load = () => fetchPrices(trip.id, controller.signal).then(setView).catch(() => {})
    load()
    const timer = pending ? setInterval(load, POLL_MS) : undefined
    return () => { controller.abort(); if (timer) clearInterval(timer) }
  }, [trip.id, trip.updated_at, pending, version])

  const rows = useMemo(() => {
    const by = new Map<string, PriceRow>()
    for (const q of view?.quotes ?? []) {
      const key = `${q.start}_${q.end}`
      const r = by.get(key) ?? { start: q.start, end: q.end }
      if (q.kind === 'flights') r.flights = q
      else r.hotels = q
      by.set(key, r)
    }
    return [...by.values()].sort((a, b) => a.start.localeCompare(b.start))
  }, [view])
  const cheapestKey = useMemo(() => {
    let best: string | null = null
    let bestTotal = Infinity
    for (const r of rows) {
      const tt = rowTotal(r)
      if (tt != null && tt < bestTotal) { bestTotal = tt; best = `${r.start}_${r.end}` }
    }
    return rows.length > 1 ? best : null
  }, [rows])

  const search = async () => {
    setBusy(true)
    setError('')
    try {
      await searchPrices(trip.id)
      setVersion(v => v + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.save'))
    } finally {
      setBusy(false)
    }
  }

  const dest = trip.doc.destination
  const px = view?.passengers
  const pxText = px ? [
    t('prices.adults', { count: px.adults }),
    ...(px.child_ages?.length ? [t('prices.children', { ages: px.child_ages.join(', ') })] : []),
    ...(px.infants ? [t('prices.infants', { count: px.infants })] : []),
  ].join(' · ') : ''

  if (!dest || dest.airports.length === 0) {
    return trip.can_edit ? <p className="text-sm text-gray-400">{t('prices.noDestination')}</p> : null
  }

  return (
    <div>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <p className="text-sm text-gray-400">
          {view?.home ?? 'BGO'} → {dest.airports.join('/')}{dest.city && ` · ${dest.city}`}{pxText && ` · ${pxText}`}
        </p>
        {trip.can_edit && view?.quota.ready && (
          <button type="button" onClick={search} disabled={busy || pending}
            className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
            {busy || pending ? <Loader2 size={14} className="animate-spin" aria-hidden="true" /> : <Search size={14} aria-hidden="true" />}
            {pending ? t('prices.searching') : t('prices.check')}
          </button>
        )}
      </div>
      {view && <p className="mt-1 text-xs text-gray-500">{t('prices.quota', { used: view.quota.used, limit: view.quota.limit })}</p>}
      {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}
      {view && rows.length === 0 && <p className="mt-3 text-sm text-gray-400">{t('prices.none')}</p>}

      {rows.length > 0 && (
        <ul className="mt-3 divide-y divide-gray-800 rounded-lg border border-gray-800">
          {rows.map(r => {
            const key = `${r.start}_${r.end}`
            const expanded = open === key
            const best = cheapestKey === key
            return (
              <li key={key} className={best ? 'bg-green-950/20' : ''}>
                <button type="button" onClick={() => setOpen(expanded ? null : key)} aria-expanded={expanded}
                  className="grid w-full grid-cols-[1.25rem_minmax(0,1fr)_auto] items-start gap-2 px-3 py-2.5 text-left text-sm cursor-pointer">
                  {expanded ? <ChevronDown size={16} className="mt-0.5" aria-hidden="true" /> : <ChevronRight size={16} className="mt-0.5" aria-hidden="true" />}
                  <span className="min-w-0">
                    <span className="font-semibold">{formatDate(r.start, lang)} – {formatDate(r.end, lang)}</span>
                    {best && <span className="ml-2 rounded-full bg-green-700 px-2 py-0.5 text-[10px] font-semibold uppercase">{t('prices.cheapest')}</span>}
                    <span className="mt-0.5 block text-xs text-gray-400">
                      {t('prices.flights')}: {r.flights?.status === 'pending' ? t('prices.searching') : r.flights?.status === 'failed' ? t('prices.failed') : nok(r.flights?.cheapest)}
                      <LevelBadge q={r.flights} />
                      {r.hotels && <> · {t('prices.hotel')}: {r.hotels.status === 'pending' ? t('prices.searching') : r.hotels.status === 'failed' ? t('prices.failed') : nok(r.hotels.cheapest)}</>}
                    </span>
                  </span>
                  <span className="text-right">
                    <span className="block font-semibold tabular-nums">{nok(rowTotal(r))}</span>
                    <span className="block text-[10px] text-gray-500">{t('prices.total')}</span>
                  </span>
                </button>
                {expanded && (
                  <div className="space-y-3 px-3 pb-3 pl-9 text-sm">
                    {r.flights && (
                      <div>
                        <p className="text-xs font-semibold uppercase tracking-wide text-gray-500">{t('prices.flightOptions')}</p>
                        <ul className="mt-1 space-y-1">
                          {options<FlightOption>(r.flights).map((o, i) => (
                            <li key={i} className="flex flex-wrap justify-between gap-x-3">
                              <span>{o.airlines.join(' + ')} · {o.legs[0]?.dep.slice(11)} → {o.legs[o.legs.length - 1]?.arr.slice(11)} · {o.stops === 0 ? t('prices.direct') : t('prices.stops', { count: o.stops })} · {hm(o.minutes)}</span>
                              <span className="tabular-nums">{nok(o.price)}</span>
                            </li>
                          ))}
                        </ul>
                        {r.flights.link && <a href={r.flights.link} target="_blank" rel="noopener noreferrer" className="mt-1 inline-flex items-center gap-1 text-xs text-blue-400">{t('prices.openFlights')} <ExternalLink size={11} /></a>}
                        <p className="text-[11px] text-gray-500">{t('prices.flightNote')}</p>
                      </div>
                    )}
                    {r.hotels && (
                      <div>
                        <p className="text-xs font-semibold uppercase tracking-wide text-gray-500">{t('prices.hotelOptions')}</p>
                        <ul className="mt-1 space-y-1">
                          {options<HotelOption>(r.hotels).map((h, i) => (
                            <li key={i} className="flex flex-wrap justify-between gap-x-3">
                              <span className="min-w-0">
                                {h.link ? <a href={h.link} target="_blank" rel="noopener noreferrer" className="hover:text-blue-300">{h.name}</a> : h.name}
                                {h.rating > 0 && <span className="ml-1 text-xs text-amber-300"><Star size={10} className="inline" aria-hidden="true" /> {h.rating}</span>}
                              </span>
                              <span className="tabular-nums">{nok(h.total)} <span className="text-xs text-gray-500">({t('prices.perNight', { price: nok(h.per_night) })})</span></span>
                            </li>
                          ))}
                        </ul>
                        {r.hotels.link && <a href={r.hotels.link} target="_blank" rel="noopener noreferrer" className="mt-1 inline-flex items-center gap-1 text-xs text-blue-400">{t('prices.openHotels')} <ExternalLink size={11} /></a>}
                      </div>
                    )}
                    <p className="text-[11px] text-gray-500">{t('prices.checkedAt', { when: new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date((r.flights ?? r.hotels)!.searched_at)) })}</p>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

/** The trip's costs, totalled in NOK. */
export function TripCosts({ trip }: { trip: Trip }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const nok = useNok()
  const cs = trip.cost_summary
  const costs = trip.doc.costs ?? []
  if (costs.length === 0) return null
  const money = (amount: number, cur: string) =>
    new Intl.NumberFormat(intlLocale(lang), { style: 'currency', currency: cur, maximumFractionDigits: 0 }).format(amount)
  return (
    <div>
      <div className="grid grid-cols-3 gap-2 text-center">
        <div className="rounded-lg bg-gray-800/60 p-2"><p className="text-xs text-gray-400">{t('costs.total')}</p><p className="font-semibold tabular-nums">{nok(cs.total_nok)}</p></div>
        <div className="rounded-lg bg-gray-800/60 p-2"><p className="text-xs text-gray-400">{t('costs.perPerson')}</p><p className="font-semibold tabular-nums">{nok(cs.per_person_nok)}</p></div>
        <div className="rounded-lg bg-gray-800/60 p-2"><p className="text-xs text-gray-400">{t('costs.booked')}</p><p className="font-semibold tabular-nums">{nok(cs.booked_nok)}</p></div>
      </div>
      <ul className="mt-3 divide-y divide-gray-800 text-sm">
        {costs.map((c, i) => (
          <li key={i} className="flex justify-between gap-3 py-1.5">
            <span className="min-w-0">{txt(c.label, lang) || t(`costs.category.${c.category}`)} <span className="text-xs text-gray-500">· {t(`costs.category.${c.category}`)} · {t(`costs.status.${c.status}`)}</span></span>
            <span className="tabular-nums">{money(c.amount, c.currency)}</span>
          </li>
        ))}
      </ul>
      {cs.unconverted && cs.unconverted.length > 0 && <p className="mt-1 text-xs text-amber-300">{t('costs.unconverted', { list: cs.unconverted.join(', ') })}</p>}
    </div>
  )
}
