import { useState, useCallback, useRef, useEffect, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useForgeWorkers, useForgeStatus, useForgeQueue } from '../hooks/useForgeStatus'
import { useToast } from '../hooks/useToast'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import type { PanelKey } from '../hooks/useKeyboardShortcuts'
import MezzanineLayout from '../components/mezzanine/MezzanineLayout'
import WorkerPanelGrid from '../components/mezzanine/WorkerPanelGrid'
import QueueSidebar from '../components/mezzanine/QueueSidebar'
import PipelineBar from '../components/mezzanine/PipelineBar'
import NeedsAttentionPanel from '../components/mezzanine/NeedsAttentionPanel'
import EventsPanel from '../components/mezzanine/EventsPanel'
import CostsPanel from '../components/mezzanine/CostsPanel'
import BeadDetailModal from '../components/BeadDetailModal'
import MergeConfirmDialog from '../components/MergeConfirmDialog'
import ShortcutHelpModal from '../components/mezzanine/ShortcutHelpModal'
import ToastList from '../components/ToastList'

export default function MezzaninePage() {
  const { t } = useTranslation('forge')
  const { workers, refresh: refreshWorkers } = useForgeWorkers()
  const { status, refresh: refreshStatus } = useForgeStatus()
  const { beads: queueBeads, refresh: refreshQueue } = useForgeQueue()
  const [searchParams, setSearchParams] = useSearchParams()
  // Capture deep-link params once at mount so they survive after URL params are cleared.
  const [initialHighlightParam] = useState(() => searchParams.get('highlight'))
  const [initialSectionParam] = useState(() => searchParams.get('section'))
  const [initialBeadDeepLink] = useState(() => searchParams.get('bead'))
  const [selectedBeadId, setSelectedBeadId] = useState<string | null>(() => searchParams.get('bead'))
  const [showShortcutHelp, setShowShortcutHelp] = useState(false)
  const [mergeConfirmPR, setMergeConfirmPR] = useState<{ id: number; number: number } | null>(null)
  const [focusedPanel, setFocusedPanel] = useState<PanelKey | null>(null)
  const [focusedWorkerIndex, setFocusedWorkerIndex] = useState<number | null>(null)
  const { toasts, showToast } = useToast()
  const abortRef = useRef<AbortController | null>(null)

  // Extract highlighted bead ID from "pr-{beadId}" format (stable, derived from initial params)
  const highlightBeadId = initialHighlightParam?.startsWith('pr-') ? initialHighlightParam.slice(3) : null
  // For needs-attention panel: highlight bead only when section targets it
  const needsAttentionHighlightBeadId = initialSectionParam === 'needs-attention' ? initialBeadDeepLink : null

  const queueRef = useRef<HTMLDivElement>(null)
  const workersRef = useRef<HTMLDivElement>(null)
  const eventsRef = useRef<HTMLDivElement>(null)
  const pipelineRef = useRef<HTMLDivElement>(null)
  const needsAttentionRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    return () => { abortRef.current?.abort() }
  }, [])

  // Auto-scroll to the targeted section from deep link params.
  // Uses stable initial values captured at mount so highlighting survives URL param cleanup.
  useEffect(() => {
    if (!initialSectionParam && !initialHighlightParam) return

    // Clear deep link params from URL immediately — state holds the stable values for highlighting
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      next.delete('highlight')
      next.delete('section')
      next.delete('bead')
      return next
    }, { replace: true })

    let cancelled = false

    // Small delay to allow data to load and components to render before scrolling
    const timer = setTimeout(() => {
      if (cancelled) return

      if (initialSectionParam === 'needs-attention' && needsAttentionRef.current) {
        needsAttentionRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' })
      } else if (initialSectionParam === 'pipeline' && pipelineRef.current) {
        pipelineRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' })
      } else if (initialHighlightParam?.startsWith('pr-') && pipelineRef.current) {
        pipelineRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' })
      }

      // Open bead detail modal if bead param is set alongside section
      if (initialBeadDeepLink) {
        setSelectedBeadId(initialBeadDeepLink)
      }
    }, 300)

    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []) // Run once on mount — initial params are stable state, no deps needed

  const handleMerge = useCallback(async (prId: number, prNumber: number) => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    try {
      const res = await fetch(`/api/forge/prs/${prId}/merge`, {
        method: 'POST',
        credentials: 'include',
        signal: controller.signal,
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        showToast((body as { error?: string }).error ?? t('readyToMerge.mergeError'), 'error')
        return
      }
      showToast(t('readyToMerge.mergeSuccess', { number: prNumber }), 'success')
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      showToast(t('readyToMerge.mergeError'), 'error')
    }
  }, [showToast, t])

  const activeWorkers = useMemo(
    () => workers.filter(w => w.status === 'pending' || w.status === 'running'),
    [workers],
  )

  const handleRefresh = useCallback(() => {
    refreshWorkers()
    refreshStatus()
    refreshQueue()
    showToast(t('mezzanine.shortcuts.refreshing'), 'success')
  }, [refreshWorkers, refreshStatus, refreshQueue, showToast, t])

  const handleMergeFirstReady = useCallback(() => {
    const prs = status?.open_prs
    if (!prs) {
      showToast(t('mezzanine.shortcuts.noMergeReady'), 'error')
      return
    }
    const ready = prs.find(
      pr => pr.ci_passing && pr.has_approval && !pr.is_conflicting && !pr.has_unresolved_threads,
    )
    if (ready) {
      setMergeConfirmPR({ id: ready.id, number: ready.number })
    } else {
      showToast(t('mezzanine.shortcuts.noMergeReady'), 'error')
    }
  }, [status, showToast, t])

  const handleKillFocusedWorker = useCallback(() => {
    if (focusedWorkerIndex === null || focusedWorkerIndex >= activeWorkers.length) {
      showToast(t('mezzanine.shortcuts.noWorkerFocused'), 'error')
      return
    }
    const panel = document.querySelector(`[data-worker-index="${focusedWorkerIndex}"]`)
    const killBtn = panel?.querySelector<HTMLButtonElement>('[data-kill-button]')
    if (killBtn) {
      killBtn.click()
    } else {
      showToast(t('mezzanine.shortcuts.noWorkerFocused'), 'error')
    }
  }, [focusedWorkerIndex, activeWorkers.length, showToast, t])

  const handleFocusPanel = useCallback((panel: PanelKey) => {
    setFocusedPanel(panel)
    setFocusedWorkerIndex(null)
    const ref = panel === 'queue' ? queueRef : panel === 'workers' ? workersRef : eventsRef
    ref.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }, [])

  const handleFocusWorker = useCallback((index: number) => {
    if (index >= activeWorkers.length) return
    setFocusedWorkerIndex(index)
    setFocusedPanel('workers')
    const el = document.querySelector(`[data-worker-index="${index}"]`)
    el?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  }, [activeWorkers.length])

  const shortcutActions = useMemo(
    () => ({
      onRefresh: handleRefresh,
      onMergeFirstReady: handleMergeFirstReady,
      onKillFocusedWorker: handleKillFocusedWorker,
      onFocusPanel: handleFocusPanel,
      onFocusWorker: handleFocusWorker,
      onShowHelp: () => setShowShortcutHelp(true),
    }),
    [handleRefresh, handleMergeFirstReady, handleKillFocusedWorker, handleFocusPanel, handleFocusWorker],
  )

  useKeyboardShortcuts(shortcutActions)

  return (
    <MezzanineLayout sidebar={
      <div ref={queueRef} className={focusedPanel === 'queue' ? 'ring-2 ring-amber-500/50 ring-inset rounded' : ''}>
        <QueueSidebar onBeadClick={setSelectedBeadId} />
      </div>
    }>
      <div className="flex flex-col gap-4">
        <div ref={pipelineRef}>
          <PipelineBar
            workers={workers}
            openPRs={status?.open_prs}
            queueBeads={queueBeads}
            onBeadClick={setSelectedBeadId}
            onMerge={handleMerge}
            showToast={showToast}
            highlightBeadId={highlightBeadId}
          />
        </div>

        <div ref={needsAttentionRef}>
          <NeedsAttentionPanel
            stuck={status?.stuck ?? []}
            showToast={showToast}
            onBeadClick={setSelectedBeadId}
            highlightBeadId={needsAttentionHighlightBeadId}
          />
        </div>

        <div ref={workersRef} className={focusedPanel === 'workers' ? 'ring-2 ring-amber-500/50 rounded-xl' : ''}>
          <WorkerPanelGrid
            workers={workers}
            onBeadClick={setSelectedBeadId}
            focusedWorkerIndex={focusedWorkerIndex}
          />
        </div>

        <div ref={eventsRef} className={`grid grid-cols-1 md:grid-cols-2 gap-4 ${focusedPanel === 'events' ? 'ring-2 ring-amber-500/50 rounded-xl' : ''}`}>
          <EventsPanel onBeadClick={setSelectedBeadId} />
          <CostsPanel />
        </div>
      </div>

      <BeadDetailModal
        open={selectedBeadId !== null}
        beadId={selectedBeadId}
        onClose={() => setSelectedBeadId(null)}
      />

      {mergeConfirmPR && (
        <MergeConfirmDialog
          open={mergeConfirmPR !== null}
          prNumber={mergeConfirmPR.number}
          onConfirm={() => {
            const pr = mergeConfirmPR
            setMergeConfirmPR(null)
            void handleMerge(pr.id, pr.number)
          }}
          onCancel={() => setMergeConfirmPR(null)}
        />
      )}

      <ShortcutHelpModal
        open={showShortcutHelp}
        onClose={() => setShowShortcutHelp(false)}
      />

      <ToastList toasts={toasts} />
    </MezzanineLayout>
  )
}
