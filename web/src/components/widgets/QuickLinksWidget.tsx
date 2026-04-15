import { useState, useEffect, useRef } from 'react'
import { ExternalLink, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'

interface QuickLink {
  title: string
  url: string
}

function normalizeUrl(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return trimmed
  let withScheme = trimmed
  if (!/^https?:\/\//i.test(trimmed)) {
    // Reject anything with an explicit non-http(s) scheme
    if (/^[a-z][a-z0-9+.-]*:/i.test(trimmed)) return ''
    withScheme = 'https://' + trimmed
  }
  try {
    const parsed = new URL(withScheme)
    // Reject schemes the backend would reject
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return ''
    // Reject URLs that exceed backend max length after normalization
    if (parsed.href.length > 2048) return ''
    return parsed.href
  } catch {
    return ''
  }
}

async function saveLinks(links: QuickLink[]): Promise<void> {
  // quick_links is a JSON-typed preference (server accepts arbitrary JSON).
  // Send the array directly — wrapping it in JSON.stringify() would produce
  // a JSON-encoded string that the server's validator rejects with 400.
  const res = await fetch('/api/settings/preferences', {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ preferences: { quick_links: links } }),
  })
  if (!res.ok) throw new Error(`Failed to save links: ${res.status}`)
}

export default function QuickLinksWidget() {
  const { t } = useTranslation('dashboard')
  const { user } = useAuth()
  const [links, setLinks] = useState<QuickLink[]>([])
  const [adding, setAdding] = useState(false)
  const [title, setTitle] = useState('')
  const [url, setUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const mountedRef = useRef(true)
  const savingRef = useRef(false)

  useEffect(() => {
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    fetch('/api/settings/preferences', { credentials: 'include', signal: controller.signal })
      .then(res => res.ok ? res.json() : Promise.reject(new Error(`${res.status}`)))
      .then(data => {
        const raw: string = data?.preferences?.quick_links ?? '[]'
        // Parse once; if a legacy value is still double-encoded (JSON string
        // wrapping a JSON array), unwrap once more before committing to state.
        let parsed: unknown = JSON.parse(raw)
        if (typeof parsed === 'string') parsed = JSON.parse(parsed)
        setLinks(Array.isArray(parsed) ? (parsed as QuickLink[]) : [])
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('Failed to load quick links:', err)
      })
    return () => { controller.abort() }
  }, [user])

  if (!user) return null

  const handleAdd = async () => {
    if (savingRef.current) return
    const trimTitle = title.trim()
    const trimUrl = normalizeUrl(url)
    if (!trimTitle || !trimUrl) {
      if (trimTitle && !trimUrl) setSaveError(t('widgets.quickLinks.errors.invalidUrl'))
      return
    }
    if (trimTitle.length > 200) {
      setSaveError(t('widgets.quickLinks.errors.titleTooLong'))
      return
    }
    if (links.length >= 50) {
      setSaveError(t('widgets.quickLinks.errors.maxLinks'))
      return
    }
    const updated = [...links, { title: trimTitle, url: trimUrl }]
    setSaving(true)
    savingRef.current = true
    setSaveError(null)
    try {
      await saveLinks(updated)
      if (mountedRef.current) {
        setLinks(updated)
        setTitle('')
        setUrl('')
        setAdding(false)
      }
    } catch (err) {
      if (mountedRef.current) {
        setSaveError(t('widgets.quickLinks.errors.saveFailed'))
      }
      console.error('Failed to save quick links:', err)
    } finally {
      savingRef.current = false
      if (mountedRef.current) setSaving(false)
    }
  }

  const handleRemove = async (index: number) => {
    if (savingRef.current) return
    const previous = links
    const updated = links.filter((_, i) => i !== index)
    savingRef.current = true
    setSaving(true)
    setSaveError(null)
    try {
      await saveLinks(updated)
      if (mountedRef.current) setLinks(updated)
    } catch (err) {
      if (mountedRef.current) {
        setLinks(previous)
        setSaveError(t('widgets.quickLinks.errors.saveFailed'))
      }
      console.error('Failed to save quick links:', err)
    } finally {
      savingRef.current = false
      if (mountedRef.current) setSaving(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') void handleAdd()
    if (e.key === 'Escape') {
      setAdding(false)
      setTitle('')
      setUrl('')
    }
  }

  return (
    <Widget title={t('widgets.quickLinks.title')}>
      <div className="space-y-2">
        {saveError && (
          <p className="text-xs text-red-400">{saveError}</p>
        )}
        {links.length === 0 && !adding && (
          <p className="text-sm text-gray-500 py-1">{t('widgets.quickLinks.noLinks')}</p>
        )}

        {links.map((link, i) => (
          <div key={`${i}-${link.url}-${link.title}`} className="flex items-center gap-2 group">
            <a
              href={link.url}
              target="_blank"
              rel="noopener noreferrer"
              className="flex-1 flex items-center gap-2 text-sm text-blue-400 hover:text-blue-300 min-w-0"
            >
              <ExternalLink size={14} className="shrink-0 text-gray-500" />
              <span className="truncate">{link.title}</span>
            </a>
            <button
              onClick={() => void handleRemove(i)}
              disabled={saving}
              aria-label={t('widgets.quickLinks.removeLink', { title: link.title })}
              className="shrink-0 text-gray-600 hover:text-red-400 transition-colors disabled:opacity-20 disabled:pointer-events-none"
            >
              <Trash2 size={14} />
            </button>
          </div>
        ))}

        {adding && (
          <div className="space-y-2 pt-1">
            <input
              autoFocus
              type="text"
              placeholder={t('widgets.quickLinks.placeholders.title')}
              aria-label={t('widgets.quickLinks.placeholders.title')}
              maxLength={200}
              value={title}
              onChange={e => setTitle(e.target.value)}
              onKeyDown={handleKeyDown}
              className="w-full bg-gray-700 text-sm text-white rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 placeholder-gray-500"
            />
            <input
              type="url"
              placeholder={t('widgets.quickLinks.placeholders.url')}
              aria-label={t('widgets.quickLinks.placeholders.url')}
              maxLength={2048}
              value={url}
              onChange={e => setUrl(e.target.value)}
              onKeyDown={handleKeyDown}
              className="w-full bg-gray-700 text-sm text-white rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 placeholder-gray-500"
            />
            <div className="flex gap-2">
              <button
                onClick={() => void handleAdd()}
                disabled={saving || !title.trim() || !url.trim()}
                className="flex-1 text-xs bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white rounded px-3 py-1.5 transition-colors"
              >
                {saving ? t('widgets.quickLinks.saving') : t('widgets.quickLinks.add')}
              </button>
              <button
                onClick={() => { setAdding(false); setTitle(''); setUrl('') }}
                className="text-xs text-gray-400 hover:text-gray-200 px-3 py-1.5"
              >
                {t('widgets.quickLinks.cancel')}
              </button>
            </div>
          </div>
        )}

        {!adding && links.length < 50 && (
          <button
            onClick={() => setAdding(true)}
            className="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-300 pt-1 transition-colors"
          >
            <Plus size={14} />
            {t('widgets.quickLinks.addLink')}
          </button>
        )}

        {links.length > 0 && !adding && (
          <p className="text-xs text-gray-600 pt-1">
            {t('widgets.quickLinks.bookmarkCount', { count: links.length })}
          </p>
        )}
      </div>
    </Widget>
  )
}
