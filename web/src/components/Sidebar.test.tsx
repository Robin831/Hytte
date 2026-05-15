// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Sidebar from './Sidebar'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('./LanguageSwitcher', () => ({
  default: () => null,
}))

interface MockUser {
  name: string
  email: string
  picture?: string
  is_admin: boolean
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

vi.mock('../auth', () => ({
  useAuth: () => ({
    ...authState,
    logout: async () => {},
    refreshFamilyStatus: async () => {},
  }),
}))

function renderSidebar() {
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  )
}

describe('Sidebar – Forge nav link', () => {
  beforeEach(() => {
    authState.user = {
      name: 'Admin User',
      email: 'admin@example.com',
      is_admin: true,
    }
    authState.loading = false
    authState.hasFeature = () => true
    authState.familyStatus = null
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (typeof url === 'string' && url.startsWith('/api/pokemon/scans/counts')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ unresolved: 0, today_used: 0, today_cap: 600 }),
        })
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ claims: [] }),
      })
    }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    authState.user = null
  })

  it('routes admin Forge link to /forge/mezzanine', () => {
    renderSidebar()
    const links = screen.getAllByRole('link', { name: 'nav.forge' })
    expect(links.length).toBeGreaterThan(0)
    for (const link of links) {
      expect(link.getAttribute('href')).toBe('/forge/mezzanine')
    }
  })
})

describe('Sidebar – Pokémon pending-resolution badge', () => {
  beforeEach(() => {
    authState.user = {
      name: 'Kid',
      email: 'kid@example.com',
      is_admin: false,
    }
    authState.loading = false
    authState.hasFeature = (key: string) => key === 'pokemon'
    authState.familyStatus = null
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    authState.user = null
  })

  it('renders the badge with the unresolved count when > 0', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (typeof url === 'string' && url.startsWith('/api/pokemon/scans/counts')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ unresolved: 3, today_used: 5, today_cap: 600 }),
        })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ claims: [] }) })
    }))
    renderSidebar()
    await waitFor(() => {
      const badges = screen.getAllByTestId('sidebar-pokemon-badge')
      expect(badges.length).toBeGreaterThan(0)
      expect(badges[0]).toHaveTextContent('3')
    })
  })

  it('hides the badge when unresolved is 0', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (typeof url === 'string' && url.startsWith('/api/pokemon/scans/counts')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ unresolved: 0, today_used: 5, today_cap: 600 }),
        })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ claims: [] }) })
    }))
    renderSidebar()
    // Allow the fetch + setState to run.
    await waitFor(() => {
      expect(screen.queryByTestId('sidebar-pokemon-badge')).not.toBeInTheDocument()
    })
  })

  it('caps the displayed badge text at "9+" once the count exceeds 9', async () => {
    vi.stubGlobal('fetch', vi.fn((url: string) => {
      if (typeof url === 'string' && url.startsWith('/api/pokemon/scans/counts')) {
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ unresolved: 42, today_used: 5, today_cap: 600 }),
        })
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ claims: [] }) })
    }))
    renderSidebar()
    await waitFor(() => {
      const badges = screen.getAllByTestId('sidebar-pokemon-badge')
      expect(badges.length).toBeGreaterThan(0)
      expect(badges[0]).toHaveTextContent('9+')
    })
  })
})
