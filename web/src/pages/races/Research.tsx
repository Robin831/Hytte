import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router'
import { Loader2, RefreshCw, Sparkles } from 'lucide-react'
import { Bar, BarChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import {
  type ResearchLog, type ResearchRun, type ResearchSettings, type SpendStats,
  fetchRace, fetchResearchLog, intlLocale, saveResearchSettings, startDiscovery, startResearch,
} from './racesApi'
import { useRaceFormat } from './useRaceFormat'

const POLL_MS = 4000

/** Maps a research start failure (server message) to a translated line. */
function useStartError() {
  const { t } = useRaceFormat()
  return (err: unknown) => {
    const msg = err instanceof Error ? err.message : ''
    if (msg.includes('checked recently')) return t('research.cooldown')
    if (msg.includes("month's")) return t('research.monthly')
    if (msg.includes('turned off')) return t('research.disabled')
    if (msg.includes('budget')) return t('research.budget')
    if (msg.includes('already running')) return t('research.busy')
    if (msg.includes('Claude')) return t('research.noClaude')
    return t('research.failedStart')
  }
}

function useWhen() {
  const { lang } = useRaceFormat()
  return (iso: string) =>
    iso ? new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }).format(new Date(iso)) : ''
}

/**
 * The latest automatic check of a race and a button to ask for a new one.
 * Remount with a new key when the parent loads a different run.
 */
