import { useCallback, useEffect, useMemo, useState } from 'react'
import { Lightbulb, Plus, Play } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../components/ui/skeleton'
import { Tabs, TabList, TabTrigger, TabPanel } from '../components/ui/tabs'
import { SuggestionCard, type Suggestion } from '../components/suggestions/SuggestionCard'
import { SuggestionActions } from '../components/suggestions/SuggestionActions'
import { NewSuggestionForm } from '../components/suggestions/NewSuggestionForm'
import { SettingsPanel } from '../components/suggestions/SettingsPanel'
import { RecentRunsPanel } from '../components/suggestions/RecentRunsPanel'
import { nextRunHintKey, formatRunTime } from './suggestionsUtils'

type TabKey = 'pending' | 'planned' | 'rejected' | 'pages'

interface ListResponse {
  pending: Suggestion[]
  planned: Suggestion[]
  rejected: Suggestion[]
  bead_created?: Suggestion[]
}

export default function Suggestions() {
  const { t, i18n } = useTranslation('suggestions')
  const { t: tCommon } = useTranslation('common')
  const [pending, setPending] = useState<Suggestion[]>([])
  const [planned, setPlanned] = useState<Suggestion[]>([])
  const [rejected, setRejected] = useState<Suggestion[]>([])
  const [beadCreated, setBeadCreated] = useState<Suggestion[]>([])
  const [loading, setLoading] = useState(true)
  const [hasData, setHasData] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [runError, setRunError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('pending')
  const [reloadKey, setReloadKey] = useState(0)
  const [newOpen, setNewOpen] = useState(false)

  const refetch = useCallback(() => {
    setReloadKey(k => k + 1)
  }, [])

  const handlePlanned = useCallback((updated: Suggestion) => {
    setPending(prev => prev.filter(s => s.id !== updated.id))
    setPlanned(prev => [updated, ...prev])
    refetch()
  }, [refetch])

  const handleBeadCreated = useCallback((updated: Suggestion) => {
    setPlanned(prev => prev.filter(s => s.id !== updated.id))
    setBeadCreated(prev => [updated, ...prev])
    refetch()
  }, [refetch])

  const failedToLoadMsg = t('errors.failedToLoad')

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      setLoadError(null)
      try {
        const res = await fetch('/api/suggestions', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) {
          throw new Error(failedToLoadMsg)
        }
        const data = (await res.json()) as ListResponse
        setPending(data.pending ?? [])
        setPlanned(data.planned ?? [])
        setRejected(data.rejected ?? [])
        setBeadCreated(data.bead_created ?? [])
        setHasData(true)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setLoadError(err instanceof Error ? err.message : failedToLoadMsg)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [reloadKey, failedToLoadMsg])

  const counts = useMemo(
    () => ({
      pending: pending.length,
      planned: planned.length + beadCreated.length,
      rejected: rejected.length,
    }),
    [pending, planned, rejected, beadCreated],
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
        throw new Error(t('errors.runFailed'))
      }
      refetch()
    } catch (err) {
      setRunError(err instanceof Error ? err.message : t('errors.runFailed'))
    } finally {
      setRunning(false)
    }
  }

  function handleNewSuggestion() {
    setNewOpen(true)
  }

  const handleCreated = useCallback(() => {
    setNewOpen(false)
    setActiveTab('pending')
    refetch()
  }, [refetch])

  function renderCard(s: Suggestion) {
    return (
      <SuggestionCard
        key={s.id}
        suggestion={s}
        actionsSlot={
          s.status === 'rejected' ? null : (
            <SuggestionActions
              suggestion={s}
              onPlanned={handlePlanned}
              onRejected={refetch}
              onBeadCreated={handleBeadCreated}
            />
          )
        }
      />
    )
  }

  function renderPanel(tab: Exclude<TabKey, 'pages'>, list: Suggestion[]) {
    if (tab === 'planned') {
      if (list.length === 0 && beadCreated.length === 0) {
        return (
          <p className="px-4 py-10 text-center text-sm text-gray-400">
            {t('empty.planned')}
          </p>
        )
      }
      return (
        <div className="space-y-6">
          {list.length > 0 && (
            <div className="space-y-3">
              {list.map(renderCard)}
            </div>
          )}
          {beadCreated.length > 0 && (
            <section
              aria-labelledby="bead-created-heading"
              data-testid="bead-created-section"
              className="space-y-3"
            >
              <h2
                id="bead-created-heading"
                className="text-sm font-semibold uppercase tracking-wide text-emerald-300"
              >
                {t('headings.beadCreated')}
              </h2>
              {beadCreated.map(renderCard)}
            </section>
          )}
        </div>
      )
    }
    if (list.length === 0) {
      return (
        <p className="px-4 py-10 text-center text-sm text-gray-400">
          {t(`empty.${tab}` as const)}
        </p>
      )
    }
    return (
      <div className="space-y-3">
        {list.map(renderCard)}
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
              {t('header.title')}
            </h1>
          </div>
          <p className="text-sm text-gray-400">{t(nextRunHintKey(new Date()), { time: formatRunTime(i18n.language) })}</p>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={handleRunNow}
              disabled={running}
              className="inline-flex items-center gap-2 rounded-lg border border-blue-500/40 bg-blue-500/20 px-3 py-2 text-sm font-medium text-blue-300 hover:bg-blue-500/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Play size={16} />
              <span>
                {running ? t('actions.running') : t('actions.runNow')}
              </span>
            </button>
            <button
              type="button"
              onClick={handleNewSuggestion}
              className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm font-medium text-gray-200 hover:border-gray-600 hover:text-white"
            >
              <Plus size={16} />
              <span>{t('actions.newSuggestion')}</span>
            </button>
          </div>
          <RecentRunsPanel />
        </header>

        <Tabs
          value={activeTab}
          onChange={v => setActiveTab(v as TabKey)}
          variant="segment"
        >
          <TabList aria-label={t('header.title')} className="mb-4">
            <TabTrigger value="pending">
              {t('tabs.pending')} ({counts.pending})
            </TabTrigger>
            <TabTrigger value="planned">
              {t('tabs.planned')} ({counts.planned})
            </TabTrigger>
            <TabTrigger value="rejected">
              {t('tabs.rejected')} ({counts.rejected})
            </TabTrigger>
            <TabTrigger value="pages">
              {t('tabs.pages')}
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
          {loading && !hasData ? (
            <div className="space-y-3" aria-label={tCommon('skeleton.loading')}>
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
            </div>
          ) : !hasData && loadError ? (
            <div
              role="alert"
              data-testid="load-error"
              className="rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-300"
            >
              {loadError}
            </div>
          ) : (
            <>
              {loadError && (
                <div
                  role="alert"
                  data-testid="load-error"
                  className="mb-4 rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-300"
                >
                  {loadError}
                </div>
              )}
              <TabPanel value="pending">{renderPanel('pending', pending)}</TabPanel>
              <TabPanel value="planned">{renderPanel('planned', planned)}</TabPanel>
              <TabPanel value="rejected">{renderPanel('rejected', rejected)}</TabPanel>
              <TabPanel value="pages">
                <SettingsPanel active={activeTab === 'pages'} />
              </TabPanel>
            </>
          )}
        </Tabs>
      </div>
      <NewSuggestionForm
        open={newOpen}
        onClose={() => setNewOpen(false)}
        onCreated={handleCreated}
      />
    </div>
  )
}

