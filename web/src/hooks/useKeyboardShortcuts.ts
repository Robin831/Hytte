import { useEffect, useCallback } from 'react'

export type PanelKey = 'queue' | 'workers' | 'events'

export interface KeyboardShortcutActions {
  onRefresh: () => void
  onMergeFirstReady: () => void
  onKillFocusedWorker: () => void
  onFocusPanel: (panel: PanelKey) => void
  onFocusWorker: (index: number) => void
  onShowHelp: () => void
  onTogglePRModal: () => void
  onToggleReleaseModal: () => void
  onToggleSettingsModal: () => void
}

export function useKeyboardShortcuts(actions: KeyboardShortcutActions, enabled = true) {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // Ignore key repeat — prevents spamming destructive actions (merge/kill)
      if (e.repeat) return

      // Ignore when typing in inputs, textareas, or contenteditable
      const target = e.target as HTMLElement
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' ||
        target.isContentEditable
      ) {
        return
      }

      // Ignore when modifier keys are held (allow browser shortcuts)
      if (e.ctrlKey || e.metaKey || e.altKey) return

      // Allow "p" and "l" to toggle modals even when a modal is open (so they can close themselves)
      if (e.key === 'p') {
        e.preventDefault()
        actions.onTogglePRModal()
        return
      }
      if (e.key === 'l') {
        e.preventDefault()
        actions.onToggleReleaseModal()
        return
      }
      if (e.key === 's') {
        e.preventDefault()
        actions.onToggleSettingsModal()
        return
      }

      // Ignore all other global shortcuts while an aria-modal dialog is open
      if (document.querySelector('[aria-modal="true"]')) return

      switch (e.key) {
        case 'r':
          e.preventDefault()
          actions.onRefresh()
          break
        case 'm':
          e.preventDefault()
          actions.onMergeFirstReady()
          break
        case 'k':
          e.preventDefault()
          actions.onKillFocusedWorker()
          break
        case 'q':
          e.preventDefault()
          actions.onFocusPanel('queue')
          break
        case 'w':
          e.preventDefault()
          actions.onFocusPanel('workers')
          break
        case 'e':
          e.preventDefault()
          actions.onFocusPanel('events')
          break
        case '1':
        case '2':
        case '3':
        case '4':
        case '5':
        case '6':
          e.preventDefault()
          actions.onFocusWorker(parseInt(e.key, 10) - 1)
          break
        case '?':
          e.preventDefault()
          actions.onShowHelp()
          break
        case 'Escape':
          // Let modals handle their own Escape
          break
      }
    },
    [actions],
  )

  useEffect(() => {
    // Only register shortcuts on pointer:fine (desktop/mouse) devices
    if (!enabled) return
    if (typeof window.matchMedia !== 'function') return
    if (!window.matchMedia('(pointer: fine)').matches) return
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown, enabled])
}