export function ResearchStatus({ eventId, run, onFinished }: {
  eventId: number
  run: ResearchRun | null
  onFinished: () => void
}) {
  const { t } = useRaceFormat()
  const when = useWhen()
  const startError = useStartError()
  const [current, setCurrent] = useState<ResearchRun | null>(run)
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)
  const running = current?.status === 'running'

  useEffect(() => {
    if (!running) return
    const timer = setInterval(async () => {
      try {
        const data = await fetchRace(eventId)
        if (data.research && data.research.status !== 'running') {
          setCurrent(data.research)
          onFinished()
        }
      } catch {
        // Keep polling; a transient failure shouldn't end the wait.
      }
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [running, eventId, onFinished])

  const start = async () => {
    setStarting(true)
    setError('')
    try {
      const { run: started } = await startResearch(eventId)
      setCurrent(started)
    } catch (err) {
      setError(startError(err))
    } finally {
      setStarting(false)
    }
  }

  let line = ''
  if (current && !running) {
    const at = when(current.finished_at || current.started_at)
    if (current.status === 'done') {
      line = current.changes > 0
        ? t('research.updated', { date: at, count: current.changes })
        : t('research.noChanges', { date: at })
    } else if (current.status === 'skipped') {
      line = t('research.unsure', { date: at })
    } else {
      line = t('research.failed', { date: at })
    }
  }

  return (
    <div className="mt-3 rounded-lg border border-gray-800 p-3 text-sm" aria-live="polite">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          {running ? (
            <span className="inline-flex items-center gap-2 text-gray-300">
              <Loader2 size={14} className="animate-spin" aria-hidden="true" /> {t('research.running')}
            </span>
          ) : line ? (
            <>
              <span className="text-gray-300">{line}</span>
              {current?.summary && current.status !== 'failed' && (
                <span className="mt-0.5 block text-xs text-gray-500">{current.summary}</span>
              )}
            </>
          ) : (
            <span className="text-gray-500">{t('research.never')}</span>
          )}
        </div>
        <button
          type="button"
          onClick={start}
          disabled={running || starting}
          className="inline-flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-1.5 text-sm hover:bg-gray-700 disabled:opacity-50 cursor-pointer"
        >
          <RefreshCw size={14} className={running ? 'animate-spin' : ''} aria-hidden="true" />
          {t('research.check')}
        </button>
      </div>
      {error && <p role="alert" className="mt-2 text-red-300">{error}</p>}
    </div>
  )
}

const usd = (n: number) => `$${n.toFixed(2)}`
const BAR_COLOR = '#3b82f6' // validated against the dark surface (dataviz validator)
const tooltipStyle = { backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: 8, color: '#f3f4f6', fontSize: 12 }

/** One spend figure; `of` adds the cap it counts against. */
function SpendTile({ label, value, of }: { label: string; value: number; of?: number }) {
  const pct = of && of > 0 ? Math.min(100, (value / of) * 100) : null
  return (
    <div className="rounded-lg border border-gray-800 bg-gray-800/40 p-3">
      <p className="text-xs font-semibold uppercase tracking-wide text-gray-500">{label}</p>
      <p className="mt-1 text-xl font-bold tabular-nums">
        {usd(value)}
        {of != null && <span className="ml-1 text-sm font-normal text-gray-500">/ {usd(of)}</span>}
      </p>
      {pct != null && (
        <div className="mt-2 h-1.5 rounded-full bg-gray-700" aria-hidden="true">
          <div className={`h-1.5 rounded-full ${pct >= 90 ? 'bg-amber-400' : 'bg-blue-500'}`} style={{ width: `${pct}%` }} />
        </div>
      )}
    </div>
  )
}

function SpendOverview({ stats, settings }: { stats: SpendStats; settings: ResearchSettings }) {
  const { t, lang } = useRaceFormat()
  const dayFmt = new Intl.DateTimeFormat(intlLocale(lang), { day: 'numeric', month: 'short', timeZone: 'UTC' })
  const data = stats.daily.map(d => ({ ...d, label: dayFmt.format(new Date(d.date + 'T12:00:00Z')) }))

  return (
    <section aria-labelledby="research-spend" className="mt-4">
      <h2 id="research-spend" className="text-lg font-bold">{t('research.spendTitle')}</h2>
      <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <SpendTile label={t('research.today')} value={stats.today_usd} of={settings.daily_budget_usd} />
        <SpendTile label={t('research.thisMonth')} value={stats.month_to_date_usd} of={settings.monthly_budget_usd} />
        <SpendTile label={t('research.last7')} value={stats.last_7_days_usd} />
        <SpendTile label={t('research.allTime')} value={stats.all_time_usd} />
      </div>
      <p className="mt-2 text-sm text-gray-400">
        {t('research.last30', {
          cost: usd(stats.last_30_days_usd), checks: stats.race_checks_30d, avg: usd(stats.avg_race_check_usd),
          discoveries: stats.discoveries_30d, changes: stats.changes_30d,
        })}
      </p>

      <h3 className="mt-5 text-sm font-semibold text-gray-300">{t('research.dailyChart')}</h3>
      <div className="mt-2 h-48" role="img" aria-label={t('research.dailyChart')}>
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#374151" vertical={false} />
            <XAxis dataKey="label" tick={{ fill: '#6b7280', fontSize: 10 }} interval={6} tickLine={false} axisLine={{ stroke: '#374151' }} />
            <YAxis tick={{ fill: '#6b7280', fontSize: 10 }} tickFormatter={(v: number) => `$${v.toFixed(v < 1 ? 2 : 0)}`} width={44} tickLine={false} axisLine={false} />
            <Tooltip
              contentStyle={tooltipStyle}
              cursor={{ fill: 'rgba(148,163,184,0.12)' }}
              labelFormatter={label => String(label)}
              formatter={(value, _name, item) => [
                `${usd(typeof value === 'number' ? value : 0)} · ${t('research.runs', { count: (item?.payload as { runs?: number })?.runs ?? 0 })}`,
                t('research.cost'),
              ]}
            />
            {settings.daily_budget_usd > 0 && (
              <ReferenceLine
                y={settings.daily_budget_usd}
                stroke="#9ca3af"
                strokeDasharray="4 4"
                label={{ value: t('research.dailyCap'), fill: '#9ca3af', fontSize: 10, position: 'insideTopRight' }}
              />
            )}
            <Bar dataKey="cost_usd" fill={BAR_COLOR} radius={[4, 4, 0, 0]} maxBarSize={14} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <details className="mt-2 text-sm text-gray-400">
        <summary className="cursor-pointer">{t('research.asTable')}</summary>
        <table className="mt-2 w-full text-left tabular-nums">
          <thead><tr className="text-xs text-gray-500"><th className="py-1">{t('research.date')}</th><th>{t('research.cost')}</th><th>{t('research.runsCol')}</th></tr></thead>
          <tbody>
            {[...data].reverse().filter(d => d.runs > 0).map(d => (
              <tr key={d.date} className="border-t border-gray-800"><td className="py-1">{d.label}</td><td>{usd(d.cost_usd)}</td><td>{d.runs}</td></tr>
            ))}
          </tbody>
        </table>
      </details>
    </section>
  )
}

function SettingsForm({ settings, models, onSaved }: {
  settings: ResearchSettings
  models: string[]
  onSaved: (s: ResearchSettings) => void
}) {
  const { t } = useRaceFormat()
  const [form, setForm] = useState(settings)
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState<'idle' | 'saved' | 'error'>('idle')
  const [error, setError] = useState('')
  const set = <K extends keyof ResearchSettings>(k: K, v: ResearchSettings[K]) => { setForm(f => ({ ...f, [k]: v })); setStatus('idle') }
  const input = 'w-full rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500'
  const label = 'mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400'

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError('')
    try {
      const res = await saveResearchSettings(form)
      setForm(res.settings)
      onSaved(res.settings)
      setStatus('saved')
    } catch (err) {
      setError(err instanceof Error ? err.message : '')
      setStatus('error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <section aria-labelledby="research-limits" className="mt-8">
      <h2 id="research-limits" className="text-lg font-bold">{t('research.limits')}</h2>
      <form onSubmit={save} className="mt-3 space-y-4 rounded-lg border border-gray-800 p-4">
        <div className="flex flex-wrap gap-6">
          <label className="inline-flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.enabled} onChange={e => set('enabled', e.target.checked)} />
            {t('research.enabled')}
          </label>
          <label className="inline-flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.discovery_enabled} onChange={e => set('discovery_enabled', e.target.checked)} />
            {t('research.discoveryEnabled')}
          </label>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <label className={label} htmlFor="rs-model">{t('research.model')}</label>
            <select id="rs-model" className={input} value={form.model} onChange={e => set('model', e.target.value)}>
              {models.map(m => <option key={m} value={m}>{m}</option>)}
            </select>
            <p className="mt-1 text-xs text-gray-500">{t('research.modelHint')}</p>
          </div>
          <div>
            <label className={label} htmlFor="rs-nightly">{t('research.nightly')}</label>
            <input id="rs-nightly" type="number" min={0} max={60} className={input} value={form.nightly_max_races}
              onChange={e => set('nightly_max_races', Number(e.target.value))} />
            <p className="mt-1 text-xs text-gray-500">{t('research.nightlyHint')}</p>
          </div>
          <div>
            <label className={label} htmlFor="rs-daily">{t('research.dailyBudget')}</label>
            <input id="rs-daily" type="number" min={0} max={100} step={0.5} className={input} value={form.daily_budget_usd}
              onChange={e => set('daily_budget_usd', Number(e.target.value))} />
          </div>
          <div>
            <label className={label} htmlFor="rs-monthly">{t('research.monthlyBudget')}</label>
            <input id="rs-monthly" type="number" min={0} max={1000} step={1} className={input} value={form.monthly_budget_usd}
              onChange={e => set('monthly_budget_usd', Number(e.target.value))} />
          </div>
        </div>
        <div className="flex items-center gap-3">
          <button type="submit" disabled={saving} className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
            {t('admin.save')}
          </button>
          {status === 'saved' && <span role="status" className="text-sm text-green-400">{t('watch.saved')}</span>}
          {status === 'error' && <span role="alert" className="text-sm text-red-300">{error || t('errors.save')}</span>}
        </div>
      </form>
    </section>
  )
}

/** Admin view: spend, limits, recent runs and a discovery trigger. */
export function ResearchLogPanel() {
  const { t } = useRaceFormat()
  const when = useWhen()
  const startError = useStartError()
  const [log, setLog] = useState<ResearchLog | null>(null)
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)

  const load = useCallback(async (signal?: AbortSignal) => {
    const data = await fetchResearchLog(signal)
    if (!signal?.aborted) setLog(data)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        await load(controller.signal)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.load'))
      }
    })()
    return () => controller.abort()
  }, [load, t])

  const anyRunning = !!log?.runs.some(r => r.status === 'running')
  useEffect(() => {
    if (!anyRunning) return
    const timer = setInterval(() => { load().catch(() => {}) }, POLL_MS)
    return () => clearInterval(timer)
  }, [anyRunning, load])

  const discover = async () => {
    setStarting(true)
    setError('')
    try {
      await startDiscovery()
      await load()
    } catch (err) {
      setError(startError(err))
    } finally {
      setStarting(false)
    }
  }

  return (
    <div>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="text-sm text-gray-300">
          <p>{t('research.about')}</p>
          {log && <p className="mt-1 text-gray-400">{log.settings.model}{!log.settings.enabled && <> · <span className="text-amber-300">{t('research.disabled')}</span></>}</p>}
          {log?.config_error && <p className="mt-1 text-amber-300">{t('research.noClaude')}</p>}
        </div>
        <button
          type="button"
          onClick={discover}
          disabled={starting || anyRunning}
          className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer"
        >
          <Sparkles size={14} aria-hidden="true" /> {t('research.discover')}
        </button>
      </div>
      {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}

      {log && <SpendOverview stats={log.stats} settings={log.settings} />}
      {log && (
        <SettingsForm
          settings={log.settings}
          models={log.models}
          onSaved={s => setLog(l => (l ? { ...l, settings: s } : l))}
        />
      )}

      <h2 className="mt-8 text-lg font-bold">{t('research.log')}</h2>
      {log && log.runs.length === 0 && <p className="mt-2 text-sm text-gray-500">{t('research.noRuns')}</p>}
      {log && log.runs.length > 0 && (
        <ul className="mt-2 text-sm">
          {log.runs.map(run => (
            <li key={run.id} className="border-t border-gray-800 py-2">
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <span className="font-medium">
                  {run.kind === 'discover' ? t('research.discoverRun') : run.event_id ? (
                    <Link to={`/races/${run.event_id}`} className="hover:text-blue-300">{run.event_name}</Link>
                  ) : run.event_name}
                </span>
                <span className="text-xs text-gray-500">
                  {when(run.started_at)} · {t(`research.trigger.${run.trigger}`)} · {usd(run.cost_usd)}
                </span>
              </div>
              <p className="mt-0.5 text-gray-400">
                <span className={run.status === 'failed' ? 'text-red-300' : run.status === 'running' ? 'text-blue-300' : ''}>
                  {t(`research.status.${run.status}`)}
                </span>
                {run.status === 'done' && <> · {t('research.changes', { count: run.changes })}</>}
                {run.summary && <> · {run.summary}</>}
              </p>
              {run.error && <p className="mt-0.5 break-words text-xs text-gray-500">{run.error}</p>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
