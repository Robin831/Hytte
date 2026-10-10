import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router'
import { Loader2, RefreshCw, Sparkles } from 'lucide-react'
import {
  type ResearchLog, type ResearchRun,
  fetchRace, fetchResearchLog, intlLocale, startDiscovery, startResearch,
} from './racesApi'
import { useRaceFormat } from './useRaceFormat'

const POLL_MS = 4000

/** Maps a research start failure (server message) to a translated line. */
function useStartError() {
  const { t } = useRaceFormat()
  return (err: unknown) => {
    const msg = err instanceof Error ? err.message : ''
    if (msg.includes('checked recently')) return t('research.cooldown')
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

/** Admin view: today's research spend, recent runs and a discovery trigger. */
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

  const usd = (n: number) => `$${n.toFixed(2)}`

  return (
    <div>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="text-sm text-gray-300">
          <p>{t('research.about')}</p>
          {log && (
            <p className="mt-1 text-gray-400">
              {t('research.spend', { spent: usd(log.spent_today_usd), budget: log.budget_usd != null ? usd(log.budget_usd) : '—' })}
              {log.model && <> · {log.model}</>}
            </p>
          )}
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

      {log && log.runs.length === 0 && <p className="mt-6 text-sm text-gray-500">{t('research.noRuns')}</p>}
      {log && log.runs.length > 0 && (
        <ul className="mt-4 text-sm">
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
