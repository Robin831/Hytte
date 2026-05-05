import { useCallback, useEffect, useMemo, useState } from 'react'
import { Lightbulb, Plus, Play } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../components/ui/skeleton'
import { Tabs, TabList, TabTrigger, TabPanel } from '../components/ui/tabs'
import { SuggestionCard, type Suggestion } from '../components/suggestions/SuggestionCard'

type TabKey = 'pending' | 'planned' | 'rejected'

interface ListResponse {
  pending: Suggestion[]
  planned: Suggestion[]
  rejected: Suggestion[]
}

export default function Suggestions() {
  const { t } = useTranslation('common')
  const [pending, setPending] = useState<Suggestion[]>([])
  const [planned, setPlanned] = useState<Suggestion[]>([])
  const [rejected, setRejected] = useState<Suggestion[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [runError, setRunError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('pending')
  const [reloadKey, setReloadKey] = useState(0)

  const refetch = useCallback(() => {
    setReloadKey(k => k + 1)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setLoadError(null)
    ;(async () => {
      try {
        const res = await fetch('/api/suggestions', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) {
          throw new Error(t('suggestions.errors.failedToLoad'))
        }
        const data = (await res.json()) as ListResponse
        setPending(data.pending ?? [])
        setPlanned(data.planned ?? [])
        setRejected(data.rejected ?? [])
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setLoadError(err instanceof Error ? err.message : t('suggestions.errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [reloadKey, t])

  const counts = useMemo(
    () => ({
      pending: pending.length,
      planned: planned.length,
      rejected: rejected.length,
    }),
    [pending, planned, rejected],
  )

  async function handleRunNow() {
    if (running) return
    setRunning(true)
    setRunError(null)
    try {
      const res = await fetch('/api/suggestions/run', {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        throw new Error(t('suggestions.errors.runFailed'))
      }
      refetch()
    } catch (err) {
      setRunError(err instanceof Error ? err.message : t('suggestions.errors.runFailed'))
    } finally {
      setRunning(false)
    }
  }

  function handleNewSuggestion() {
    // Wired up by a follow-up sub-task that adds the create-suggestion form.
  }

  const handlePlan = useCallback(
    (_id: number) => {
      refetch()
    },
    [refetch],
  )

  const handleReject = useCallback(
    (_id: number) => {
      refetch()
    },
    [refetch],
  )

  function renderPanel(tab: TabKey, list: Suggestion[]) {
    if (list.length === 0) {
      return (
        <p className="px-4 py-10 text-center text-sm text-gray-400">
          {t(`suggestions.empty.${tab}` as const)}
        </p>
      )
    }
    return (
      <div className="space-y-3">
        {list.map(s => (
          <SuggestionCard
            key={s.id}
            suggestion={s}
            onPlan={handlePlan}
            onReject={handleReject}
          />
        ))}
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-950 text-gray-100">
      <div className="mx-auto w-full max-w-3xl px-4 py-6 space-y-6">
        <header className="space-y-3">
          <div className="flex items-center gap-3">
            <Lightbulb size={24} className="text-yellow-400 shrink-0" />
            <h1 className="text-2xl font-semibold text-white">
              {t('nav.suggestions')}
            </h1>
          </div>
          <p className="text-sm text-gray-400">{t('suggestions.nextRunHint')}</p>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={handleRunNow}
              disabled={running}
              className="inline-flex items-center gap-2 rounded-lg border border-blue-500/40 bg-blue-500/20 px-3 py-2 text-sm font-medium text-blue-300 hover:bg-blue-500/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Play size={16} />
              <span>
                {running ? t('suggestions.actions.running') : t('suggestions.actions.runNow')}
              </span>
            </button>
            <button
              type="button"
              onClick={handleNewSuggestion}
              className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm font-medium text-gray-200 hover:border-gray-600 hover:text-white"
            >
              <Plus size={16} />
              <span>{t('suggestions.actions.newSuggestion')}</span>
            </button>
          </div>
        </header>

        <Tabs
          value={activeTab}
          onChange={v => setActiveTab(v as TabKey)}
          variant="segment"
        >
          <TabList aria-label={t('nav.suggestions')} className="mb-4">
            <TabTrigger value="pending">
              {t('suggestions.tabs.pending')} ({counts.pending})
            </TabTrigger>
            <TabTrigger value="planned">
              {t('suggestions.tabs.planned')} ({counts.planned})
            </TabTrigger>
            <TabTrigger value="rejected">
              {t('suggestions.tabs.rejected')} ({counts.rejected})
            </TabTrigger>
          </TabList>

          {runError && (
            <div
              role="alert"
              data-testid="run-error"
              className="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-300"
            >
              {runError}
            </div>
          )}
          {loading ? (
            <div className="space-y-3" aria-label={t('skeleton.loading')}>
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
            </div>
          ) : loadError ? (
            <div
              role="alert"
              data-testid="load-error"
              className="rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-300"
            >
              {loadError}
            </div>
          ) : (
            <>
              <TabPanel value="pending">{renderPanel('pending', pending)}</TabPanel>
              <TabPanel value="planned">{renderPanel('planned', planned)}</TabPanel>
              <TabPanel value="rejected">{renderPanel('rejected', rejected)}</TabPanel>
            </>
          )}
        </Tabs>
      </div>
    </div>
  )
}

