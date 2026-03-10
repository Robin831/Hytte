import { useState, useEffect, useCallback, useRef } from 'react'
import { Copy, Trash2, ExternalLink, Plus, Pencil, X, Check } from 'lucide-react'

interface Link {
  id: number
  code: string
  target_url: string
  title: string
  clicks: number
  created_at: string
}

export default function Links() {
  const [links, setLinks] = useState<Link[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [targetUrl, setTargetUrl] = useState('')
  const [title, setTitle] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editCode, setEditCode] = useState('')
  const [editUrl, setEditUrl] = useState('')
  const [editTitle, setEditTitle] = useState('')
  const [copiedId, setCopiedId] = useState<number | null>(null)
  const copyTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current)
    }
  }, [])
  const [saving, setSaving] = useState(false)
  const [deletingId, setDeletingId] = useState<number | null>(null)

  const fetchLinks = useCallback(async () => {
    try {
      const res = await fetch('/api/links', { credentials: 'include' })
      if (res.ok) {
        const data = await res.json()
        setLinks(data.links)
      } else {
        setError('Failed to load links')
      }
    } catch {
      setError('Failed to load links')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchLinks()
  }, [fetchLinks])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setCreating(true)

    try {
      const res = await fetch('/api/links', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ target_url: targetUrl, title, code: code || undefined }),
      })

      if (!res.ok) {
        try {
          const data = await res.json()
          setError(data.error || 'Failed to create link')
        } catch {
          setError('Failed to create link')
        }
        return
      }

      setTargetUrl('')
      setTitle('')
      setCode('')
      setShowForm(false)
      fetchLinks()
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: number) => {
    if (deletingId !== null) return
    setDeletingId(id)
    try {
      const res = await fetch(`/api/links/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (res.ok) {
        setLinks(prev => prev.filter(l => l.id !== id))
      } else {
        try {
          const data = await res.json()
          setError(data.error || 'Failed to delete link')
        } catch {
          setError('Failed to delete link')
        }
      }
    } catch {
      setError('Failed to delete link')
    } finally {
      setDeletingId(null)
    }
  }

  const handleUpdate = async (id: number) => {
    if (saving) return
    setError('')
    setSaving(true)
    try {
      const res = await fetch(`/api/links/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ target_url: editUrl, title: editTitle, code: editCode }),
      })

      if (!res.ok) {
        try {
          const data = await res.json()
          setError(data.error || 'Failed to update link')
        } catch {
          setError('Failed to update link')
        }
        return
      }

      setEditingId(null)
      fetchLinks()
    } finally {
      setSaving(false)
    }
  }

  const startEdit = (link: Link) => {
    setEditingId(link.id)
    setEditCode(link.code)
    setEditUrl(link.target_url)
    setEditTitle(link.title)
    setError('')
  }

  const copyShortUrl = async (link: Link) => {
    const shortUrl = `${window.location.origin}/go/${link.code}`

    if (!navigator.clipboard || !navigator.clipboard.writeText) {
      setError('Clipboard is not available in this browser.')
      return
    }

    try {
      await navigator.clipboard.writeText(shortUrl)
      setCopiedId(link.id)
      if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current)
      copyTimeoutRef.current = setTimeout(() => setCopiedId(null), 2000)
    } catch (err) {
      console.error('Failed to copy short URL to clipboard', err)
      setError('Failed to copy link to clipboard')
    }
  }

  const shortUrlBase = `${window.location.origin}/go/`

  if (loading) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-bold mb-4">Short Links</h1>
        <p className="text-gray-400">Loading...</p>
      </div>
    )
  }

  return (
    <div className="p-6 max-w-4xl">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Short Links</h1>
        <button
          onClick={() => { setShowForm(!showForm); setError('') }}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded-lg text-sm font-medium transition-colors cursor-pointer"
        >
          <Plus size={16} />
          New Link
        </button>
      </div>

      {/* Create form */}
      {showForm && (
        <form onSubmit={handleCreate} className="mb-6 p-4 bg-gray-800 rounded-lg space-y-3">
          <div>
            <label className="block text-sm text-gray-400 mb-1">Destination URL *</label>
            <input
              type="text"
              value={targetUrl}
              onChange={e => setTargetUrl(e.target.value)}
              placeholder="https://example.com/long-url"
              className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-blue-500"
              required
            />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="block text-sm text-gray-400 mb-1">Title (optional)</label>
              <input
                type="text"
                value={title}
                onChange={e => setTitle(e.target.value)}
                placeholder="My awesome link"
                className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Custom code (optional)</label>
              <div className="flex items-center">
                <span className="text-xs text-gray-500 mr-2 hidden sm:inline">/go/</span>
                <input
                  type="text"
                  value={code}
                  onChange={e => setCode(e.target.value.replace(/[^a-zA-Z0-9_-]/g, ''))}
                  placeholder="auto-generated"
                  className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-blue-500"
                />
              </div>
            </div>
          </div>
          {error && <p className="text-red-400 text-sm">{error}</p>}
          <div className="flex gap-2">
            <button
              type="submit"
              disabled={creating}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              {creating ? 'Creating...' : 'Create'}
            </button>
            <button
              type="button"
              onClick={() => { setShowForm(false); setError('') }}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {/* Links table */}
      {links.length === 0 ? (
        <div className="text-center py-12 text-gray-400">
          <p className="text-lg mb-2">No short links yet</p>
          <p className="text-sm">Create your first short link to get started.</p>
        </div>
      ) : (
        <div className="space-y-2">
          {links.map(link => (
            <div
              key={link.id}
              className="flex items-center gap-4 p-4 bg-gray-800 rounded-lg group"
            >
              {editingId === link.id ? (
                /* Edit mode */
                <div className="flex-1 space-y-2">
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                    <input
                      type="text"
                      value={editCode}
                      onChange={e => setEditCode(e.target.value.replace(/[^a-zA-Z0-9_-]/g, ''))}
                      className="px-2 py-1 bg-gray-900 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-blue-500"
                      placeholder="code"
                    />
                    <input
                      type="text"
                      value={editTitle}
                      onChange={e => setEditTitle(e.target.value)}
                      className="px-2 py-1 bg-gray-900 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-blue-500"
                      placeholder="title"
                    />
                    <input
                      type="text"
                      value={editUrl}
                      onChange={e => setEditUrl(e.target.value)}
                      className="px-2 py-1 bg-gray-900 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-blue-500"
                      placeholder="target URL"
                    />
                  </div>
                  {error && <p className="text-red-400 text-xs">{error}</p>}
                  <div className="flex gap-2">
                    <button
                      onClick={() => handleUpdate(link.id)}
                      disabled={saving}
                      className="p-1.5 text-green-400 hover:text-green-300 cursor-pointer disabled:opacity-50"
                      title="Save"
                    >
                      <Check size={16} />
                    </button>
                    <button
                      onClick={() => { setEditingId(null); setError('') }}
                      className="p-1.5 text-gray-400 hover:text-white cursor-pointer"
                      title="Cancel"
                    >
                      <X size={16} />
                    </button>
                  </div>
                </div>
              ) : (
                /* View mode */
                <>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <span className="text-sm font-mono text-blue-400">/go/{link.code}</span>
                      {link.title && (
                        <span className="text-sm text-gray-300">&mdash; {link.title}</span>
                      )}
                    </div>
                    <a
                      href={link.target_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs text-gray-500 hover:text-gray-300 truncate block transition-colors"
                    >
                      {link.target_url}
                    </a>
                  </div>

                  <div className="flex items-center gap-1 text-xs text-gray-500 shrink-0">
                    <span>{link.clicks} click{link.clicks !== 1 ? 's' : ''}</span>
                  </div>

                  <div className="flex items-center gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                    <button
                      onClick={() => copyShortUrl(link)}
                      className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                      title={copiedId === link.id ? 'Copied!' : `Copy ${shortUrlBase}${link.code}`}
                    >
                      {copiedId === link.id ? <Check size={16} className="text-green-400" /> : <Copy size={16} />}
                    </button>
                    <a
                      href={`/go/${link.code}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="p-1.5 text-gray-400 hover:text-white transition-colors"
                      title="Open short link"
                    >
                      <ExternalLink size={16} />
                    </a>
                    <button
                      onClick={() => startEdit(link)}
                      className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                      title="Edit"
                    >
                      <Pencil size={16} />
                    </button>
                    <button
                      onClick={() => handleDelete(link.id)}
                      disabled={deletingId === link.id}
                      className="p-1.5 text-gray-400 hover:text-red-400 transition-colors cursor-pointer disabled:opacity-50"
                      title="Delete"
                    >
                      <Trash2 size={16} />
                    </button>
                  </div>
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
