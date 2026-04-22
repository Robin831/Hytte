// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import MathHeatmap from './MathHeatmap'

// stableT must be a stable reference — MathHeatmap's useEffect has `t` as a
// dependency, so a new function on every render would cause an infinite re-run.
function stableT(key: string): string {
  return key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

type Op = '*' | '/'
type Level = 'unseen' | 'red' | 'yellow' | 'green'

function makeGrid(op: Op) {
  return Array.from({ length: 10 }, (_, row) =>
    Array.from({ length: 10 }, (_, col) => ({
      a: row + 1,
      b: col + 1,
      op,
      count: 0,
      correct_count: 0,
      accuracy_pct: 0,
      avg_ms: 0,
      avg_ms_last5: 0,
      last5: [],
      level: 'unseen' as Level,
    }))
  )
}

function successFetch() {
  return vi.fn(() =>
    Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ multiplication: makeGrid('*'), division: makeGrid('/') }),
    })
  )
}

function renderPage() {
  return render(
    <MemoryRouter>
      <MathHeatmap />
    </MemoryRouter>,
  )
}

describe('MathHeatmap – successful fetch', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders a 10×10 grid after successful fetch', async () => {
    vi.stubGlobal('fetch', successFetch())
    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tabpanel')).toBeInTheDocument()
    })

    const panel = screen.getByRole('tabpanel')
    expect(within(panel).getAllByRole('button')).toHaveLength(100)
  })

  it('tab switch shows division grid', async () => {
    vi.stubGlobal('fetch', successFetch())
    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('tabpanel')).toBeInTheDocument()
    })

    // Multiplication tab is active by default — first cell is 1×1
    expect(within(screen.getByRole('tabpanel')).getByText('1×1')).toBeInTheDocument()

    // Switch to division tab
    fireEvent.click(screen.getByRole('tab', { name: 'heatmap.tabDivision' }))

    // Division panel is now active — first cell is 1÷1 (a=1, b=1: (1*1)÷1)
    await waitFor(() => {
      expect(within(screen.getByRole('tabpanel')).getByText('1÷1')).toBeInTheDocument()
    })
  })
})

describe('MathHeatmap – error state', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows heatmap.errorLoad when fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderPage()

    await waitFor(() => {
      expect(screen.getByText('heatmap.errorLoad')).toBeInTheDocument()
    })
  })
})
