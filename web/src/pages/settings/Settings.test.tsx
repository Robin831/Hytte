// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { StrictMode } from 'react'
import { MemoryRouter } from 'react-router'
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
// ProfileSection echoes the theme preference so tests can assert the sections
// render against the *fetched* map, not an empty one.
vi.mock('../settings/ProfileSection', () => ({
  default: ({ preferences }: { preferences: Record<string, string> }) => (
    <div data-testid="profile-section" data-theme={preferences.theme ?? ''} />
  ),
}))
// TrainingSection echoes hasStride so the Stride feature gate on its switches is
// observable here — dropping, inverting or misspelling the prop fails a test.
vi.mock('../settings/TrainingSection', () => ({
  default: ({ hasStride }: { hasStride: boolean }) => (
    <div data-testid="training-section" data-has-stride={String(hasStride)} />
  ),
}))
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

  it('tells TrainingSection to show the Stride block for a user with the stride feature', async () => {
    authState.user = makeUser({ features: { stride: true } })
    authState.hasFeature = (key: string) => key === 'stride'
    renderSettings()

    await waitFor(() => expect(screen.getByTestId('training-section')).toBeInTheDocument())
    expect(screen.getByTestId('training-section')).toHaveAttribute('data-has-stride', 'true')
  })

  it('shows integrations for a non-admin with the infra feature but hides admin-only sections', async () => {
    authState.user = makeUser({ features: { infra: true } })
    authState.hasFeature = (key: string) => key === 'infra'
    renderSettings()

    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())
    expect(screen.getByTestId('training-section')).toBeInTheDocument()
    // No stride feature: TrainingSection must be told to hide its Stride block.
    expect(screen.getByTestId('training-section')).toHaveAttribute('data-has-stride', 'false')
    expect(screen.getByTestId('notifications-section')).toBeInTheDocument()
    expect(screen.getByTestId('integrations-section')).toBeInTheDocument()
    expect(screen.queryByTestId('pokemon-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('ai-automation-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('kiosk-tokens-section')).not.toBeInTheDocument()
  })
})

describe('Settings – preference load failures', () => {
  beforeEach(() => {
    authState.user = makeUser()
    authState.loading = false
    authState.hasFeature = () => false
    authState.familyStatus = null
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  it('renders the sections from the fetched preference map on the happy path', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ preferences: { theme: 'dark' } }),
    })))
    renderSettings()

    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())
    expect(screen.getByTestId('profile-section')).toHaveAttribute('data-theme', 'dark')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows the error panel instead of the sections when the response is not OK', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) })))
    renderSettings()

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByText('loadError.title')).toBeInTheDocument()
    expect(screen.queryByTestId('profile-section')).not.toBeInTheDocument()
    expect(screen.queryByTestId('security-section')).not.toBeInTheDocument()
  })

  it('shows the error panel when the request throws', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new Error('network down')
    }))
    renderSettings()

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    expect(screen.getByText('loadError.title')).toBeInTheDocument()
    expect(screen.queryByTestId('profile-section')).not.toBeInTheDocument()
  })

  it('clears the error panel and renders the sections after a successful retry', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ preferences: { theme: 'light' } }),
      })
    vi.stubGlobal('fetch', fetchMock)
    renderSettings()

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'loadError.retry' }))

    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByTestId('profile-section')).toHaveAttribute('data-theme', 'light')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('ignores a superseded load that resolves after a newer one (StrictMode double-mount)', async () => {
    // The first mount's load is held open past the effect cleanup, which bumps
    // loadSeq. Its late failure must not clobber the second load's result.
    let releaseStale: () => void = () => {}
    const staleGate = new Promise<void>((resolve) => {
      releaseStale = resolve
    })
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(async () => {
        await staleGate
        return { ok: false, status: 500, json: async () => ({}) }
      })
      .mockImplementationOnce(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ preferences: { theme: 'dark' } }),
      }))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <StrictMode>
        <MemoryRouter>
          <Settings />
        </MemoryRouter>
      </StrictMode>,
    )

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByTestId('profile-section')).toBeInTheDocument())

    await act(async () => {
      releaseStale()
      await staleGate
    })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByTestId('profile-section')).toHaveAttribute('data-theme', 'dark')
  })
})
