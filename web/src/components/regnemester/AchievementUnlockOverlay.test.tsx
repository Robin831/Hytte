// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, fireEvent, act, screen } from '@testing-library/react'
import { AchievementUnlockOverlay } from './AchievementUnlockOverlay'
import {
  emitAchievementUnlock,
  _resetAchievementHandlers,
} from '../../lib/regnemester/feedback/achievementEvents'
import { soundEngine } from '../../lib/regnemester/feedback/sound'
import type { UnlockedAchievement } from '../math/UnlockedAchievements'

// Pass-through translator keeps assertions readable without needing the
// real i18next instance loaded. Interpolation mirrors how react-i18next
// would render templates with {{tier}} / {{count}} placeholders.
function stableT(key: string, opts?: Record<string, unknown>): string {
  if (!opts) return key
  let out = key
  for (const [k, v] of Object.entries(opts)) {
    out = out.replace(new RegExp(`{{${k}}}`, 'g'), String(v))
  }
  return out
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en', exists: () => false },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function makeAch(code: string, unlockedAt = '2026-04-22T00:00:00Z'): UnlockedAchievement {
  return {
    code,
    title: `title:${code}`,
    description: `desc:${code}`,
    tier: 'marathon',
    unlocked_at: unlockedAt,
  }
}

function installMatchMedia(reduce: boolean) {
  const mql = {
    matches: reduce,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }
  Object.defineProperty(window, 'matchMedia', { value: vi.fn(() => mql), configurable: true })
}

