// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { WorkerInfo } from '../hooks/useForgeStatus'

// ── i18n mock ───────────────────────────────────────────────────────────────
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Auth mock ───────────────────────────────────────────────────────────────
vi.mock('../auth', () => ({
  useAuth: () => ({ user: { name: 'Test', is_admin: true, features: {} } }),
}))

// ── Worker data (mutable so we can simulate polling updates) ──────────────────
let workersData: WorkerInfo[] = []

vi.mock('../hooks/useForgeStatus', () => ({
  useForgeStatus: () => ({ status: null, error: null, loading: false, refresh: vi.fn() }),
  useForgeWorkers: () => ({ workers: workersData, loading: false, error: null, refresh: vi.fn() }),
}))

vi.mock('../hooks/useAllPRs', () => ({
  useAllPRs: () => ({ data: null }),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => ({ toasts: [], showToast: vi.fn() }),
}))

vi.mock('../hooks/usePanelCollapse', () => ({
  usePanelCollapse: () => [true, vi.fn()],
}))

// ── Child components ──────────────────────────────────────────────────────────
// WorkersCard: render a clickable button per worker so we can drive selection.
vi.mock('../components/WorkersCard', () => ({
  default: ({
    workers,
    selectedWorkerId,
    onSelectWorker,
  }: {
    workers: WorkerInfo[]
    selectedWorkerId: string | null
    onSelectWorker: (id: string) => void
  }) => (
    <div>
      <span data-testid="selected-id">{selectedWorkerId ?? 'none'}</span>
      {workers.map(w => (
        <button key={w.id} data-testid={`select-${w.id}`} onClick={() => onSelectWorker(w.id)}>
          {w.bead_id}
        </button>
      ))}
    </div>
  ),
}))

// LiveActivity: expose which worker the panel is showing.
vi.mock('../components/LiveActivity', () => ({
  default: ({ selectedWorker }: { selectedWorker: WorkerInfo | null }) => (
    <div data-testid="live-worker">{selectedWorker?.id ?? 'none'}</div>
  ),
}))

// Remaining cards / chrome are irrelevant to selection logic — stub them out.
vi.mock('../components/NeedsAttentionCard', () => ({ default: () => null }))
vi.mock('../components/ReadyToMergeCard', () => ({ default: () => null }))
vi.mock('../components/RecentlyClosedPRsCard', () => ({ default: () => null }))
vi.mock('../components/TodayStatsCard', () => ({ default: () => null }))
vi.mock('../components/CostsDashboardCard', () => ({ default: () => null }))
vi.mock('../components/FullQueueCard', () => ({ default: () => null }))
vi.mock('../components/ReleaseCard', () => ({ default: () => null }))
vi.mock('../components/ConfirmDialog', () => ({ default: () => null }))
vi.mock('../components/ToastList', () => ({ default: () => null }))
vi.mock('../components/BeadDetailModal', () => ({ default: () => null }))
vi.mock('../components/ResizePanelHandle', () => ({ ResizePanelHandle: () => null }))

import ForgeDashboardPage from './ForgeDashboardPage'

function makeWorker(over: Partial<WorkerInfo> & { id: string }): WorkerInfo {
  return {
    bead_id: `bead-${over.id}`,
    anvil: 'anvil',
    branch: 'branch',
    pid: 1,
    status: 'running',
    phase: 'smith',
    title: 'title',
    started_at: '2026-05-28T10:00:00Z',
    log_path: '/tmp/log',
    pr_number: 0,
    ...over,
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ForgeDashboardPage />
    </MemoryRouter>,
  )
}

describe('ForgeDashboardPage selectedWorkerId stickiness', () => {
  beforeEach(() => {
    workersData = []
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}) })))
    localStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps an explicitly selected worker pinned when a new active worker starts', () => {
    // One active worker A.
    workersData = [makeWorker({ id: 'A', status: 'running', started_at: '2026-05-28T10:00:00Z' })]
    const { rerender } = renderPage()

    // Default selection is the only active worker.
    expect(screen.getByTestId('live-worker')).toHaveTextContent('A')

    // User explicitly clicks worker A.
    fireEvent.click(screen.getByTestId('select-A'))
    expect(screen.getByTestId('live-worker')).toHaveTextContent('A')

    // A completes and a brand-new active worker B starts.
    workersData = [
      makeWorker({ id: 'A', status: 'completed', completed_at: '2026-05-28T10:05:00Z', started_at: '2026-05-28T10:00:00Z' }),
      makeWorker({ id: 'B', status: 'running', started_at: '2026-05-28T10:06:00Z' }),
    ]
    rerender(
      <MemoryRouter>
        <ForgeDashboardPage />
      </MemoryRouter>,
    )

    // Selection must stay on the completed worker the user pinned.
    expect(screen.getByTestId('live-worker')).toHaveTextContent('A')
  })

  it('falls back to the newest active worker when the selected worker disappears', () => {
    workersData = [makeWorker({ id: 'A', status: 'running', started_at: '2026-05-28T10:00:00Z' })]
    const { rerender } = renderPage()

    fireEvent.click(screen.getByTestId('select-A'))
    expect(screen.getByTestId('live-worker')).toHaveTextContent('A')

    // A vanishes entirely; B and C are active (C started most recently).
    workersData = [
      makeWorker({ id: 'B', status: 'running', started_at: '2026-05-28T10:01:00Z' }),
      makeWorker({ id: 'C', status: 'running', started_at: '2026-05-28T10:09:00Z' }),
    ]
    rerender(
      <MemoryRouter>
        <ForgeDashboardPage />
      </MemoryRouter>,
    )

    expect(screen.getByTestId('live-worker')).toHaveTextContent('C')
  })

  it('falls back to the newest completed worker when none are active and the selection vanished', () => {
    workersData = [makeWorker({ id: 'A', status: 'running', started_at: '2026-05-28T10:00:00Z' })]
    const { rerender } = renderPage()

    fireEvent.click(screen.getByTestId('select-A'))

    // A vanishes; only completed workers remain.
    workersData = [
      makeWorker({ id: 'X', status: 'completed', completed_at: '2026-05-28T09:00:00Z', started_at: '2026-05-28T08:00:00Z' }),
      makeWorker({ id: 'Y', status: 'completed', completed_at: '2026-05-28T11:00:00Z', started_at: '2026-05-28T10:30:00Z' }),
    ]
    rerender(
      <MemoryRouter>
        <ForgeDashboardPage />
      </MemoryRouter>,
    )

    expect(screen.getByTestId('live-worker')).toHaveTextContent('Y')
  })

  it('uses the newest active worker by default with no manual selection', () => {
    workersData = [
      makeWorker({ id: 'old', status: 'running', started_at: '2026-05-28T10:00:00Z' }),
      makeWorker({ id: 'new', status: 'running', started_at: '2026-05-28T10:08:00Z' }),
    ]
    renderPage()

    expect(screen.getByTestId('live-worker')).toHaveTextContent('new')
  })
})
