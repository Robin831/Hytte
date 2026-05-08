import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Lightbulb, Plus, Play, X, AlertTriangle, CheckCircle2, XCircle, MinusCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../components/ui/skeleton'
import { Tabs, TabList, TabTrigger, TabPanel } from '../components/ui/tabs'
import { SuggestionCard, NEW_PAGE_SLUG, type Suggestion } from '../components/suggestions/SuggestionCard'
import { SuggestionGroup } from '../components/suggestions/SuggestionGroup'
import { SuggestionActions } from '../components/suggestions/SuggestionActions'
import { NewSuggestionForm } from '../components/suggestions/NewSuggestionForm'
import { SettingsPanel } from '../components/suggestions/SettingsPanel'
import { RecentRunsPanel } from '../components/suggestions/RecentRunsPanel'
import { nextRunHintKey, formatRunTime } from './suggestionsUtils'

type TabKey = 'pending' | 'planned' | 'created' | 'rejected' | 'pages'
type GroupTabKey = Exclude<TabKey, 'pages'>

interface ListResponse {
  pending: Suggestion[]
  planned: Suggestion[]
  rejected: Suggestion[]
  bead_created?: Suggestion[]
}

interface RunLogEntry {
  slug: string
  status: 'ok' | 'error' | 'skipped_cap'
  count?: number
  cost?: number
  reason?: string
  seconds?: number
  cap?: number
  pendingCount?: number
}

interface RunProgress {
  done: number
  total: number
  pageSlugs: string[]
  log: RunLogEntry[]
}

// SSE event payloads sent by POST /api/suggestions/run.
// See internal/suggestions/handlers.go:RunHandler for the source of truth.
interface StartedEvent {
  run_id: number
  total_pages: number
  page_slugs: string[]
}
interface PageCompleteEvent {
  page_slug: string
  generated: number
  errors: number
  cost_usd: number
  elapsed_ms: number
  status: 'ok' | 'error'
  error?: string
}
interface PageSkippedCapEvent {
  page_slug: string
  pending_count: number
  cap: number
}

function sortPageSlugs(slugs: string[]): string[] {
  return [...slugs].sort((a, b) => {
    if (a === NEW_PAGE_SLUG && b === NEW_PAGE_SLUG) return 0
    if (a === NEW_PAGE_SLUG) return 1
    if (b === NEW_PAGE_SLUG) return -1
    return a.localeCompare(b)
  })
}

// Pending sections start collapsed because that tab grows long enough to need
// scrolling even with cards collapsed; other tabs stay expanded so the
// operator can see at a glance what's planned, created, or rejected.
function defaultGroupExpanded(tab: GroupTabKey): boolean {
  return tab !== 'pending'
}