describe('AchievementUnlockOverlay', () => {
  beforeEach(() => {
    installMatchMedia(false)
    vi.useFakeTimers()
    vi.spyOn(soundEngine, 'play').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    _resetAchievementHandlers()
  })

  it('renders nothing when no unlocks have been emitted', () => {
    render(<AchievementUnlockOverlay />)
    expect(screen.queryByTestId('regnemester-achievement-overlay')).toBeNull()
  })

  it('renders the first unlock when an event fires', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    expect(screen.getByTestId('regnemester-achievement-overlay')).toBeTruthy()
    expect(screen.getByText('title:streak_25')).toBeTruthy()
    expect(screen.getByText('desc:streak_25')).toBeTruthy()
  })

  it('plays fanfare and vibrates on mount per unlock', () => {
    const vibrateSpy = vi.fn().mockReturnValue(true)
    Object.defineProperty(navigator, 'vibrate', { value: vibrateSpy, configurable: true })
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    expect(soundEngine.play).toHaveBeenCalledWith('fanfare')
    expect(vibrateSpy).toHaveBeenCalledWith([40, 50, 40, 50, 80])
  })

  it('queues multiple unlocks and shows them sequentially', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25'), makeAch('marathon_sub_5')])
    })
    expect(screen.getByText('title:streak_25')).toBeTruthy()

    // Dismiss the first; after the exit animation, the next should show.
    act(() => {
      fireEvent.click(screen.getByRole('button'))
    })
    act(() => { vi.advanceTimersByTime(200) })

    expect(screen.getByText('title:marathon_sub_5')).toBeTruthy()
  })

  it('auto-advances after the hold window elapses', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25'), makeAch('streak_50')])
    })
    expect(screen.getByText('title:streak_25')).toBeTruthy()
    act(() => { vi.advanceTimersByTime(4200) })
    act(() => { vi.advanceTimersByTime(200) })
    expect(screen.getByText('title:streak_50')).toBeTruthy()
  })

  it('dismisses on Escape key press', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    })
    act(() => { vi.advanceTimersByTime(200) })
    expect(screen.queryByTestId('regnemester-achievement-overlay')).toBeNull()
  })

  it('dismisses on Enter key press', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    })
    act(() => { vi.advanceTimersByTime(200) })
    expect(screen.queryByTestId('regnemester-achievement-overlay')).toBeNull()
  })

  it('dismisses on backdrop tap but not on card tap', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    // Tapping the card should NOT dismiss thanks to stopPropagation.
    const heading = screen.getByText('title:streak_25')
    act(() => { fireEvent.click(heading) })
    expect(screen.queryByTestId('regnemester-achievement-overlay')).toBeTruthy()

    // Tapping the backdrop dismisses.
    const backdrop = screen.getByTestId('regnemester-achievement-overlay')
    act(() => { fireEvent.click(backdrop) })
    act(() => { vi.advanceTimersByTime(200) })
    expect(screen.queryByTestId('regnemester-achievement-overlay')).toBeNull()
  })

  it('rapid double-click does not skip an extra entry in the queue', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('a'), makeAch('b'), makeAch('c')])
    })
    const backdrop = screen.getByTestId('regnemester-achievement-overlay')
    act(() => {
      fireEvent.click(backdrop)
      fireEvent.click(backdrop) // second click during exit animation
    })
    act(() => { vi.advanceTimersByTime(200) })
    // Only one entry should have been consumed; 'b' is up next.
    expect(screen.getByText('title:b')).toBeTruthy()
  })

  it('shows remaining-count hint when more unlocks are queued', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('a'), makeAch('b'), makeAch('c')])
    })
    // Two more to go behind the current one.
    expect(screen.getByText(/2 more to go/)).toBeTruthy()
  })

  it('moves focus to the dismiss button when the overlay opens', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    const button = screen.getByRole('button')
    expect(document.activeElement).toBe(button)
  })

  it('restores focus to the previously focused element when closed', () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'trigger'
    document.body.appendChild(trigger)
    try {
      trigger.focus()
      expect(document.activeElement).toBe(trigger)

      render(<AchievementUnlockOverlay />)
      act(() => {
        emitAchievementUnlock([makeAch('streak_25')])
      })
      // Focus moved into the dialog.
      expect(document.activeElement).not.toBe(trigger)

      act(() => {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      })
      act(() => { vi.advanceTimersByTime(200) })

      expect(screen.queryByTestId('regnemester-achievement-overlay')).toBeNull()
      expect(document.activeElement).toBe(trigger)
    } finally {
      document.body.removeChild(trigger)
    }
  })

  it('keeps focus inside the dialog when Tab is pressed', () => {
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    const button = screen.getByRole('button')
    expect(document.activeElement).toBe(button)

    const tabEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true,
    })
    act(() => {
      window.dispatchEvent(tabEvent)
    })
    expect(tabEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(button)

    const shiftTabEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true,
    })
    act(() => {
      window.dispatchEvent(shiftTabEvent)
    })
    expect(shiftTabEvent.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(button)
  })

  it('restores focus only after the full queue has drained', () => {
    const trigger = document.createElement('button')
    trigger.textContent = 'trigger'
    document.body.appendChild(trigger)
    try {
      trigger.focus()

      render(<AchievementUnlockOverlay />)
      act(() => {
        emitAchievementUnlock([makeAch('a'), makeAch('b')])
      })

      // Dismiss the first — focus should stay inside the dialog on the
      // next unlock, not return to the trigger yet.
      act(() => {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      })
      act(() => { vi.advanceTimersByTime(200) })
      expect(document.activeElement).not.toBe(trigger)
      expect(screen.getByText('title:b')).toBeTruthy()

      // Dismiss the second — now the queue is empty and focus restores.
      act(() => {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
      })
      act(() => { vi.advanceTimersByTime(200) })
      expect(document.activeElement).toBe(trigger)
    } finally {
      document.body.removeChild(trigger)
    }
  })

  it('skips vibration when prefers-reduced-motion is on', () => {
    installMatchMedia(true)
    const vibrateSpy = vi.fn().mockReturnValue(true)
    Object.defineProperty(navigator, 'vibrate', { value: vibrateSpy, configurable: true })
    render(<AchievementUnlockOverlay />)
    act(() => {
      emitAchievementUnlock([makeAch('streak_25')])
    })
    // The haptics helper is the one that guards on reduced-motion; the
    // fanfare should still play.
    expect(soundEngine.play).toHaveBeenCalledWith('fanfare')
    expect(vibrateSpy).not.toHaveBeenCalled()
  })
})
