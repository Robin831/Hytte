// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Settings from '../Settings'

// i18n: return the key verbatim so we can assert on section headings.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// Replace each section with a lightweight marker so the test targets the
// orchestrator's gating/composition, not the sections' internals.
vi.mock('../settings/ProfileSection', () => ({ default: () => <div data-testid="profile-section" /> }))
vi.mock('../settings/TrainingSection', () => ({ default: () => <div data-testid="training-section" /> }))
vi.mock('../settings/NotificationsSection', () => ({ default: () => <div data-testid="notifications-section" /> }))
vi.mock('../settings/SecuritySection', () => ({ default: () => <div data-testid="security-section" /> }))
vi.mock('../settings/IntegrationsSection', () => ({ default: () => <div data-testid="integrations-section" /> }))
vi.mock('../settings/PokemonSection', () => ({ default: () => <div data-testid="pokemon-section" /> }))
vi.mock('../settings/AIAutomationSection', () => ({ default: () => <div data-testid="ai-automation-section" /> }))
vi.mock('../settings/KioskTokensSection', () => ({ default: () => <div data-testid="kiosk-tokens-section" /> }))

interface MockUser {
  id: number
  name: string
  email: string
  picture: string
  created_at: string
  is_admin: boolean
  features: Record<string, boolean>
}

const authState: {
  user: MockUser | null
  loading: boolean
  hasFeature: (key: string) => boolean
  familyStatus: { is_parent: boolean; is_child: boolean } | null
} = {
  user: null,
  loading: false,
  hasFeature: () => false,
  familyStatus: null,
}

vi.mock('../../auth', () => ({
  useAuth: () => ({
    ...authState,
    logout: async () => {},
    refreshFamilyStatus: async () => {},
  }),
}))

function makeUser(overrides: Partial<MockUser> = {}): MockUser {
  return {
    id: 1,
    name: 'Test User',
    email: 'test@example.com',
    picture: '',
    created_at: '2024-01-01T00:00:00Z',
    is_admin: false,
    features: {},
    ...overrides,
  }
}

function renderSettings() {
  return render(
    <MemoryRouter>
      <Settings />
    </MemoryRouter>,
  )
}

describe('Settings – section gating', () => {
  beforeEach(() => {
    authState.user = null
    authState.loading = false
    authState.hasFeature = () => false
    authState.familyStatus = null
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ preferences: {} }),
    })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders all sections for an admin with the pokemon feature', async () => {
    authState.user = makeUser({ is_admin: true, features: { pokemon: true } })
    authState.hasFeature = (key: string) => key === 'pokemon'
    renderSettings()

    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())
    expect(screen.getByTestId('training-section')).toBeInTheDocument()
    expect(screen.getByTestId('notifications-section')).toBeInTheDocument()
    expect(screen.getByTestId('security-section')).toBeInTheDocument()
    expect(screen.getByTestId('integrations-section')).toBeInTheDocument()
    expect(screen.getByTestId('pokemon-section')).toBeInTheDocument()
    expect(screen.getByTestId('ai-automation-section')).toBeInTheDocument()
    expect(screen.getByTestId('kiosk-tokens-section')).toBeInTheDocument()
  })

  it('hides training, notifications, integrations, and admin sections for a child account', async () => {
    authState.user = makeUser({ features: { kids_stars: true } })
    authState.familyStatus = { is_parent: false, is_child: true }
    renderSettings()

    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())
    expect(screen.getByTestId('security-section')).toBeInTheDocument()
    expect(screen.queryByTestId('training-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('notifications-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('integrations-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('ai-automation-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-tokens-section')).not.toBeInTheDocument()
  })

  it('shows integrations for a non-admin with the infra feature but hides admin-only sections', async () => {
    authState.user = makeUser({ features: { infra: true } })
    authState.hasFeature = (key: string) => key === 'infra'
    renderSettings()

    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())
    expect(screen.getByTestId('training-section')).toBeInTheDocument()
    expect(screen.getByTestId('notifications-section')).toBeInTheDocument()
    expect(screen.getByTestId('integrations-section')).toBeInTheDocument()
    expect(screen.queryByTestId('pokemon-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('ai-automation-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-tokens-section')).not.toBeInTheDocument()
  })
})
