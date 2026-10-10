import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { useTranslation } from 'react-i18next'
import { Plane, Plus } from 'lucide-react'
import {
  type Flex, type Person, type Traveller, type TripKind, type TripSummary, TRIP_KINDS, createTrip, dateIn, emptyDoc, fetchTrips,
  formatDate, toLang, txt,
} from './tripsApi'
import { DateFields, FamilyPanel, FamilyPicker } from './Family'
import { useFamily } from './useFamily'

const ZONES = ['Europe/Oslo', 'Europe/London', 'Europe/Amsterdam', 'Europe/Berlin', 'Europe/Paris', 'Europe/Madrid', 'Europe/Lisbon',
  'Europe/Rome', 'Europe/Prague', 'Europe/Copenhagen', 'Europe/Stockholm', 'Europe/Helsinki', 'Asia/Bangkok', 'America/New_York',
  'America/Chicago', 'Asia/Tokyo', 'Australia/Sydney']

function TripCard({ trip, now }: { trip: TripSummary; now: Date }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const today = dateIn(trip.dest_tz, now)
  const ongoing = trip.start_date <= today && today <= trip.end_date
  return (
    <Link to={`/trips/${trip.id}`}
      className={`block rounded-lg border p-4 hover:border-gray-500 ${ongoing ? 'border-blue-700 bg-blue-900/20' : 'border-gray-800 bg-gray-800/40'}`}>
      <div className="flex items-start justify-between gap-2">
        <span className="text-lg font-semibold leading-tight">{txt(trip.title, lang)}</span>
        {ongoing && <span className="rounded-full bg-blue-600 px-2 py-0.5 text-xs font-semibold">{t('now')}</span>}
      </div>
      <p className="mt-1 text-sm text-gray-400">
        {trip.tentative && <span className="mr-1 rounded bg-amber-900/50 px-1.5 py-0.5 text-xs text-amber-200">{t('dates.tentative')}</span>}
        {formatDate(trip.start_date, lang)} – {formatDate(trip.end_date, lang)} · {t(`kind.${trip.kind}`)}
      </p>
      {trip.route?.length > 0 && <p className="mt-1 font-mono text-xs text-gray-400">{trip.route.join(' → ')}</p>}
      <p className="mt-2 flex flex-wrap gap-x-3 text-xs text-gray-500">
        {trip.travellers && trip.travellers.length > 0 && <span>{trip.travellers.join(', ')}</span>}
        {trip.total > 0 && <span>{t('checklistProgress', { done: trip.total - trip.open, total: trip.total })}</span>}
      </p>
    </Link>
  )
}

