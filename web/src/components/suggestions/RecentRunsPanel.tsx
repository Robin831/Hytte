import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../ui/skeleton'

export interface SuggestionRun {
  id: number
  user_id: number
  started_at: string
  finished_at?: string | null
  trigger: string
  page_slugs: string
  generated: number
  errors: number
  cost_usd: number
}

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000

function pageCount(csv: string): number {
  if (!csv) return 0
  return csv
    .split(',')
    .map(s => s.trim())
    .filter(Boolean).length
}

function formatCost(cost: number, placeholder: string): string {
  if (cost === 0) return placeholder
  return `$${cost.toFixed(4)}`
}

export function RecentRunsPanel() {
  const { t, i18n } = useTranslation('suggestions')
  const { t: tCommon } = useTranslation('common')
  const [open, setOpen] = useState(false)
  const [runs, setRuns] = useState<SuggestionRun[]>([])
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)

  const loadErrorMsg = t('recentRuns.loadError')

  useEffect(() => {
    if (!open || loaded) return
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      setLoadError(null)
      try {
        const res = await fetch('/api/suggestions/runs?limit=20', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error('failed')
        const data = (await res.json()) as SuggestionRun[]
        setRuns(Array.isArray(data) ? data : [])
        setLoaded(true)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setLoadError(loadErrorMsg)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [open, loaded, loadErrorMsg])

  const dateFormatter = useMemo(() => {
    const isThaiLocale = i18n.language.toLowerCase().startsWith('th')
    return new Intl.DateTimeFormat(i18n.language, {
      dateStyle: 'medium',
      timeStyle: 'short',
      ...(isThaiLocale ? { calendar: 'gregory' } : {}),
    })
  }, [i18n.language])

  const summary = useMemo(() => {
    const cutoff = Date.now() - SEVEN_DAYS_MS
    let totalCost = 0
    let totalGenerated = 0
    for (const r of runs) {
      const startedMs = Date.parse(r.started_at)
      if (Number.isNaN(startedMs) || startedMs < cutoff) continue
      totalCost += r.cost_usd
      totalGenerated += r.generated
    }
    return { totalCost, totalGenerated }
  }, [runs])

  const costPlaceholder = t('recentRuns.costUnknown')

  const toggle = useCallback(() => setOpen(prev => !prev), [])

  return (
    <section
      aria-labelledby="recent-runs-heading"
      data-testid="recent-runs-panel"
      className="rounded-lg border border-gray-800 bg-gray-900/40"
    >
      <h2 id="recent-runs-heading" className="m-0">
        <button
          type="button"
          onClick={toggle}
          aria-expanded={open}
          aria-controls="recent-runs-content"
          className="flex w-full items-center justify-between gap-2 rounded-lg px-4 py-3 text-left text-sm font-medium text-gray-100 hover:bg-gray-800/40"
        >
          <span className="flex items-center gap-2">
            {open ? (
              <ChevronDown size={16} className="text-gray-400" aria-hidden="true" />
            ) : (
              <ChevronRight size={16} className="text-gray-400" aria-hidden="true" />
            )}
            {t('recentRuns.title')}
          </span>
          <span className="sr-only">
            {open ? t('recentRuns.hide') : t('recentRuns.show')}
          </span>
        </button>
      </h2>
      {open && (
        <div
          id="recent-runs-content"
          data-testid="recent-runs-content"
          className="border-t border-gray-800 px-4 py-3"
        >
          {loading && !loaded ? (
            <div className="space-y-2" aria-label={tCommon('skeleton.loading')}>
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : loadError ? (
            <div
              role="alert"
              data-testid="recent-runs-error"
              className="rounded-lg border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-300"
            >
              {loadError}
            </div>
          ) : runs.length === 0 ? (
            <p
              data-testid="recent-runs-empty"
              className="px-2 py-6 text-center text-sm text-gray-400"
            >
              {t('recentRuns.empty')}
            </p>
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="text-xs uppercase tracking-wide text-gray-500">
                    <tr>
                      <th scope="col" className="px-2 py-2 font-medium">
                        {t('recentRuns.columns.startedAt')}
                      </th>
                      <th scope="col" className="px-2 py-2 font-medium">
                        {t('recentRuns.columns.trigger')}
                      </th>
                      <th scope="col" className="hidden px-2 py-2 font-medium sm:table-cell">
                        {t('recentRuns.columns.pages')}
                      </th>
                      <th scope="col" className="hidden px-2 py-2 font-medium sm:table-cell">
                        {t('recentRuns.columns.generated')}
                      </th>
                      <th scope="col" className="hidden px-2 py-2 font-medium sm:table-cell">
                        {t('recentRuns.columns.errors')}
                      </th>
                      <th scope="col" className="px-2 py-2 font-medium text-right">
                        {t('recentRuns.columns.cost')}
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-800/70 text-gray-200">
                    {runs.map(run => {
                      const startedMs = Date.parse(run.started_at)
                      const startedLabel = Number.isNaN(startedMs)
                        ? run.started_at
                        : dateFormatter.format(new Date(startedMs))
                      const triggerKey =
                        run.trigger === 'scheduled'
                          ? 'recentRuns.trigger.scheduled'
                          : 'recentRuns.trigger.manual'
                      const badgeClass =
                        run.trigger === 'scheduled'
                          ? 'border-blue-500/40 bg-blue-500/15 text-blue-300'
                          : 'border-emerald-500/40 bg-emerald-500/15 text-emerald-300'
                      return (
                        <tr key={run.id} data-testid={`recent-run-${run.id}`}>
                          <td className="px-2 py-2 align-top text-gray-300">
                            {startedLabel}
                          </td>
                          <td className="px-2 py-2 align-top">
                            <span
                              data-testid={`recent-run-${run.id}-trigger`}
                              className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${badgeClass}`}
                            >
                              {t(triggerKey)}
                            </span>
                          </td>
                          <td
                            className="hidden px-2 py-2 align-top text-gray-300 sm:table-cell"
                            data-testid={`recent-run-${run.id}-pages`}
                          >
                            {pageCount(run.page_slugs)}
                          </td>
                          <td className="hidden px-2 py-2 align-top text-gray-300 sm:table-cell">
                            {run.generated}
                          </td>
                          <td className="hidden px-2 py-2 align-top text-gray-300 sm:table-cell">
                            {run.errors}
                          </td>
                          <td
                            className="px-2 py-2 align-top text-right font-mono text-gray-300"
                            data-testid={`recent-run-${run.id}-cost`}
                          >
                            {formatCost(run.cost_usd, costPlaceholder)}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
              <p
                data-testid="recent-runs-summary"
                className="mt-3 text-sm text-gray-400"
              >
                {t('recentRuns.summary', {
                  total: `$${summary.totalCost.toFixed(2)}`,
                  count: summary.totalGenerated,
                })}
              </p>
            </>
          )}
        </div>
      )}
    </section>
  )
}
