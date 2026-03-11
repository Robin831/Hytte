import { useState, useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { Plus, Search, Tag, Trash2, Save, Eye, Edit3, X, FileText } from 'lucide-react'

interface Note {
  id: number
  user_id: number
  title: string
  content: string
  tags: string[]
  created_at: string
  updated_at: string
}

type ViewMode = 'edit' | 'preview'

export default function Notes() {
  const [notes, setNotes] = useState<Note[]>([])
  const [allTags, setAllTags] = useState<string[]>([])
  const [selectedNote, setSelectedNote] = useState<Note | null>(null)
  const [isCreating, setIsCreating] = useState(false)
  const [search, setSearch] = useState('')
  const [activeTag, setActiveTag] = useState('')
  const [viewMode, setViewMode] = useState<ViewMode>('edit')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)

  // Draft state for the editor
  const [draftTitle, setDraftTitle] = useState('')
  const [draftContent, setDraftContent] = useState('')
  const [draftTags, setDraftTags] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const params = new URLSearchParams()
        if (search) params.set('search', search)
        if (activeTag) params.set('tag', activeTag)
        const res = await fetch(`/api/notes?${params}`, { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error('Failed to load notes')
        const data = await res.json()
        setNotes(data.notes ?? [])
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [search, activeTag, refreshKey])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/notes/tags', { credentials: 'include', signal: controller.signal })
        if (!res.ok) return
        const data = await res.json()
        setAllTags(data.tags ?? [])
      } catch {
        // non-critical
      }
    })()
    return () => { controller.abort() }
  }, [refreshKey])

  function openNote(note: Note) {
    setSelectedNote(note)
    setIsCreating(false)
    setDraftTitle(note.title)
    setDraftContent(note.content)
    setDraftTags(note.tags.join(', '))
    setViewMode('edit')
    setError('')
  }

  function startCreating() {
    setSelectedNote(null)
    setIsCreating(true)
    setDraftTitle('')
    setDraftContent('')
    setDraftTags('')
    setViewMode('edit')
    setError('')
  }

  function cancelEdit() {
    setSelectedNote(null)
    setIsCreating(false)
    setError('')
  }

  function parseTags(raw: string): string[] {
    return raw
      .split(',')
      .map(t => t.trim())
      .filter(t => t.length > 0)
  }

  async function saveNote() {
    setSaving(true)
    setError('')
    const tags = parseTags(draftTags)

    try {
      if (isCreating) {
        const res = await fetch('/api/notes', {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title: draftTitle, content: draftContent, tags }),
        })
        if (!res.ok) {
          const data = await res.json()
          throw new Error(data.error ?? 'Failed to create note')
        }
        const data = await res.json()
        setIsCreating(false)
        setSelectedNote(data.note)
      } else if (selectedNote) {
        const res = await fetch(`/api/notes/${selectedNote.id}`, {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title: draftTitle, content: draftContent, tags }),
        })
        if (!res.ok) {
          const data = await res.json()
          throw new Error(data.error ?? 'Failed to save note')
        }
        const data = await res.json()
        setSelectedNote(data.note)
      }
      setRefreshKey(k => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  async function deleteNote(note: Note) {
    if (!confirm(`Delete "${note.title || 'Untitled'}"?`)) return
    try {
      const res = await fetch(`/api/notes/${note.id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json()
        throw new Error(data.error ?? 'Failed to delete')
      }
      if (selectedNote?.id === note.id) {
        setSelectedNote(null)
        setIsCreating(false)
      }
      setRefreshKey(k => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  const hasChanges = selectedNote
    ? draftTitle !== selectedNote.title ||
      draftContent !== selectedNote.content ||
      draftTags !== selectedNote.tags.join(', ')
    : isCreating

  return (
    <div className="flex h-[calc(100vh-3.5rem)] md:h-screen overflow-hidden">
      {/* Left panel — note list */}
      <aside className="w-72 shrink-0 bg-gray-950 border-r border-gray-800 flex flex-col">
        {/* Search + new */}
        <div className="p-3 border-b border-gray-800 space-y-2">
          <div className="flex gap-2">
            <div className="relative flex-1">
              <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-500" />
              <input
                type="text"
                placeholder="Search notes…"
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="w-full pl-8 pr-3 py-1.5 bg-gray-800 border border-gray-700 rounded text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                aria-label="Search notes"
              />
            </div>
            <button
              onClick={startCreating}
              className="flex items-center gap-1 px-2 py-1.5 bg-blue-600 hover:bg-blue-500 text-white rounded text-sm transition-colors cursor-pointer shrink-0"
              title="New note"
            >
              <Plus size={16} />
            </button>
          </div>

          {/* Tag filters */}
          {allTags.length > 0 && (
            <div className="flex flex-wrap gap-1">
              <button
                onClick={() => setActiveTag('')}
                className={`px-2 py-0.5 rounded text-xs transition-colors cursor-pointer ${
                  activeTag === ''
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
              >
                All
              </button>
              {allTags.map(tag => (
                <button
                  key={tag}
                  onClick={() => setActiveTag(activeTag === tag ? '' : tag)}
                  className={`px-2 py-0.5 rounded text-xs transition-colors cursor-pointer ${
                    activeTag === tag
                      ? 'bg-blue-600 text-white'
                      : 'bg-gray-800 text-gray-400 hover:text-white'
                  }`}
                >
                  {tag}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Note list */}
        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <p className="p-4 text-gray-500 text-sm">Loading…</p>
          ) : notes.length === 0 ? (
            <div className="p-4 text-center">
              <FileText size={32} className="mx-auto text-gray-700 mb-2" />
              <p className="text-gray-500 text-sm">No notes yet.</p>
              <button
                onClick={startCreating}
                className="mt-2 text-blue-400 hover:text-blue-300 text-sm underline cursor-pointer"
              >
                Create your first note
              </button>
            </div>
          ) : (
            notes.map(note => (
              <button
                key={note.id}
                onClick={() => openNote(note)}
                className={`w-full text-left px-3 py-2.5 border-b border-gray-800/50 hover:bg-gray-800/50 transition-colors cursor-pointer ${
                  selectedNote?.id === note.id ? 'bg-gray-800' : ''
                }`}
              >
                <p className="text-sm font-medium text-white truncate">
                  {note.title || <span className="text-gray-500 italic">Untitled</span>}
                </p>
                <p className="text-xs text-gray-500 truncate mt-0.5">{note.content.slice(0, 60)}</p>
                {note.tags.length > 0 && (
                  <div className="flex flex-wrap gap-1 mt-1">
                    {note.tags.map(tag => (
                      <span
                        key={tag}
                        className="px-1.5 py-0.5 bg-gray-700 text-gray-400 text-xs rounded"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
              </button>
            ))
          )}
        </div>
      </aside>

      {/* Right panel — editor / viewer */}
      <main className="flex-1 min-w-0 flex flex-col bg-gray-900">
        {isCreating || selectedNote ? (
          <>
            {/* Toolbar */}
            <div className="flex items-center gap-2 px-4 py-2 border-b border-gray-800 shrink-0">
              <div className="flex rounded overflow-hidden border border-gray-700">
                <button
                  onClick={() => setViewMode('edit')}
                  className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors cursor-pointer ${
                    viewMode === 'edit'
                      ? 'bg-gray-700 text-white'
                      : 'text-gray-400 hover:text-white'
                  }`}
                >
                  <Edit3 size={14} />
                  Edit
                </button>
                <button
                  onClick={() => setViewMode('preview')}
                  className={`flex items-center gap-1.5 px-3 py-1.5 text-sm transition-colors cursor-pointer ${
                    viewMode === 'preview'
                      ? 'bg-gray-700 text-white'
                      : 'text-gray-400 hover:text-white'
                  }`}
                >
                  <Eye size={14} />
                  Preview
                </button>
              </div>

              <div className="ml-auto flex items-center gap-2">
                {error && <span className="text-red-400 text-sm">{error}</span>}
                <button
                  onClick={saveNote}
                  disabled={saving || !hasChanges}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 disabled:cursor-default text-white rounded text-sm transition-colors cursor-pointer"
                >
                  <Save size={14} />
                  {saving ? 'Saving…' : 'Save'}
                </button>
                {selectedNote && (
                  <button
                    onClick={() => deleteNote(selectedNote)}
                    className="flex items-center gap-1.5 px-3 py-1.5 text-red-400 hover:text-red-300 hover:bg-gray-800 rounded text-sm transition-colors cursor-pointer"
                    title="Delete note"
                  >
                    <Trash2 size={14} />
                  </button>
                )}
                <button
                  onClick={cancelEdit}
                  className="flex items-center gap-1 px-2 py-1.5 text-gray-400 hover:text-white hover:bg-gray-800 rounded text-sm transition-colors cursor-pointer"
                  title="Close"
                >
                  <X size={16} />
                </button>
              </div>
            </div>

            {/* Note meta: title + tags */}
            {viewMode === 'edit' && (
              <div className="px-6 pt-4 space-y-2 shrink-0">
                <input
                  type="text"
                  placeholder="Note title…"
                  value={draftTitle}
                  onChange={e => setDraftTitle(e.target.value)}
                  className="w-full bg-transparent text-2xl font-bold text-white placeholder-gray-600 focus:outline-none"
                  aria-label="Note title"
                />
                <div className="flex items-center gap-2">
                  <Tag size={14} className="text-gray-500 shrink-0" />
                  <input
                    type="text"
                    placeholder="Tags (comma-separated)…"
                    value={draftTags}
                    onChange={e => setDraftTags(e.target.value)}
                    className="flex-1 bg-transparent text-sm text-gray-400 placeholder-gray-600 focus:outline-none"
                    aria-label="Note tags"
                  />
                </div>
                <hr className="border-gray-800" />
              </div>
            )}

            {viewMode === 'edit' ? (
              <textarea
                value={draftContent}
                onChange={e => setDraftContent(e.target.value)}
                placeholder="Write your note in Markdown…"
                className="flex-1 px-6 py-4 bg-transparent text-gray-200 text-sm font-mono leading-relaxed resize-none focus:outline-none placeholder-gray-600"
                aria-label="Note content"
                spellCheck
              />
            ) : (
              <div className="flex-1 overflow-y-auto px-6 py-4">
                <h1 className="text-2xl font-bold text-white mb-1">
                  {draftTitle || <span className="text-gray-500 italic">Untitled</span>}
                </h1>
                {draftTags && (
                  <div className="flex flex-wrap gap-1 mb-4">
                    {parseTags(draftTags).map(tag => (
                      <span
                        key={tag}
                        className="px-2 py-0.5 bg-gray-700 text-gray-300 text-xs rounded"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
                <div className="prose prose-invert prose-sm max-w-none">
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    components={{
                      code({ className, children, ...props }) {
                        const match = /language-(\w+)/.exec(className ?? '')
                        const isBlock = !!match
                        return isBlock ? (
                          <SyntaxHighlighter
                            style={vscDarkPlus}
                            language={match![1]}
                            PreTag="div"
                          >
                            {String(children).replace(/\n$/, '')}
                          </SyntaxHighlighter>
                        ) : (
                          <code
                            className="px-1 py-0.5 bg-gray-800 rounded text-sm font-mono text-gray-200"
                            {...props}
                          >
                            {children}
                          </code>
                        )
                      },
                      h1: ({ children }) => (
                        <h1 className="text-2xl font-bold text-white mt-6 mb-3">{children}</h1>
                      ),
                      h2: ({ children }) => (
                        <h2 className="text-xl font-semibold text-white mt-5 mb-2">{children}</h2>
                      ),
                      h3: ({ children }) => (
                        <h3 className="text-lg font-semibold text-white mt-4 mb-2">{children}</h3>
                      ),
                      p: ({ children }) => (
                        <p className="text-gray-300 mb-3 leading-relaxed">{children}</p>
                      ),
                      ul: ({ children }) => (
                        <ul className="list-disc list-inside text-gray-300 mb-3 space-y-1">
                          {children}
                        </ul>
                      ),
                      ol: ({ children }) => (
                        <ol className="list-decimal list-inside text-gray-300 mb-3 space-y-1">
                          {children}
                        </ol>
                      ),
                      li: ({ children }) => <li className="text-gray-300">{children}</li>,
                      blockquote: ({ children }) => (
                        <blockquote className="border-l-4 border-gray-600 pl-4 text-gray-400 italic mb-3">
                          {children}
                        </blockquote>
                      ),
                      a: ({ href, children }) => (
                        <a
                          href={href}
                          className="text-blue-400 hover:text-blue-300 underline"
                          target="_blank"
                          rel="noopener noreferrer"
                        >
                          {children}
                        </a>
                      ),
                      strong: ({ children }) => (
                        <strong className="font-semibold text-white">{children}</strong>
                      ),
                      em: ({ children }) => <em className="italic text-gray-200">{children}</em>,
                      hr: () => <hr className="border-gray-700 my-4" />,
                      table: ({ children }) => (
                        <div className="overflow-x-auto mb-3">
                          <table className="w-full text-sm text-gray-300 border-collapse">
                            {children}
                          </table>
                        </div>
                      ),
                      th: ({ children }) => (
                        <th className="border border-gray-700 px-3 py-1.5 bg-gray-800 font-semibold text-white text-left">
                          {children}
                        </th>
                      ),
                      td: ({ children }) => (
                        <td className="border border-gray-700 px-3 py-1.5">{children}</td>
                      ),
                    }}
                  >
                    {draftContent || '*Nothing to preview yet.*'}
                  </ReactMarkdown>
                </div>
              </div>
            )}
          </>
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center text-center p-8">
            <FileText size={48} className="text-gray-700 mb-4" />
            <h2 className="text-xl font-semibold text-gray-400 mb-2">Select a note</h2>
            <p className="text-gray-600 text-sm mb-4">
              Choose a note from the list, or create a new one.
            </p>
            <button
              onClick={startCreating}
              className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm transition-colors cursor-pointer"
            >
              <Plus size={16} />
              New note
            </button>
          </div>
        )}
      </main>
    </div>
  )
}
