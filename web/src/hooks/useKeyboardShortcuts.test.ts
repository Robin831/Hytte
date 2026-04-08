// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'
import type { KeyboardShortcutActions } from './useKeyboardShortcuts'

function makeActions(overrides: Partial<KeyboardShortcutActions> = {}): KeyboardShortcutActions {
  return {
    onRefresh: vi.fn(),
    onMergeFirstReady: vi.fn(),
    onKillFocusedWorker: vi.fn(),
    onFocusPanel: vi.fn(),
    onFocusWorker: vi.fn(),
    onShowHelp: vi.fn(),
    onTogglePRModal: vi.fn(),
    ...overrides,
  }
}

function fireKey(key: string, targetOverrides: Partial<EventTarget & HTMLElement> = {}) {
  const target = Object.assign(document.createElement('div'), targetOverrides)
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
  Object.defineProperty(event, 'target', { value: target })
  document.dispatchEvent(event)
  return event
}

beforeEach(() => {
  // Default: pointer:fine device (desktop)
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(pointer: fine)',
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useKeyboardShortcuts', () => {
  describe('desktop gating via matchMedia', () => {
    it('registers listener on pointer:fine devices', () => {
      const addSpy = vi.spyOn(document, 'addEventListener')
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      expect(addSpy).toHaveBeenCalledWith('keydown', expect.any(Function))
    })

    it('does not register listener on touch devices (pointer:coarse)', () => {
      vi.stubGlobal('matchMedia', (_query: string) => ({ matches: false }))
      const addSpy = vi.spyOn(document, 'addEventListener')
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      expect(addSpy).not.toHaveBeenCalled()
    })

    it('does not register listener when matchMedia is unavailable', () => {
      vi.stubGlobal('matchMedia', undefined)
      const addSpy = vi.spyOn(document, 'addEventListener')
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      expect(addSpy).not.toHaveBeenCalled()
    })

    it('does not register listener when enabled=false', () => {
      const addSpy = vi.spyOn(document, 'addEventListener')
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions, false))
      expect(addSpy).not.toHaveBeenCalled()
    })
  })

  describe('listener unregistration', () => {
    it('removes listener on unmount', () => {
      const removeSpy = vi.spyOn(document, 'removeEventListener')
      const actions = makeActions()
      const { unmount } = renderHook(() => useKeyboardShortcuts(actions))
      unmount()
      expect(removeSpy).toHaveBeenCalledWith('keydown', expect.any(Function))
    })
  })

  describe('key → action mapping', () => {
    it('calls onRefresh for "r"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('r')
      expect(actions.onRefresh).toHaveBeenCalledOnce()
    })

    it('calls onMergeFirstReady for "m"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('m')
      expect(actions.onMergeFirstReady).toHaveBeenCalledOnce()
    })

    it('calls onKillFocusedWorker for "k"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('k')
      expect(actions.onKillFocusedWorker).toHaveBeenCalledOnce()
    })

    it('calls onFocusPanel("queue") for "q"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('q')
      expect(actions.onFocusPanel).toHaveBeenCalledWith('queue')
    })

    it('calls onFocusPanel("workers") for "w"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('w')
      expect(actions.onFocusPanel).toHaveBeenCalledWith('workers')
    })

    it('calls onFocusPanel("events") for "e"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('e')
      expect(actions.onFocusPanel).toHaveBeenCalledWith('events')
    })

    it('calls onFocusWorker with 0-based index for digit keys', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('1')
      expect(actions.onFocusWorker).toHaveBeenCalledWith(0)
      fireKey('3')
      expect(actions.onFocusWorker).toHaveBeenCalledWith(2)
      fireKey('6')
      expect(actions.onFocusWorker).toHaveBeenCalledWith(5)
    })

    it('calls onTogglePRModal for "p"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('p')
      expect(actions.onTogglePRModal).toHaveBeenCalledOnce()
    })

    it('calls onShowHelp for "?"', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('?')
      expect(actions.onShowHelp).toHaveBeenCalledOnce()
    })

    it('ignores unknown keys', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('z')
      expect(actions.onRefresh).not.toHaveBeenCalled()
      expect(actions.onMergeFirstReady).not.toHaveBeenCalled()
    })
  })

  describe('input suppression', () => {
    it('ignores keys when target is an INPUT', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      const input = document.createElement('input')
      const event = new KeyboardEvent('keydown', { key: 'r', bubbles: true })
      Object.defineProperty(event, 'target', { value: input })
      document.dispatchEvent(event)
      expect(actions.onRefresh).not.toHaveBeenCalled()
    })

    it('ignores keys when target is a TEXTAREA', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      const textarea = document.createElement('textarea')
      const event = new KeyboardEvent('keydown', { key: 'r', bubbles: true })
      Object.defineProperty(event, 'target', { value: textarea })
      document.dispatchEvent(event)
      expect(actions.onRefresh).not.toHaveBeenCalled()
    })

    it('ignores keys when target is contenteditable', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      const div = document.createElement('div')
      div.contentEditable = 'true'
      const event = new KeyboardEvent('keydown', { key: 'r', bubbles: true })
      Object.defineProperty(event, 'target', { value: div })
      document.dispatchEvent(event)
      expect(actions.onRefresh).not.toHaveBeenCalled()
    })
  })

  describe('modal suppression', () => {
    it('ignores keys when an aria-modal dialog is open', () => {
      const dialog = document.createElement('div')
      dialog.setAttribute('aria-modal', 'true')
      document.body.appendChild(dialog)

      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      fireKey('r')
      expect(actions.onRefresh).not.toHaveBeenCalled()

      document.body.removeChild(dialog)
    })
  })

  describe('key repeat suppression', () => {
    it('ignores repeated keydown events', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      const event = new KeyboardEvent('keydown', { key: 'r', bubbles: true, repeat: true })
      document.dispatchEvent(event)
      expect(actions.onRefresh).not.toHaveBeenCalled()
    })
  })

  describe('modifier key suppression', () => {
    it('ignores Ctrl+key combos', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      const event = new KeyboardEvent('keydown', { key: 'r', ctrlKey: true, bubbles: true })
      document.dispatchEvent(event)
      expect(actions.onRefresh).not.toHaveBeenCalled()
    })

    it('ignores Meta+key combos', () => {
      const actions = makeActions()
      renderHook(() => useKeyboardShortcuts(actions))
      const event = new KeyboardEvent('keydown', { key: 'r', metaKey: true, bubbles: true })
      document.dispatchEvent(event)
      expect(actions.onRefresh).not.toHaveBeenCalled()
    })
  })
})
