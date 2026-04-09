// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import HomePage from './HomePage'

// ── Translation mock ──────────────────────────────────────────────────────────

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { name?: string }) => {
      const map: Record<string, string> = {
        'greeting.morning': 'Good morning!',
        'greeting.afternoon': 'Good afternoon!',
        'greeting.evening': 'Good evening!',
        'greeting.morningNamed': `Good morning, ${opts?.name}!`,
        'greeting.afternoonNamed': `Good afternoon, ${opts?.name}!`,
        'greeting.eveningNamed': `Good evening, ${opts?.name}!`,
      }
      return map[key] ?? key
    },
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Wednesday, April 9, 2026',
  formatTime: () => '08:00:00',
  formatNumber: (n: number) => String(n),
}))

// ── useNow mock ───────────────────────────────────────────────────────────────

const mockNow = { value: new Date('2026-04-09T08:00:00') }

vi.mock('../hooks/useNow', () => ({
  useNow: () => mockNow.value,
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

interface MockUser {
  name: string
  picture?: string
}

const authState: { user: MockUser | null; hasFeature: (f: string) => boolean } = {
  user: null,
  hasFeature: () => false,
}

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

vi.mock('../components/home/WeatherCard', () => ({
  default: () => null,
}))

vi.mock('../components/home/TodayScheduleCard', () => ({
  default: () => null,
}))

vi.mock('../components/home/StridePlanCard', () => ({
  default: () => <div data-testid="stride-plan-card" />,
}))

vi.mock('../components/home/WorkHoursCard', () => ({
  default: () => <div data-testid="work-hours-card" />,
}))

vi.mock('../components/home/BudgetSnapshotCard', () => ({
  default: () => <div data-testid="budget-snapshot-card" />,
}))

function setAuth(user: MockUser | null, hasFeature: (f: string) => boolean = () => false) {
  authState.user = user
  authState.hasFeature = hasFeature
}

function renderPage() {
  return render(
    <MemoryRouter>
      <HomePage />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('HomePage – greeting key selection', () => {
  afterEach(() => {
    vi.clearAllMocks()
    authState.user = null
  })

  it('shows morning greeting (unnamed) when hour < 12 and no user', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth(null)
    renderPage()
    expect(screen.getByText('Good morning!')).toBeInTheDocument()
  })

  it('shows afternoon greeting (unnamed) when 12 ≤ hour < 17 and no user', () => {
    mockNow.value = new Date('2026-04-09T14:00:00')
    setAuth(null)
    renderPage()
    expect(screen.getByText('Good afternoon!')).toBeInTheDocument()
  })

  it('shows evening greeting (unnamed) when hour ≥ 17 and no user', () => {
    mockNow.value = new Date('2026-04-09T20:00:00')
    setAuth(null)
    renderPage()
    expect(screen.getByText('Good evening!')).toBeInTheDocument()
  })

  it('shows named morning greeting when user is logged in', () => {
    mockNow.value = new Date('2026-04-09T09:00:00')
    setAuth({ name: 'Robin Smith' })
    renderPage()
    expect(screen.getByText('Good morning, Robin!')).toBeInTheDocument()
  })

  it('shows named afternoon greeting when user is logged in', () => {
    mockNow.value = new Date('2026-04-09T15:00:00')
    setAuth({ name: 'Robin Smith' })
    renderPage()
    expect(screen.getByText('Good afternoon, Robin!')).toBeInTheDocument()
  })

  it('shows named evening greeting when user is logged in', () => {
    mockNow.value = new Date('2026-04-09T18:00:00')
    setAuth({ name: 'Robin Smith' })
    renderPage()
    expect(screen.getByText('Good evening, Robin!')).toBeInTheDocument()
  })
})

describe('HomePage – clock rendering', () => {
  afterEach(() => {
    vi.clearAllMocks()
    authState.user = null
  })

  it('renders the formatted time string', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth(null)
    renderPage()
    expect(screen.getByText('08:00:00')).toBeInTheDocument()
  })

  it('renders the formatted date string', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth(null)
    renderPage()
    expect(screen.getByText('Wednesday, April 9, 2026')).toBeInTheDocument()
  })
})

describe('HomePage – avatar fallback', () => {
  afterEach(() => {
    vi.clearAllMocks()
    authState.user = null
  })

  it('renders initial letter avatar when user has no picture', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' })
    renderPage()
    expect(screen.getByRole('img', { name: 'Robin Smith' })).toBeInTheDocument()
    expect(screen.getByText('R')).toBeInTheDocument()
  })

  it('renders img avatar when user has a picture', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith', picture: 'https://example.com/avatar.jpg' })
    renderPage()
    const img = screen.getByAltText('Robin Smith') as HTMLImageElement
    expect(img.tagName).toBe('IMG')
    expect(img.src).toBe('https://example.com/avatar.jpg')
  })

  it('renders no avatar when user is not logged in', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth(null)
    renderPage()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
  })
})

describe('HomePage – feature-flagged cards', () => {
  afterEach(() => {
    vi.clearAllMocks()
    authState.user = null
  })

  it('does not render StridePlanCard when stride feature is disabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, () => false)
    renderPage()
    expect(screen.queryByTestId('stride-plan-card')).not.toBeInTheDocument()
  })

  it('renders StridePlanCard when stride feature is enabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, (f) => f === 'stride')
    renderPage()
    expect(screen.getByTestId('stride-plan-card')).toBeInTheDocument()
  })

  it('does not render WorkHoursCard when work_hours feature is disabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, () => false)
    renderPage()
    expect(screen.queryByTestId('work-hours-card')).not.toBeInTheDocument()
  })

  it('renders WorkHoursCard when work_hours feature is enabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, (f) => f === 'work_hours')
    renderPage()
    expect(screen.getByTestId('work-hours-card')).toBeInTheDocument()
  })

  it('renders both cards when both features are enabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, () => true)
    renderPage()
    expect(screen.getByTestId('stride-plan-card')).toBeInTheDocument()
    expect(screen.getByTestId('work-hours-card')).toBeInTheDocument()
  })

  it('does not render BudgetSnapshotCard when budget feature is disabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, () => false)
    renderPage()
    expect(screen.queryByTestId('budget-snapshot-card')).not.toBeInTheDocument()
  })

  it('renders BudgetSnapshotCard when budget feature is enabled', () => {
    mockNow.value = new Date('2026-04-09T08:00:00')
    setAuth({ name: 'Robin Smith' }, (f) => f === 'budget')
    renderPage()
    expect(screen.getByTestId('budget-snapshot-card')).toBeInTheDocument()
  })
})
