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
import ShortcutHelpModal from '../components/mezzanine/ShortcutHelpModal'
import ToastList from '../components/ToastList'

export default function MezzaninePage() {
  const { t } = useTranslation('forge')
  const { workers, refresh: refreshWorkers } = useForgeWorkers()
  const { status, refresh: refreshStatus } = useForgeStatus()
  const { beads: queueBeads, refresh: refreshQueue } = useForgeQueue()
  const [searchParams] = useSearchParams()
  const [selectedBeadId, setSelectedBeadId] = useState<string | null>(() => searchParams.get('bead'))
  const [showShortcutHelp, setShowShortcutHelp] = useState(false)
  const [focusedPanel, setFocusedPanel] = useState<PanelKey | null>(null)
  const [focusedWorkerIndex, setFocusedWorkerIndex] = useState<number | null>(null)
  const { toasts, showToast } = useToast()
  const abortRef = useRef<AbortController | null>(null)

  const queueRef = useRef<HTMLDivElement>(null)
  const workersRef = useRef<HTMLDivElement>(null)
  const eventsRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    return () => { abortRef.current?.abort() }
  }, [])

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
    showToast(t('mezzanine.shortcuts.refreshing'), 'info')
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
      void handleMerge(ready.id, ready.number)
    } else {
      showToast(t('mezzanine.shortcuts.noMergeReady'), 'error')
    }
  }, [status, handleMerge, showToast, t])

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
        <PipelineBar
          workers={workers}
          openPRs={status?.open_prs}
          queueBeads={queueBeads}
          onBeadClick={setSelectedBeadId}
          onMerge={handleMerge}
          showToast={showToast}
        />

        <NeedsAttentionPanel
          stuck={status?.stuck ?? []}
          showToast={showToast}
          onBeadClick={setSelectedBeadId}
        />

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

      <ShortcutHelpModal
        open={showShortcutHelp}
        onClose={() => setShowShortcutHelp(false)}
      />

      <ToastList toasts={toasts} />
    </MezzanineLayout>
  )
}
