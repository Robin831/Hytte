import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router'
import { AlertTriangle, Check, ExternalLink, X } from 'lucide-react'
import { useAuth } from '../../auth'
import { type RaceEvent, type StrideRace, type Watch, formatDuration, intlLocale, pickText } from './racesApi'
import { buildSeason, checkQualifying, formatMargin } from './season'
import { QUALIFYING_RESEARCHED_AT, QUALIFYING_STANDARDS } from './qualifying'
import { StatusPill } from './RaceParts'
import { useRaceFormat } from './useRaceFormat'
import { LedgerSection, SeriesSection } from './Phase5'

interface Prediction {
  distance_m: number
  time_seconds: number
}

/** Fetches JSON, resolving to null on any failure (these extras are optional). */
async function optionalJSON<T>(url: string, signal: AbortSignal): Promise<T | null> {
  try {
    const res = await fetch(url, { credentials: 'include', signal })
    return res.ok ? ((await res.json()) as T) : null
  } catch {
    return null
  }
}

/**
 * The season at a glance: upcoming races the user is committed to or
 * considering, with spacing warnings, and how their predicted marathon
 * compares with qualifying standards.
 */
export function SeasonPlanner({ events, watches }: { events: RaceEvent[]; watches: Watch[] }) {
  const { t, lang } = useRaceFormat()
  const { hasFeature } = useAuth()
  const [strideRaces, setStrideRaces] = useState<StrideRace[]>([])
  const [prediction, setPrediction] = useState<{ seconds: number; asOf: string } | null>(null)
  const [profile, setProfile] = useState<{ birthYear: number | null; sex: 'male' | 'female' | null }>({ birthYear: null, sex: null })
  const [today] = useState(() => new Date().toISOString().slice(0, 10))

  const hasStride = hasFeature('stride')
  const hasTraining = hasFeature('training')

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      const [stride, pred, prefs] = await Promise.all([
        hasStride ? optionalJSON<{ races: StrideRace[] }>('/api/stride/races', controller.signal) : null,
        hasTraining ? optionalJSON<{ as_of?: string; predictions?: Prediction[] | null }>('/api/training/predictions', controller.signal) : null,
        optionalJSON<{ preferences?: Record<string, string> }>('/api/settings/preferences', controller.signal),
      ])
      if (controller.signal.aborted) return
      setStrideRaces(stride?.races ?? [])
      const marathon = pred?.predictions?.find(p => Math.abs(p.distance_m - 42195) < 300)
      setPrediction(marathon ? { seconds: marathon.time_seconds, asOf: pred?.as_of ?? '' } : null)
      const p = prefs?.preferences ?? {}
      const year = Number(p.athlete_birth_year)
      setProfile({
        birthYear: Number.isInteger(year) && year > 1900 ? year : null,
        sex: p.athlete_sex === 'male' || p.athlete_sex === 'female' ? p.athlete_sex : null,
      })
    })()
    return () => controller.abort()
  }, [hasStride, hasTraining])

  const season = useMemo(() => buildSeason(events, watches, strideRaces, today), [events, watches, strideRaces, today])
  const results = useMemo(
    () => checkQualifying(QUALIFYING_STANDARDS, profile.birthYear, profile.sex, prediction?.seconds ?? null),
    [profile, prediction],
  )
  const slugToId = useMemo(() => new Map(events.map(e => [e.slug, e.id])), [events])
  const dateFmt = new Intl.DateTimeFormat(intlLocale(lang), { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })

  return (
    <div className="space-y-10">
      <section aria-labelledby="season-timeline">
        <h2 id="season-timeline" className="text-xl font-bold">{t('season.title')}</h2>
        <p className="mt-1 text-sm text-gray-400">{t('season.intro')}</p>
        {season.length === 0 ? (
          <p className="mt-4 rounded-lg border border-gray-800 p-4 text-sm text-gray-400">{t('season.empty')}</p>
        ) : (
          <ol className="mt-4">
            {season.map((item, i) => {
              const text = pickText(item.event.texts, lang)
              return (
                <li key={item.event.id}>
                  {i > 0 && item.weeksSincePrevious !== undefined && (
                    <div className="ml-5 border-l-2 border-gray-800 py-1 pl-5 text-xs text-gray-500">
                      {t('season.weeksBetween', { count: item.weeksSincePrevious })}
                    </div>
                  )}
                  <div className={`rounded-lg border p-3 ${item.committed ? 'border-gray-700 bg-gray-800/50' : 'border-dashed border-gray-700'}`}>
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div className="min-w-0">
                        <Link to={`/races/${item.event.id}`} className="font-semibold hover:text-blue-300">{item.event.name}</Link>
                        <p className="text-sm text-gray-400">
                          {dateFmt.format(new Date(item.event.race_date + 'T12:00:00Z'))} · {text?.place}
                        </p>
                      </div>
                      <div className="flex items-center gap-2 text-xs">
                        {item.priority && (
                          <span title={t(`stride.priority${item.priority}`)} className="rounded-full bg-green-900/40 px-2 py-0.5 font-semibold text-green-300">
                            {t('season.priority', { priority: item.priority })}
                          </span>
                        )}
                        <span className="rounded-full bg-blue-900/50 px-2 py-0.5 text-blue-200">{t(`watchState.${item.watch.state}`)}</span>
                        <StatusPill status={item.event.status} />
                      </div>
                    </div>
                    {item.warnings.length > 0 && (
                      <ul className="mt-2 space-y-1 text-sm text-amber-300">
                        {item.warnings.map(w => (
                          <li key={w} className="flex items-start gap-1.5">
                            <AlertTriangle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
                            {t(`season.warning.${w}`)}
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                </li>
              )
            })}
          </ol>
        )}
        <p className="mt-3 text-xs text-gray-500">{t('season.legend')}</p>
      </section>

      <section aria-labelledby="season-qualifying">
        <h2 id="season-qualifying" className="text-xl font-bold">{t('qualifying.title')}</h2>
        <p className="mt-1 text-sm text-gray-400">
          {prediction
            ? t('qualifying.basedOn', { time: formatDuration(prediction.seconds) })
            : hasTraining ? t('qualifying.noPrediction') : t('qualifying.noTraining')}
        </p>
        {(!profile.birthYear || !profile.sex) && (
          <p className="mt-2 text-sm text-amber-300">
            {t('qualifying.needProfile')} <Link to="/settings#races" className="text-blue-400 hover:text-blue-300">{t('qualifying.openSettings')}</Link>
          </p>
        )}
        <ul className="mt-4">
          {results.map(({ standard, group, age, margin }) => {
            const s = standard as (typeof QUALIFYING_STANDARDS)[number]
            const id = slugToId.get(s.slug)
            const inside = margin !== undefined && margin <= 0
            return (
              <li key={s.race} className="border-t border-gray-800 py-3">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span className="font-semibold">
                    {id ? <Link to={`/races/${id}`} className="hover:text-blue-300">{s.name}</Link> : s.name}
                    <span className="ml-2 text-xs font-normal text-gray-500">
                      {t(`qualifying.kind.${s.kind}`)} · {s.edition}{!s.verified && ` · ${t('qualifying.unverified')}`}
                    </span>
                  </span>
                  {group && (
                    <span className="text-sm tabular-nums">
                      {t('qualifying.standard', { time: formatDuration(group.seconds), age: age ?? '' })}
                      {margin !== undefined && (
                        <span className={`ml-2 inline-flex items-center gap-1 font-semibold ${inside ? 'text-green-400' : 'text-gray-300'}`}>
                          {inside ? <Check size={14} aria-hidden="true" /> : <X size={14} aria-hidden="true" />}
                          {formatMargin(margin)}
                        </span>
                      )}
                    </span>
                  )}
                </div>
                <p className="mt-1 text-sm text-gray-400">{s.note[lang] ?? s.note.en}</p>
                {s.groups.length === 0 && <p className="mt-1 text-xs text-gray-500">{t('qualifying.none')}</p>}
                <a href={s.source} target="_blank" rel="noopener noreferrer" className="mt-1 inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300">
                  {t('qualifying.source')} <ExternalLink size={10} aria-hidden="true" />
                </a>
              </li>
            )
          })}
        </ul>
        <p className="mt-3 text-xs text-gray-500">{t('qualifying.disclaimer', { date: QUALIFYING_RESEARCHED_AT })}</p>
      </section>

      <SeriesSection />
      <LedgerSection />
    </div>
  )
}
