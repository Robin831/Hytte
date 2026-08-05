// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WordfeudBoard from './WordfeudBoard'

// ── Translation mock ──────────────────────────────────────────────────────────
// mockT must be a stable reference: handleSolve lists `t` in its dependency
// array, so a fresh function per render would churn the solve callback.

const TRANSLATIONS: Record<string, string> = {
  'board.rack': 'Your rack',
  'board.excludeTile': 'Exclude {{letter}} from suggestions',
  'board.includeTile': 'Include {{letter}} in suggestions again',
  'board.excludeHint': 'Click a tile to leave it out of the suggestions.',
  'board.tilesInPlay': 'Using {{used}} of {{total}} tiles',
  'board.includeAllTiles': 'Use all tiles',
  'board.tilesInBag': '{{count}} of {{total}} tiles in bag',
  'solver.solve': 'Find moves',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  const template = TRANSLATIONS[key] ?? key
  if (!opts) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, name) => String(opts[name] ?? ''))
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

vi.mock('../auth', () => ({
  useAuth: () => ({ user: null }),
}))

// ── i18n mock ─────────────────────────────────────────────────────────────────
// WordfeudBoard pulls in ../utils/formatDate, which imports the real i18n
// instance. Loading that kicks off an HTTP fetch for the locale bundles, which
// has nowhere to go under happy-dom and surfaces as a connection error.

vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

// ── Fetch mock ────────────────────────────────────────────────────────────────

let solveRacks: string[] = []

function mockFetch() {
  return vi.fn(async (url: string, opts?: RequestInit) => {
    const href = String(url)
    if (href.includes('/api/wordfeud/games')) {
      // No connected account — keeps the game list out of the way.
      return { ok: false, status: 400, json: async () => ({}) } as unknown as Response
    }
    if (href.includes('/api/wordfeud/solve')) {
      solveRacks.push(JSON.parse(String(opts?.body)).rack)
      return {
        ok: true,
        status: 200,
        json: async () => ({ moves: [], elapsed_ms: 1 }),
      } as unknown as Response
    }
    throw new Error(`unexpected fetch: ${href}`)
  })
}

// Installed once for the whole file and never removed: the board's mount-time
// game fetch runs inside startTransition, so it can still be in flight after a
// test ends. Restoring the real fetch would let it hit the network.
vi.stubGlobal('fetch', mockFetch())

function renderBoard() {
  return render(
    <MemoryRouter>
      <WordfeudBoard />
    </MemoryRouter>
  )
}

async function typeRack(value: string) {
  const input = screen.getByLabelText('Your rack')
  await act(async () => {
    fireEvent.change(input, { target: { value } })
  })
  return input
}

async function clickSolve() {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Find moves' }))
  })
}

describe('WordfeudBoard rack tile exclusion', () => {
  beforeEach(() => {
    solveRacks = []
  })

  afterEach(() => {
    cleanup()
  })

  it('sends the full rack when no tiles are excluded', async () => {
    renderBoard()
    await typeRack('KTNNDA*')
    await clickSolve()

    await waitFor(() => expect(solveRacks).toEqual(['KTNNDA*']))
  })

  it('withholds an excluded tile from the solve request and re-solves automatically', async () => {
    renderBoard()
    await typeRack('KTNNDA*')
    await clickSolve()
    await waitFor(() => expect(solveRacks).toHaveLength(1))

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Exclude K from suggestions' }))
    })

    // Toggling re-runs the solver with K withheld.
    await waitFor(() => expect(solveRacks).toEqual(['KTNNDA*', 'TNNDA*']))

    // The tile is still shown, now offering to put it back.
    expect(screen.getByRole('button', { name: 'Include K in suggestions again' })).toBeTruthy()
    expect(screen.getByText('Using 6 of 7 tiles')).toBeTruthy()
  })

  it('excludes the blank tile like any other', async () => {
    renderBoard()
    await typeRack('KTNNDA*')
    await clickSolve()
    await waitFor(() => expect(solveRacks).toHaveLength(1))

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Exclude ? from suggestions' }))
    })

    await waitFor(() => expect(solveRacks[1]).toBe('KTNNDA'))
  })

  it('excludes only one of two identical letters', async () => {
    renderBoard()
    await typeRack('KTNNDA*')
    await clickSolve()
    await waitFor(() => expect(solveRacks).toHaveLength(1))

    // Two N tiles are on the rack; clicking the first must leave the second.
    const nTiles = screen.getAllByRole('button', { name: 'Exclude N from suggestions' })
    expect(nTiles).toHaveLength(2)
    await act(async () => {
      fireEvent.click(nTiles[0])
    })

    await waitFor(() => expect(solveRacks[1]).toBe('KTNDA*'))
  })

  it('restores every tile via "use all tiles"', async () => {
    renderBoard()
    await typeRack('KTNNDA*')
    await clickSolve()
    await waitFor(() => expect(solveRacks).toHaveLength(1))

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Exclude K from suggestions' }))
    })
    await waitFor(() => expect(solveRacks).toHaveLength(2))

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Use all tiles' }))
    })

    await waitFor(() => expect(solveRacks[2]).toBe('KTNNDA*'))
  })

  it('clears exclusions when the rack changes', async () => {
    renderBoard()
    await typeRack('KTNNDA*')
    await clickSolve()
    await waitFor(() => expect(solveRacks).toHaveLength(1))

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Exclude K from suggestions' }))
    })
    await waitFor(() => expect(solveRacks).toHaveLength(2))

    // A new rack invalidates the index-based exclusions.
    await typeRack('BCDFGHJ')
    solveRacks.length = 0
    await clickSolve()

    await waitFor(() => expect(solveRacks).toEqual(['BCDFGHJ']))
    expect(screen.queryByRole('button', { name: 'Use all tiles' })).toBeNull()
  })

  it('disables solving when every tile is excluded', async () => {
    renderBoard()
    await typeRack('AB')

    for (const letter of ['A', 'B']) {
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: `Exclude ${letter} from suggestions` }))
      })
    }

    const solveButton = screen.getByRole('button', { name: 'Find moves' }) as HTMLButtonElement
    expect(solveButton.disabled).toBe(true)
    expect(solveRacks).toEqual([])
  })

  it('keeps excluded tiles in the bag/tile tracker count', async () => {
    renderBoard()
    await typeRack('KTNNDA*')

    const before = screen.getByText(/tiles in bag/).textContent

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Exclude K from suggestions' }))
    })

    // An excluded tile is still physically on your rack, so it must not go back
    // into the bag — that would corrupt the bag-empty opponent deduction.
    expect(screen.getByText(/tiles in bag/).textContent).toBe(before)
  })
})