function groupBySlug(list: Suggestion[]): Map<string, Suggestion[]> {
  const groups = new Map<string, Suggestion[]>()
  for (const s of list) {
    const existing = groups.get(s.page_slug)
    if (existing) {
      existing.push(s)
    } else {
      groups.set(s.page_slug, [s])
    }
  }
  return groups
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
  const [runProgress, setRunProgress] = useState<RunProgress | null>(null)
  const [alreadyRunning, setAlreadyRunning] = useState(false)
  const [streamError, setStreamError] = useState(false)
  const [recentRunsReloadSignal, setRecentRunsReloadSignal] = useState(0)
  const abortRef = useRef<AbortController | null>(null)

  // Per-card expanded state. Cards default to collapsed (absent key). We
  // remember toggled cards across tab switches but not across reloads.
  const [cardExpanded, setCardExpanded] = useState<Map<number, boolean>>(() => new Map())
  // Per-tab section expanded state. Defaults are tab-specific: the Pending tab
  // starts collapsed (operators scroll a long list, so we want only headers
  // visible) while other tabs start expanded (shorter, at-a-glance content).
  // The map only stores explicit user toggles that diverge from the default.
  const [groupOverrides, setGroupOverrides] = useState<Map<string, boolean>>(() => new Map())

  const refetch = useCallback(() => {
    setReloadKey(k => k + 1)
  }, [])

  const setCardExpansion = useCallback((id: number, next: boolean) => {
    setCardExpanded(prev => {
      const m = new Map(prev)
      m.set(id, next)
      return m
    })
  }, [])

  const isCardExpanded = useCallback(
    (id: number) => cardExpanded.get(id) === true,
    [cardExpanded],
  )

  const setGroupExpansion = useCallback(
    (tab: GroupTabKey, slug: string, next: boolean) => {
      setGroupOverrides(prev => {
        const m = new Map(prev)
        const key = `${tab}::${slug}`
        if (next === defaultGroupExpanded(tab)) {
          m.delete(key)
        } else {
          m.set(key, next)
        }
        return m
      })
    },
    [],
  )

  const isGroupExpanded = useCallback(
    (tab: GroupTabKey, slug: string) => {
      const key = `${tab}::${slug}`
      const override = groupOverrides.get(key)
      if (override !== undefined) return override
      return defaultGroupExpanded(tab)
    },
    [groupOverrides],
  )

  const handlePlanned = useCallback((updated: Suggestion) => {
    setPending(prev => prev.filter(s => s.id !== updated.id))
    setPlanned(prev => [updated, ...prev])
    // Keep the card expanded after the user planned it so the rendered plan is
    // immediately visible when they switch to the Planned tab.
    setCardExpansion(updated.id, true)
    refetch()
  }, [refetch, setCardExpansion])

  const handleBeadCreated = useCallback((updated: Suggestion) => {
    setPlanned(prev => prev.filter(s => s.id !== updated.id))
    setBeadCreated(prev => [updated, ...prev])
    setCardExpansion(updated.id, true)
    refetch()
  }, [refetch, setCardExpansion])

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

  // Cancel any in-flight stream when the component unmounts.
  useEffect(() => {
    return () => {
      abortRef.current?.abort()
      abortRef.current = null
    }
  }, [])

  const counts = useMemo(
    () => ({
      pending: pending.length,
      planned: planned.length,
      created: beadCreated.length,
      rejected: rejected.length,
    }),
    [pending, planned, rejected, beadCreated],
  )

  async function handleRunNow() {
    if (running) return
    setRunning(true)
    setRunError(null)
    setStreamError(false)
    setAlreadyRunning(false)
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    // Tracked locally so the catch handler can distinguish "stream broke
    // after we already entered progress mode" from "request never started".
    let started = false

    const dispatch = (eventName: string, data: unknown) => {
      if (eventName === 'started') {
        const ev = data as StartedEvent
        started = true
        setRunProgress({
          done: 0,
          total: ev.total_pages,
          pageSlugs: ev.page_slugs ?? [],
          log: [],
        })
        return
      }
      if (eventName === 'page_complete') {
        const ev = data as PageCompleteEvent
        const entry: RunLogEntry = {
          slug: ev.page_slug,
          status: ev.status,
          count: ev.generated,
          cost: ev.cost_usd,
          seconds: Math.round((ev.elapsed_ms ?? 0) / 1000),
        }
        if (ev.status === 'error' && ev.error) {
          entry.reason = ev.error
        }
        setRunProgress(p =>
          p ? { ...p, done: p.done + 1, log: [...p.log, entry] } : p,
        )
        // Refetch the Pending tab so newly persisted suggestions appear as
        // soon as each page finishes.
        refetch()
        return
      }
      if (eventName === 'page_skipped_cap') {
        const ev = data as PageSkippedCapEvent
        const entry: RunLogEntry = {
          slug: ev.page_slug,
          status: 'skipped_cap',
          pendingCount: ev.pending_count,
          cap: ev.cap,
        }
        // The page still occupied a rotation slot, so increment done so
        // the progress bar tracks against total_pages.
        setRunProgress(p =>
          p ? { ...p, done: p.done + 1, log: [...p.log, entry] } : p,
        )
        return
      }
      if (eventName === 'new_page_complete') {
        const ev = data as PageCompleteEvent
        const entry: RunLogEntry = {
          slug: ev.page_slug,
          status: ev.status,
          count: ev.generated,
          cost: ev.cost_usd,
          seconds: Math.round((ev.elapsed_ms ?? 0) / 1000),
        }
        if (ev.status === 'error' && ev.error) {
          entry.reason = ev.error
        }
        // Don't increment done — this pass is not counted in total_pages from the server.
        setRunProgress(p =>
          p ? { ...p, log: [...p.log, entry] } : p,
        )
        refetch()
        return
      }
      if (eventName === 'done') {
        setRunProgress(null)
        setStreamError(false)
        refetch()
        setRecentRunsReloadSignal(s => s + 1)
        return
      }
    }

    try {
      const res = await fetch('/api/suggestions/run', {
        method: 'POST',
        credentials: 'include',
        signal: controller.signal,
      })
      if (res.status === 409) {
        // Surface a banner pointing the user at the recent runs panel.
        // Don't auto-open it — the banner link is the explicit affordance.
        setAlreadyRunning(true)
        return
      }
      if (!res.ok || !res.body) {
        throw new Error(t('errors.runFailed'))
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      // SSE frame parser: events are separated by a blank line. Each event
      // has zero or more `event:` and `data:` fields. We accumulate partial
      // chunks in `buffer` and only consume up to the last complete frame.
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const frames = buffer.split('\n\n')
        buffer = frames.pop() ?? ''
        for (const frame of frames) {
          if (!frame.trim()) continue
          let eventName = 'message'
          const dataLines: string[] = []
          for (const line of frame.split('\n')) {
            if (line.startsWith('event:')) {
              eventName = line.slice(6).trim()
            } else if (line.startsWith('data:')) {
              dataLines.push(line.slice(5).trim())
            }
          }
          if (dataLines.length === 0) continue
          try {
            const data = JSON.parse(dataLines.join('\n'))
            dispatch(eventName, data)
          } catch {
            // Ignore malformed SSE frames — server should never emit them
            // but a partial buffer flush could in theory.
          }
        }
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      // If we already received a `started` event, keep the progress UI
      // visible and surface a Reconnect affordance instead of clobbering
      // it with the generic banner.
      if (started) {
        setStreamError(true)
      } else {
        setRunError(err instanceof Error ? err.message : t('errors.runFailed'))
      }
    } finally {
      setRunning(false)
      if (abortRef.current === controller) {
        abortRef.current = null
      }
    }
  }

  // Polled fallback when the SSE stream is interrupted: ask the server
  // whether the latest run has finished. If yes, clear the progress UI and
  // refetch everything; if no, just clear the error so the user can wait.
  async function handleReconnect() {
    try {
      const res = await fetch('/api/suggestions/runs?limit=1', {
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      const data = (await res.json()) as Array<{ finished_at?: string | null }>
      const latest = Array.isArray(data) ? data[0] : null
      if (latest && latest.finished_at) {
        setStreamError(false)
        setRunProgress(null)
        refetch()
        setRecentRunsReloadSignal(s => s + 1)
      }
      // If run is still in progress, leave streamError true so the Reconnect
      // button stays visible for another retry.
    } catch {
      setStreamError(true)
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
        expanded={isCardExpanded(s.id)}
        onToggleExpanded={next => setCardExpansion(s.id, next)}
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

  function renderPanel(tab: GroupTabKey, list: Suggestion[]) {
    if (list.length === 0) {
      return (
        <p className="px-4 py-10 text-center text-sm text-gray-400">
          {t(`empty.${tab}` as const)}
        </p>
      )
    }
    const groups = groupBySlug(list)
    const sortedKeys = sortPageSlugs([...groups.keys()])
    return (
      <div className="space-y-4">
        {sortedKeys.map(slug => {
          const items = groups.get(slug) ?? []
          const expanded = isGroupExpanded(tab, slug)
          const title = slug === NEW_PAGE_SLUG ? t('groups.newPageIdeas') : slug
          return (
            <SuggestionGroup
              key={slug}
              groupKey={slug}
              pageTitle={title}
              count={items.length}
              expanded={expanded}
              onToggle={next => setGroupExpansion(tab, slug, next)}
            >
              {items.map(renderCard)}
            </SuggestionGroup>
          )
        })}
      </div>
    )
  }

  const progressPercent =
    runProgress && runProgress.total > 0
      ? Math.min(100, Math.round((runProgress.done / runProgress.total) * 100))
      : 0

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
              disabled={running || runProgress !== null}
              className="inline-flex items-center gap-2 rounded-lg border border-blue-500/40 bg-blue-500/20 px-3 py-2 text-sm font-medium text-blue-300 hover:bg-blue-500/30 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Play size={16} />
              <span>
                {running || runProgress !== null ? t('actions.running') : t('actions.runNow')}
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

          {alreadyRunning && (
            <div
              role="alert"
              data-testid="already-running-banner"
              className="flex items-start gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-200"
            >
              <AlertTriangle size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
              <div className="flex-1 space-y-1">
                <p>{t('run.alreadyRunning')}</p>
                <a
                  href="#recent-runs"
                  className="inline-block text-xs font-medium text-amber-300 underline hover:text-amber-200"
                >
                  {t('run.alreadyRunningLink')}
                </a>
              </div>
              <button
                type="button"
                onClick={() => setAlreadyRunning(false)}
                aria-label={t('run.dismiss')}
                className="text-amber-300/70 hover:text-amber-200"
              >
                <X size={16} aria-hidden="true" />
              </button>
            </div>
          )}

          {runProgress && (
            <div
              data-testid="run-progress"
              className="space-y-2 rounded-lg border border-gray-800 bg-gray-900/60 px-4 py-3"
            >
              <div className="flex items-center justify-between gap-2">
                <span
                  data-testid="run-progress-pill"
                  className="inline-flex items-center rounded-full border border-blue-500/40 bg-blue-500/15 px-2.5 py-0.5 text-xs font-medium text-blue-200"
                >
                  {t('run.inProgress', {
                    done: runProgress.done,
                    total: runProgress.total,
                  })}
                </span>
                {streamError && (
                  <button
                    type="button"
                    onClick={handleReconnect}
                    data-testid="run-reconnect"
                    className="inline-flex items-center gap-1 rounded-md border border-yellow-500/40 bg-yellow-500/10 px-2 py-1 text-xs font-medium text-yellow-200 hover:bg-yellow-500/20"
                  >
                    {t('run.reconnect')}
                  </button>
                )}
              </div>
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={runProgress.total}
                aria-valuenow={runProgress.done}
                className="h-1.5 w-full overflow-hidden rounded-full bg-gray-800"
              >
                <div
                  className="h-full bg-blue-500 transition-all"
                  style={{ width: `${progressPercent}%` }}
                />
              </div>
              {streamError && (
                <p
                  data-testid="run-stream-error"
                  className="text-xs text-yellow-300"
                >
                  {t('run.streamError')}
                </p>
              )}
              {runProgress.log.length > 0 && (
                <ul
                  data-testid="run-progress-log"
                  className="space-y-1 text-xs"
                >
                  {runProgress.log.map((entry, idx) => {
                    const tone =
                      entry.status === 'ok'
                        ? 'text-emerald-300'
                        : entry.status === 'skipped_cap'
                          ? 'text-gray-400'
                          : 'text-red-300'
                    return (
                      <li
                        key={`${entry.slug}-${idx}`}
                        data-testid={`run-progress-log-${entry.slug}`}
                        className={`flex items-center gap-2 ${tone}`}
                      >
                        {entry.status === 'ok' ? (
                          <CheckCircle2 size={14} aria-hidden="true" />
                        ) : entry.status === 'skipped_cap' ? (
                          <MinusCircle size={14} aria-hidden="true" />
                        ) : (
                          <XCircle size={14} aria-hidden="true" />
                        )}
                        <span className="font-mono">
                          {entry.status === 'ok'
                            ? t('run.pageOk', {
                                slug: entry.slug,
                                count: entry.count ?? 0,
                                cost: (entry.cost ?? 0).toFixed(2),
                              })
                            : entry.status === 'skipped_cap'
                              ? t('run.pageSkippedCap', {
                                  slug: entry.slug,
                                  cap: entry.cap ?? 0,
                                })
                              : t('run.pageError', {
                                  slug: entry.slug,
                                  reason: entry.reason ?? t('errors.unknown'),
                                  seconds: entry.seconds ?? 0,
                                })}
                        </span>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          )}

          <RecentRunsPanel reloadSignal={recentRunsReloadSignal} />
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
            <TabTrigger value="created">
              {t('tabs.created')} ({counts.created})
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
              <TabPanel value="created">{renderPanel('created', beadCreated)}</TabPanel>
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
