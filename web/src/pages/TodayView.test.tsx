// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TodayView from './TodayView'
import enToday from '../../public/locales/en/today.json'

// ── Translation mock ──────────────────────────────────────────────────────────

vi.mock('react-i18next', () => ({
  useTranslation: (ns?: string) => ({
    t: (key: string) => {
      if (ns !== 'today') return key
      const parts = key.split('.')
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let val: any = enToday
      for (const part of parts) {
        val = val?.[part]
      }
      return typeof val === 'string' ? val : key
    },
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../utils/formatDate', () => ({
  formatDate: () => 'Monday, January 1',
  formatTime: () => '12:00',
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────

type FamilyStatus = { is_parent: boolean; is_child: boolean } | null
interface MockAuthState {
  user: object | null
  familyStatus: FamilyStatus
}

const authState: MockAuthState = { user: null, familyStatus: null }

vi.mock('../auth', () => ({
  useAuth: () => authState,
}))

function setAuth(user: object | null, familyStatus: FamilyStatus) {
  authState.user = user
  authState.familyStatus = familyStatus
}

function renderPage() {
  return render(
    <MemoryRouter>
      <TodayView />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('TodayView – role label', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('shows Guest role label when user is not logged in', () => {
    setAuth(null, null)
    renderPage()
    expect(screen.getByText('Guest')).toBeInTheDocument()
  })

  it('shows Parent role label when familyStatus.is_parent is true', () => {
    setAuth({ id: 1 }, { is_parent: true, is_child: false })
    renderPage()
    expect(screen.getByText('Parent')).toBeInTheDocument()
  })

  it('shows Kid role label when familyStatus.is_child is true', () => {
    setAuth({ id: 1 }, { is_parent: false, is_child: true })
    renderPage()
    expect(screen.getByText('Kid')).toBeInTheDocument()
  })

  it('shows Guest role label when user is authenticated but neither parent nor child', () => {
    setAuth({ id: 1 }, { is_parent: false, is_child: false })
    renderPage()
    expect(screen.getByText('Guest')).toBeInTheDocument()
  })
})

describe('TodayView – widget sets', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders parent widgets (Weather, Training, Budget) for parent role', () => {
    setAuth({ id: 1 }, { is_parent: true, is_child: false })
    renderPage()
    expect(screen.getByText('Weather')).toBeInTheDocument()
    expect(screen.getByText('Training')).toBeInTheDocument()
    expect(screen.getByText('Budget')).toBeInTheDocument()
  })

  it('renders kid widgets (Stars, Chores) for kid role', () => {
    setAuth({ id: 1 }, { is_parent: false, is_child: true })
    renderPage()
    expect(screen.getByText('Stars')).toBeInTheDocument()
    expect(screen.getByText('Chores')).toBeInTheDocument()
  })

  it('renders guest widgets (Weather, Calendar) without Training for guest role', () => {
    setAuth(null, null)
    renderPage()
    expect(screen.getByText('Weather')).toBeInTheDocument()
    expect(screen.getByText('Calendar')).toBeInTheDocument()
    expect(screen.queryByText('Training')).not.toBeInTheDocument()
  })
})
