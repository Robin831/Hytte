import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'

const SYNCED_KEY = 'hytte-language-synced'
const SUPPORTED = ['en', 'nb', 'th']

/**
 * Mirrors the UI language into the ui_language preference, so server-rendered
 * text (push notifications) arrives in the language the user reads Hytte in.
 * Saves once per user + language; the marker in localStorage avoids a PUT on
 * every page load.
 */
export function useSyncLanguagePreference() {
  const { user } = useAuth()
  const { i18n } = useTranslation()
  const lang = (i18n.resolvedLanguage || i18n.language || '').split('-')[0]

  useEffect(() => {
    if (!user || !SUPPORTED.includes(lang)) return
    const marker = `${user.id}:${lang}`
    try {
      if (localStorage.getItem(SYNCED_KEY) === marker) return
    } catch {
      // Storage unavailable: sync anyway, it's one small request.
    }
    const controller = new AbortController()
    fetch('/api/settings/preferences', {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ preferences: { ui_language: lang } }),
      signal: controller.signal,
    })
      .then(res => {
        if (!res.ok) return
        try {
          localStorage.setItem(SYNCED_KEY, marker)
        } catch {
          // Not fatal: we'll just sync again next load.
        }
      })
      .catch(() => { /* best effort; retried next load */ })
    return () => controller.abort()
  }, [user, lang])
}