function NewTrip({ onCancel, people }: { onCancel: () => void; people: Person[] }) {
  const { t, i18n } = useTranslation('trips')
  const lang = toLang(i18n.language)
  const navigate = useNavigate()
  const [title, setTitle] = useState('')
  const [kind, setKind] = useState<TripKind>('holiday')
  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [flex, setFlex] = useState<Flex | null>(null)
  const [destTZ, setDestTZ] = useState('Europe/Oslo')
  const [travellers, setTravellers] = useState<Traveller[]>([])
  const [error, setError] = useState('')
  const input = 'w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm'
  const label = 'mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400'

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    try {
      const doc = emptyDoc()
      doc.title = { [lang]: title.trim() }
      doc.travellers = travellers
      doc.flex = flex
      const res = await createTrip({ kind, start_date: start, end_date: end || start, home_tz: 'Europe/Oslo', dest_tz: destTZ,
        share_family: false, race_event_id: null, result_id: null, doc })
      navigate(`/trips/${res.id}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.save'))
    }
  }

  return (
    <form onSubmit={save} className="mb-6 space-y-3 rounded-lg border border-gray-700 bg-gray-800/50 p-4" aria-label={t('newTrip')}>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <label className={label} htmlFor="nt-title">{t('field.title')}</label>
          <input id="nt-title" className={input} value={title} required maxLength={120} onChange={e => setTitle(e.target.value)} />
        </div>
        <div>
          <label className={label} htmlFor="nt-kind">{t('field.kind')}</label>
          <select id="nt-kind" className={input} value={kind} onChange={e => setKind(e.target.value as TripKind)}>
            {TRIP_KINDS.map(k => <option key={k} value={k}>{t(`kind.${k}`)}</option>)}
          </select>
        </div>
        <div>
          <label className={label} htmlFor="nt-tz">{t('field.destTZ')}</label>
          <select id="nt-tz" className={input} value={destTZ} onChange={e => setDestTZ(e.target.value)}>
            {ZONES.map(z => <option key={z} value={z}>{z.replace('_', ' ')}</option>)}
          </select>
        </div>
        <div className="sm:col-span-2">
          <DateFields start={start} end={end} flex={flex} onChange={v => { setStart(v.start); setEnd(v.end); setFlex(v.flex) }} />
        </div>
        <div className="sm:col-span-2">
          <FamilyPicker people={people} value={travellers} onChange={setTravellers} date={start} />
        </div>
      </div>
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <div className="flex gap-2">
        <button type="submit" className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer">{t('create')}</button>
        <button type="button" onClick={onCancel} className="rounded-lg bg-gray-700 px-4 py-2 text-sm hover:bg-gray-600 cursor-pointer">{t('cancel')}</button>
      </div>
    </form>
  )
}

export default function TripsPage() {
  const { t } = useTranslation('trips')
  const [trips, setTrips] = useState<TripSummary[] | null>(null)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [now] = useState(() => new Date())
  const family = useFamily()

  useEffect(() => {
    const controller = new AbortController()
    fetchTrips(controller.signal)
      .then(d => setTrips(d.trips))
      .catch(() => { if (!controller.signal.aborted) setError(t('errors.load')) })
    return () => controller.abort()
  }, [t])

  const groups = useMemo(() => {
    const all = trips ?? []
    const planning = all.filter(tr => tr.tentative && tr.end_date >= dateIn(tr.dest_tz, now))
    const list = all.filter(tr => !planning.includes(tr))
    const ongoing = list.filter(tr => { const d = dateIn(tr.dest_tz, now); return tr.start_date <= d && d <= tr.end_date })
    const upcoming = list.filter(tr => tr.start_date > dateIn(tr.dest_tz, now)).sort((a, b) => a.start_date.localeCompare(b.start_date))
    const past = list.filter(tr => tr.end_date < dateIn(tr.dest_tz, now))
    return { planning, ongoing, upcoming, past }
  }, [trips, now])

  return (
    <div className="mx-auto max-w-4xl p-4 md:p-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-bold"><Plane size={22} aria-hidden="true" /> {t('title')}</h1>
          <p className="mt-1 text-sm text-gray-400">{t('subtitle')}</p>
        </div>
        <button type="button" onClick={() => setCreating(true)} className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer">
          <Plus size={16} /> {t('newTrip')}
        </button>
      </header>
      <FamilyPanel people={family.people} onChange={family.reload} />
      {creating && <NewTrip onCancel={() => setCreating(false)} people={family.people} />}
      {error && <p role="alert" className="mb-4 rounded-lg border border-red-700 bg-red-900/50 p-3 text-sm text-red-200">{error}</p>}
      {trips && trips.length === 0 && !creating && <p className="rounded-lg border border-gray-800 p-6 text-center text-gray-400">{t('empty')}</p>}
      {(['ongoing', 'planning', 'upcoming', 'past'] as const).map(g => groups[g].length > 0 && (
        <section key={g} className="mb-8" aria-labelledby={`trips-${g}`}>
          <h2 id={`trips-${g}`} className="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-400">{t(`group.${g}`)}</h2>
          <div className="grid gap-3 sm:grid-cols-2">{groups[g].map(tr => <TripCard key={tr.id} trip={tr} now={now} />)}</div>
        </section>
      ))}
    </div>
  )
}
