import { useState, useEffect, useCallback } from 'react'
import { ExternalLink, Plus, Trash2, Link } from 'lucide-react'
import Widget from '../Widget'

interface QuickLink {
  title: string
  url: string
}

function normalizeUrl(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return trimmed
  return /^https?:\/\//i.test(trimmed) ? trimmed : 'https://' + trimmed
}

async function loadLinks(): Promise<QuickLink[]> {
  try {
    const res = await fetch('/api/settings/preferences', { credentials: 'include' })
    if (!res.ok) return []
    const data = await res.json()
    const raw: string = data?.preferences?.quick_links ?? '[]'
    return JSON.parse(raw) as QuickLink[]
  } catch {
    return []
  }
}

async function saveLinks(links: QuickLink[]): Promise<void> {
  const res = await fetch('/api/settings/preferences', {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ preferences: { quick_links: JSON.stringify(links) } }),
  })
  if (!res.ok) throw new Error(`Failed to save links: ${res.status}`)
}

export default function QuickLinksWidget() {
  const [links, setLinks] = useState<QuickLink[]>([])
  const [adding, setAdding] = useState(false)
  const [title, setTitle] = useState('')
  const [url, setUrl] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    loadLinks()
      .then(loaded => { if (!cancelled) setLinks(loaded) })
      .catch(err => { if (!cancelled) console.error('Failed to load quick links:', err) })
    return () => { cancelled = true }
  }, [])

  const persist = useCallback(async (updated: QuickLink[], rollback: QuickLink[]) => {
    setSaving(true)
    setSaveError(null)
    try {
      await saveLinks(updated)
    } catch (err) {
      setLinks(rollback)
      setSaveError('Failed to save. Please try again.')
      console.error('Failed to save quick links:', err)
    } finally {
      setSaving(false)
    }
  }, [])

  const handleAdd = async () => {
    const trimTitle = title.trim()
    const trimUrl = normalizeUrl(url)
    if (!trimTitle || !trimUrl) return
    const previous = links
    const updated = [...links, { title: trimTitle, url: trimUrl }]
    setLinks(updated)
    setTitle('')
    setUrl('')
    setAdding(false)
    await persist(updated, previous)
  }

  const handleRemove = async (index: number) => {
    const previous = links
    const updated = links.filter((_, i) => i !== index)
    setLinks(updated)
    await persist(updated, previous)
  }

  const handleKeyDown = (e: { key: string }) => {
    if (e.key === 'Enter') handleAdd()
    if (e.key === 'Escape') {
      setAdding(false)
      setTitle('')
      setUrl('')
    }
  }

  return (
    <Widget title="Quick Links">
      <div className="space-y-2">
        {saveError && (
          <p className="text-xs text-red-400">{saveError}</p>
        )}
        {links.length === 0 && !adding && (
          <p className="text-sm text-gray-500 py-1">No links yet. Add one below.</p>
        )}

        {links.map((link, i) => (
          <div key={i} className="flex items-center gap-2 group">
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
              onClick={() => handleRemove(i)}
              aria-label={`Remove ${link.title}`}
              className="shrink-0 text-gray-600 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-opacity"
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
              placeholder="Title"
              aria-label="Link title"
              value={title}
              onChange={e => setTitle(e.target.value)}
              onKeyDown={handleKeyDown}
              className="w-full bg-gray-700 text-sm text-white rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 placeholder-gray-500"
            />
            <input
              type="url"
              placeholder="URL"
              aria-label="Link URL"
              value={url}
              onChange={e => setUrl(e.target.value)}
              onKeyDown={handleKeyDown}
              className="w-full bg-gray-700 text-sm text-white rounded px-3 py-1.5 outline-none focus:ring-1 focus:ring-blue-500 placeholder-gray-500"
            />
            <div className="flex gap-2">
              <button
                onClick={handleAdd}
                disabled={saving || !title.trim() || !url.trim()}
                className="flex-1 text-xs bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white rounded px-3 py-1.5 transition-colors"
              >
                {saving ? 'Saving…' : 'Add'}
              </button>
              <button
                onClick={() => { setAdding(false); setTitle(''); setUrl('') }}
                className="text-xs text-gray-400 hover:text-gray-200 px-3 py-1.5"
              >
                Cancel
              </button>
            </div>
          </div>
        )}

        {!adding && (
          <button
            onClick={() => setAdding(true)}
            className="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-300 pt-1 transition-colors"
          >
            <Plus size={14} />
            Add link
          </button>
        )}

        {links.length > 0 && !adding && (
          <p className="text-xs text-gray-600 pt-1 flex items-center gap-1">
            <Link size={10} />
            {links.length} bookmark{links.length !== 1 ? 's' : ''}
          </p>
        )}
      </div>
    </Widget>
  )
}
