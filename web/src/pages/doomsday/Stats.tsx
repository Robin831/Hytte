import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { parseIso, type Weekday } from './doomsday'
import { dateText, weekdayName } from './format'

interface LevelStats {
  level: number
  attempts: number
  correct: number
  accuracy: number
  median_ms: number
  best_ms: number
  streak: number
  best_streak: number
}

interface Attempt {
  level: number
  drill: string
  date: string
  answer: number
  response_ms: number
  correct: boolean
  slip: string
}

export interface StatsData {
  levels: LevelStats[]
  slips: Record<string, number>
  recent: Attempt[]
  today: { attempts: number; correct: number }
  daily: { day: string; attempts: number; correct: number }[]
}

const secs = (ms: number) => (ms > 0 ? `${(ms / 1000).toFixed(1)} s` : '—')

export default function Stats({ version }: { version: number }) {
  const { t, i18n } = useTranslation('doomsday')
  const [data, setData] = useState<StatsData | null>(null)
  const [error, setError] = useState(false)

  useEffect(() => {
    const c = new AbortController()
    fetch('/api/math/doomsday/stats', { credentials: 'include', signal: c.signal })
      .then(r => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
      .then((d: StatsData) => { setData(d); setError(false) })
      .catch(() => { if (!c.signal.aborted) setError(true) })
    return () => c.abort()
  }, [version])

  if (error) return <p role="alert" className="text-sm text-red-300">{t('errors.load')}</p>
  if (!data) return null
  if (data.levels.length === 0) return <p className="text-sm text-gray-400">{t('stats.empty')}</p>

  const slipLabel = (s: string) => {
    switch (s) {
      case 'leap': return t('practice.slip.leap')
      case 'anchor': return t('practice.slip.anchor')
      case 'firstEleven': return t('practice.slip.firstEleven')
      case 'secondEleven': return t('practice.slip.secondEleven')
      case 'noSubtract': return t('practice.slip.noSubtract')
      case 'month': return t('practice.slip.month')
      case 'offByOne': return t('practice.slip.offByOne')
      case 'timeout': return t('practice.slip.timeout')
      default: return t('practice.slip.unknown')
    }
  }
  const slips = Object.entries(data.slips).sort((a, b) => b[1] - a[1])
  const maxDaily = Math.max(1, ...data.daily.map(d => d.attempts))

  return (
    <div className="space-y-5">
      <p className="text-sm text-gray-300">{t('stats.todayLine', { correct: data.today.correct, attempts: data.today.attempts })}</p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-gray-400">
              <th className="font-normal py-1 pr-3">{t('stats.level')}</th>
              <th className="font-normal py-1 pr-3">{t('stats.attempts')}</th>
              <th className="font-normal py-1 pr-3">{t('stats.accuracy')}</th>
              <th className="font-normal py-1 pr-3">{t('stats.median')}</th>
              <th className="font-normal py-1 pr-3">{t('stats.best')}</th>
              <th className="font-normal py-1">{t('stats.streak')}</th>
            </tr>
          </thead>
          <tbody>
            {data.levels.map(l => (
              <tr key={l.level} className="border-t border-gray-800 tabular-nums">
                <td className="py-1.5 pr-3">{l.level}</td>
                <td className="py-1.5 pr-3">{l.attempts}</td>
                <td className="py-1.5 pr-3">{Math.round(l.accuracy * 100)} %</td>
                <td className="py-1.5 pr-3">{secs(l.median_ms)}</td>
                <td className="py-1.5 pr-3">{secs(l.best_ms)}</td>
                <td className="py-1.5">{l.streak} ({l.best_streak})</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <section>
        <h3 className="text-sm font-semibold mb-1">{t('stats.slipsTitle')}</h3>
        {slips.length === 0 ? <p className="text-sm text-gray-400">{t('stats.noSlips')}</p> : (
          <ul className="space-y-1 text-sm">
            {slips.map(([s, n]) => (
              <li key={s} className="flex gap-2"><span className="tabular-nums text-amber-300 w-6 shrink-0">{n}×</span><span className="text-gray-300">{slipLabel(s)}</span></li>
            ))}
          </ul>
        )}
      </section>

      {data.daily.length > 0 && (
        <section>
          <h3 className="text-sm font-semibold mb-1">{t('stats.dailyTitle')}</h3>
          <div className="flex items-end gap-1 h-16">
            {data.daily.map(d => (
              <div key={d.day} title={`${d.day}: ${d.correct}/${d.attempts}`} className="flex-1 max-w-4 flex flex-col justify-end h-full">
                <div className="bg-red-800/70" style={{ height: `${((d.attempts - d.correct) / maxDaily) * 100}%` }} />
                <div className="bg-emerald-600" style={{ height: `${(d.correct / maxDaily) * 100}%` }} />
              </div>
            ))}
          </div>
        </section>
      )}

      <section>
        <h3 className="text-sm font-semibold mb-1">{t('stats.recentTitle')}</h3>
        <ul className="text-sm divide-y divide-gray-800">
          {data.recent.map((a, i) => {
            const date = parseIso(a.date)
            return (
              <li key={i} className="flex flex-wrap gap-x-3 py-1">
                <span className={a.correct ? 'text-emerald-400' : 'text-red-400'}>{a.correct ? '✓' : '✗'}</span>
                <span className="text-gray-400 tabular-nums">{a.level}{a.drill ? `·${a.drill}` : ''}</span>
                <span>{date && !a.drill ? dateText(date, i18n.language) : ''}</span>
                {!a.drill && a.answer >= 0 && <span className="text-gray-400">{weekdayName(a.answer as Weekday, i18n.language)}</span>}
                <span className="text-gray-500 tabular-nums">{secs(a.response_ms)}</span>
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  )
}
