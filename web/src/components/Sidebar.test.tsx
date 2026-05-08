// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
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
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
      ok: true,
      json: () => Promise.resolve({ claims: [] }),
    })))
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
