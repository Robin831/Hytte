// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, cleanup } from '@testing-library/react'
import { useSyncLanguagePreference } from './useSyncLanguagePreference'

const state: { user: { id: number } | null; language: string } = { user: { id: 7 }, language: 'nb-NO' }

vi.mock('../auth', () => ({ useAuth: () => ({ user: state.user }) }))
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ i18n: { language: state.language, resolvedLanguage: state.language } }),
}))

describe('useSyncLanguagePreference', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    localStorage.clear()
    state.user = { id: 7 }
    state.language = 'nb-NO'
    fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('saves the base language once per user and language', async () => {
    renderHook(() => useSyncLanguagePreference())
    await waitFor(() => expect(localStorage.getItem('hytte-language-synced')).toBe('7:nb'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ preferences: { ui_language: 'nb' } })

    cleanup()
    renderHook(() => useSyncLanguagePreference())
    expect(fetchMock).toHaveBeenCalledTimes(1)

    state.language = 'th'
    renderHook(() => useSyncLanguagePreference())
    await waitFor(() => expect(localStorage.getItem('hytte-language-synced')).toBe('7:th'))
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('does nothing when logged out or for unsupported languages', () => {
    state.user = null
    renderHook(() => useSyncLanguagePreference())
    state.user = { id: 7 }
    state.language = 'de'
    renderHook(() => useSyncLanguagePreference())
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
