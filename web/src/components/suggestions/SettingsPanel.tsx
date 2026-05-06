import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../ui/skeleton'

export interface PageSummary {
  slug: string
  title: string
  rotation_enabled: boolean | null
}

interface SettingsPanelProps {
  active: boolean
}

const NEW_PAGE_SLUG = '__new_page__'

function pageEnabled(p: PageSummary): boolean {
  return p.rotation_enabled === null || p.rotation_enabled === true
}

export function SettingsPanel({ active }: SettingsPanelProps) {
  const { t } = useTranslation('suggestions')
  const { t: tCommon } = useTranslation('common')
  const [pages, setPages] = useState<PageSummary[]>([])
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [toast, setToast] = useState<string | null>(null)
  const [pendingSlug, setPendingSlug] = useState<string | null>(null)
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (toastTimer.current) clearTimeout(toastTimer.current)
    }
  }, [])

  const showToast = useCallback((message: string) => {
    setToast(message)
    if (toastTimer.current) clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(null), 4000)
  }, [])

  // Resolve translated strings outside the effect so a fresh `t` reference
  // (e.g. between mocked renders in tests, or after a language change) does
  // not retrigger the fetch. Effect re-runs only on real string changes.
  const loadErrorMsg = t('settings.loadError')

  useEffect(() => {
    if (!active || loaded) return
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      setLoadError(null)
      try {
        const res = await fetch('/api/suggestions/pages', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error('failed')
        const data = (await res.json()) as PageSummary[]
        setPages(Array.isArray(data) ? data.filter(p => p.slug !== NEW_PAGE_SLUG) : [])
        setLoaded(true)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setLoadError(loadErrorMsg)
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [active, loaded, loadErrorMsg])

  async function handleToggle(page: PageSummary) {
    if (pendingSlug) return
    const previous = page.rotation_enabled
    const nextValue = !pageEnabled(page)
    setPages(prev =>
      prev.map(p =>
        p.slug === page.slug ? { ...p, rotation_enabled: nextValue } : p,
      ),
    )
    setPendingSlug(page.slug)
    try {
      const res = await fetch(`/api/suggestions/pages/${encodeURIComponent(page.slug)}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ rotation_enabled: nextValue }),
      })
      if (!res.ok) throw new Error('failed')
    } catch {
      setPages(prev =>
        prev.map(p =>
          p.slug === page.slug ? { ...p, rotation_enabled: previous } : p,
        ),
      )
      showToast(t('settings.toggleError'))
    } finally {
      setPendingSlug(null)
    }
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-400">{t('settings.description')}</p>

      {loading && !loaded ? (
        <div className="space-y-2" aria-label={tCommon('skeleton.loading')}>
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </div>
      ) : loadError ? (
        <div
          role="alert"
          data-testid="settings-load-error"
          className="rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-300"
        >
          {loadError}
        </div>
      ) : pages.length === 0 ? (
        <p className="px-4 py-10 text-center text-sm text-gray-400">
          {t('settings.empty')}
        </p>
      ) : (
        <ul className="divide-y divide-gray-800 rounded-lg border border-gray-800 bg-gray-900/40">
          {pages.map(page => {
            const enabled = pageEnabled(page)
            return (
              <li
                key={page.slug}
                className="flex items-center justify-between gap-4 px-4 py-3"
                data-testid={`settings-page-${page.slug}`}
              >
                <div className="min-w-0">
                  <p className="font-medium text-gray-100 truncate">{page.title}</p>
                  <p className="text-xs text-gray-500 font-mono truncate">{page.slug}</p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={enabled}
                  aria-label={t('settings.toggleAria', { title: page.title })}
                  data-testid={`settings-toggle-${page.slug}`}
                  disabled={pendingSlug !== null}
                  onClick={() => handleToggle(page)}
                  className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                    enabled ? 'bg-blue-600' : 'bg-gray-600'
                  }`}
                >
                  <span
                    className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                      enabled ? 'translate-x-6' : 'translate-x-1'
                    }`}
                  />
                </button>
              </li>
            )
          })}
        </ul>
      )}

      {toast && (
        <div
          role="alert"
          data-testid="settings-toggle-error"
          className="fixed top-4 right-4 z-50 rounded-lg bg-red-700 px-4 py-3 text-sm font-medium text-white shadow-lg"
        >
          {toast}
        </div>
      )}
    </div>
  )
}
