// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import HomePage from './HomePage'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('./Dashboard', () => ({ default: () => <div data-testid="dashboard" /> }))
vi.mock('../components/widgets/WeatherWidget', () => ({ default: () => <div data-testid="weather" /> }))

const authState: { user: { name: string } | null; loading: boolean } = { user: null, loading: false }
vi.mock('../auth', () => ({ useAuth: () => authState }))

describe('HomePage', () => {
  afterEach(() => {
    cleanup()
    authState.user = null
    authState.loading = false
  })

  it('welcomes a signed-out visitor with a way to sign in and the weather', () => {
    render(<HomePage />)
    expect(screen.getByRole('heading', { name: 'appName' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'sidebar.signIn' })).toHaveAttribute('href', '/api/auth/google/login')
    expect(screen.getByTestId('weather')).toBeInTheDocument()
    expect(screen.queryByTestId('dashboard')).toBeNull()
  })

  it('is the dashboard itself once signed in', () => {
    authState.user = { name: 'Robin Smith' }
    render(<HomePage />)
    expect(screen.getByTestId('dashboard')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'sidebar.signIn' })).toBeNull()
  })

  it('shows neither while the session is still being checked', () => {
    authState.loading = true
    const { container } = render(<HomePage />)
    expect(container).toBeEmptyDOMElement()
  })
})
