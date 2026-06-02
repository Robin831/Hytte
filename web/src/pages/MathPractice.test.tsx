// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import MathPractice from './MathPractice'

// stableT must be a stable reference — the page's effects don't depend on `t`,
// but keeping it stable mirrors the other math page tests and avoids surprises.
function stableT(key: string, opts?: Record<string, unknown>): string {
  if (opts && typeof opts.count === 'number') return `${key}:${opts.count}`
  if (opts && typeof opts.answer !== 'undefined') return `${key}:${opts.answer}`
  return key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// The feedback hook touches audio + preferences; stub it out entirely.
vi.mock('../lib/regnemester/feedback', () => ({
  useFeedback: () => ({
    play: () => {},
    flashCorrect: () => {},
    flashWrong: () => {},
    flashMilestone: () => {},
    vibrate: () => {},
    vibrateCorrect: () => {},
    vibrateWrong: () => {},
    muted: false,
    toggleMute: () => {},
    setMuted: () => {},
  }),
}))

type Op = '*' | '/'
type Level = 'unseen' | 'red' | 'yellow' | 'green'

// Build a stats grid where the cell at [row][col] gets the supplied level
// (defaults to all-green so the weakest set is empty unless overridden).
function makeGrid(op: Op, overrides: Record<string, Level> = {}, base: Level = 'green') {
  return Array.from({ length: 10 }, (_, row) =>
    Array.from({ length: 10 }, (_, col) => ({
      a: row + 1,
      b: col + 1,
      op,
      level: overrides[`${row}:${col}`] ?? base,
    })),
  )
}

function renderPage() {
  return render(
    <MemoryRouter>
      <MathPractice />
    </MemoryRouter>,
  )
}

describe('MathPractice – weakest facts derivation', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('counts red and yellow multiplication cells as weak', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({
          multiplication: makeGrid('*', { '0:0': 'red', '1:1': 'yellow' }),
          division: makeGrid('/'),
        }),
      }),
    ))
    renderPage()

    // Multiplication is the default operation; weakest is the default selection.
    await waitFor(() => {
      expect(screen.getByText('practice.weakestCount:2')).toBeInTheDocument()
    })
  })

  it('shows the empty fallback when there are no weak facts', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({
          multiplication: makeGrid('*'),
          division: makeGrid('/'),
        }),
      }),
    ))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('practice.weakestEmpty')).toBeInTheDocument()
    })
  })
})

describe('MathPractice – session lifecycle', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('starts a session and posts an attempt for a specific fact', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/math/stats') {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ multiplication: makeGrid('*'), division: makeGrid('/') }),
        })
      }
      if (url === '/api/math/sessions' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ session_id: 42 }) })
      }
      if (url === '/api/math/sessions/42/attempts') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ is_correct: true }) })
      }
      return Promise.resolve({ ok: false, json: () => Promise.resolve({}) })
    })
    vi.stubGlobal('fetch', fetchMock)
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('practice.weakestEmpty')).toBeInTheDocument()
    })

    // Switch to a specific fact so the drilled fact is deterministic (1 × 1 = 1).
    fireEvent.click(screen.getByText('practice.specific'))
    fireEvent.click(screen.getByText('practice.start'))

    await waitFor(() => {
      const startCall = fetchMock.mock.calls.find(c => c[0] === '/api/math/sessions')
      expect(startCall).toBeTruthy()
      expect(JSON.parse((startCall![1] as RequestInit).body as string)).toEqual({ mode: 'mult' })
    })

    // Answer "1" and submit via the keypad.
    await waitFor(() => expect(screen.getByText('1')).toBeInTheDocument())
    fireEvent.click(screen.getByText('1'))
    fireEvent.click(screen.getByLabelText('keypad.enterAria'))

    await waitFor(() => {
      const attemptCall = fetchMock.mock.calls.find(c => c[0] === '/api/math/sessions/42/attempts')
      expect(attemptCall).toBeTruthy()
      const body = JSON.parse((attemptCall![1] as RequestInit).body as string)
      expect(body).toMatchObject({ a: 1, b: 1, op: '*', user_answer: 1 })
    })
  })
})
