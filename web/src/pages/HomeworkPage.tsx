import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus, BookOpen, Loader2, Settings, MoreVertical, Pencil, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime as fmtTime } from '../utils/formatDate'
import { SkeletonRow } from '../components/Skeleton'

interface Conversation {
  id: number
  kid_id: number
  subject: string
  last_message_preview?: string
  created_at: string
  updated_at: string
}

export default function HomeworkPage() {
  const { t } = useTranslation('homework')
  const navigate = useNavigate()
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  // Only one row may have its menu, rename editor or delete confirmation open.
  const [menuOpenId, setMenuOpenId] = useState<number | null>(null)
  const [renamingId, setRenamingId] = useState<number | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const menuTriggerRefs = useRef<Record<number, HTMLButtonElement | null>>({})
  // Mirrors renamingId synchronously so a blur fired while the input unmounts
  // (after Enter or Escape) cannot commit the rename a second time.
  const renamingIdRef = useRef<number | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/homework/conversations', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoad'))
        const data = await res.json()
        setConversations(data.conversations ?? [])
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [t])

  // Close the open menu when clicking outside of it.
  useEffect(() => {
    if (menuOpenId === null) return
    const openId = menuOpenId
    function handlePointerDown(e: MouseEvent) {
      const target = e.target as Node
      if (menuRef.current?.contains(target)) return
      if (menuTriggerRefs.current[openId]?.contains(target)) return
      setMenuOpenId(null)
      setConfirmDeleteId(null)
    }
    document.addEventListener('mousedown', handlePointerDown)
    return () => { document.removeEventListener('mousedown', handlePointerDown) }
  }, [menuOpenId])

  function closeMenu(focusTrigger = false) {
    const id = menuOpenId
    setMenuOpenId(null)
    setConfirmDeleteId(null)
    if (focusTrigger && id !== null) menuTriggerRefs.current[id]?.focus()
  }

  function toggleMenu(id: number) {
    setConfirmDeleteId(null)
    setMenuOpenId(prev => (prev === id ? null : id))
  }

  function startRename(conv: Conversation) {
    setError('')
    setMenuOpenId(null)
    setConfirmDeleteId(null)
    setRenameValue(conv.subject)
    renamingIdRef.current = conv.id
    setRenamingId(conv.id)
  }

  function cancelRename() {
    renamingIdRef.current = null
    setRenamingId(null)
    setRenameValue('')
  }

  async function commitRename(conv: Conversation) {
    if (renamingIdRef.current !== conv.id) return

    const subject = renameValue.trim()
    if (subject === '') {
      setError(t('errors.nameRequired'))
      return
    }
    if (subject === conv.subject) {
      cancelRename()
      return
    }

    const previous = conversations
    renamingIdRef.current = null
    setRenamingId(null)
    setRenameValue('')
    setError('')
    setConversations(prev => prev.map(c => (c.id === conv.id ? { ...c, subject } : c)))

    try {
      const res = await fetch(`/api/homework/conversations/${conv.id}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ subject }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToRename'))
      }
    } catch (err) {
      setConversations(previous)
      setError(err instanceof Error ? err.message : t('errors.failedToRename'))
    }
  }

  async function deleteConversation(conv: Conversation) {
    const previous = conversations
    setMenuOpenId(null)
    setConfirmDeleteId(null)
    setError('')
    setConversations(prev => prev.filter(c => c.id !== conv.id))

    try {
      const res = await fetch(`/api/homework/conversations/${conv.id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToDelete'))
      }
    } catch (err) {
      setConversations(previous)
      setError(err instanceof Error ? err.message : t('errors.failedToDelete'))
    }
  }

  async function createConversation() {
    setCreating(true)
    setError('')
    try {
      const res = await fetch('/api/homework/conversations', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToCreate'))
      }
      const data = await res.json()
      const conv = data.conversation as Conversation
      navigate(`/homework/${conv.id}`)
    } catch (err) {
      if (err instanceof Error) setError(err.message)
    } finally {
      setCreating(false)
    }
  }

  function formatConversationTime(dateStr: string): string {
    const date = new Date(dateStr)
    const now = new Date()
    const dateLocal = new Date(date.getFullYear(), date.getMonth(), date.getDate())
    const nowLocal = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    const diffDays = Math.round((nowLocal.getTime() - dateLocal.getTime()) / (1000 * 60 * 60 * 24))

    if (diffDays === 0) {
      return fmtTime(date, { hour: '2-digit', minute: '2-digit' })
    }
    if (diffDays === 1) return t('yesterday')
    if (diffDays < 7) {
      return formatDate(date, { weekday: 'short' })
    }
    return formatDate(date, { month: 'short', day: 'numeric' })
  }

  return (
    <div className="max-w-2xl mx-auto px-4 py-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-semibold">{t('title')}</h1>
        <div className="flex items-center gap-2">
          <button
            onClick={() => navigate('/homework/settings')}
            className="p-2 rounded-lg hover:bg-gray-800 transition-colors cursor-pointer text-gray-400 hover:text-white"
            aria-label={t('settings.title')}
          >
            <Settings size={20} />
          </button>
          <button
            onClick={createConversation}
            disabled={creating}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {creating ? (
              <Loader2 size={16} className="animate-spin" />
            ) : (
              <Plus size={16} />
            )}
            {t('newConversation')}
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {loading ? (
        <div className="space-y-2" role="status" aria-busy="true">
          <span className="sr-only">{t('loading')}</span>
          {Array.from({ length: 5 }).map((_, i) => (
            <SkeletonRow key={i} />
          ))}
        </div>
      ) : conversations.length === 0 ? (
        <div className="text-center text-gray-500 py-12">
          <BookOpen size={48} className="mx-auto mb-4 opacity-30" />
          <p className="text-lg">{t('empty.noConversations')}</p>
          <p className="text-sm mt-1">{t('empty.startNew')}</p>
        </div>
      ) : (
        <div className="space-y-2">
          {conversations.map(conv => (
            <div
              key={conv.id}
              className="relative flex items-center gap-1 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors"
            >
              {renamingId === conv.id ? (
                <div className="flex-1 flex items-center gap-3 px-4 py-3 min-w-0">
                  <BookOpen size={20} className="shrink-0 text-blue-400" />
                  <input
                    autoFocus
                    value={renameValue}
                    aria-label={t('renameLabel')}
                    onChange={e => setRenameValue(e.target.value)}
                    onFocus={e => e.currentTarget.select()}
                    onBlur={() => commitRename(conv)}
                    onKeyDown={e => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        commitRename(conv)
                      } else if (e.key === 'Escape') {
                        e.preventDefault()
                        cancelRename()
                      }
                    }}
                    className="flex-1 min-w-0 bg-gray-900 border border-gray-600 rounded px-2 py-1 text-sm text-white focus:outline-none focus:border-blue-500"
                  />
                </div>
              ) : (
                <button
                  onClick={() => navigate(`/homework/${conv.id}`)}
                  className="flex-1 flex items-center gap-3 px-4 py-3 text-left cursor-pointer min-w-0"
                >
                  <BookOpen size={20} className="shrink-0 text-blue-400" />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline justify-between gap-2">
                      <p className="text-sm font-medium truncate">
                        {conv.subject || t('noSubject')}
                      </p>
                      <p className="text-xs text-gray-500 shrink-0">
                        {formatConversationTime(conv.updated_at)}
                      </p>
                    </div>
                    {conv.last_message_preview && (
                      <p className="text-xs text-gray-500 mt-0.5 truncate">
                        {conv.last_message_preview}
                      </p>
                    )}
                  </div>
                </button>
              )}

              <button
                ref={el => { menuTriggerRefs.current[conv.id] = el }}
                onClick={() => toggleMenu(conv.id)}
                onKeyDown={e => {
                  if (e.key === 'Escape' && menuOpenId === conv.id) {
                    e.preventDefault()
                    closeMenu(true)
                  }
                }}
                aria-haspopup="menu"
                aria-expanded={menuOpenId === conv.id}
                aria-label={t('conversationActions')}
                className="shrink-0 mr-2 p-2 rounded-lg text-gray-400 hover:text-white hover:bg-gray-600 transition-colors cursor-pointer"
              >
                <MoreVertical size={16} />
              </button>

              {menuOpenId === conv.id && (
                <div
                  ref={menuRef}
                  role="menu"
                  onKeyDown={e => {
                    if (e.key === 'Escape') {
                      e.preventDefault()
                      closeMenu(true)
                    }
                  }}
                  className="absolute right-2 top-full mt-1 z-10 bg-gray-800 border border-gray-700 rounded-lg shadow-lg min-w-[180px] py-1"
                >
                  {confirmDeleteId === conv.id ? (
                    <div className="px-3 py-2">
                      <p className="text-xs text-gray-300 mb-2">{t('deleteConfirm')}</p>
                      <div className="flex items-center gap-2">
                        <button
                          role="menuitem"
                          onClick={() => deleteConversation(conv)}
                          className="px-2 py-1 text-xs rounded bg-red-600 hover:bg-red-500 text-white transition-colors cursor-pointer"
                        >
                          {t('deleteConfirmYes')}
                        </button>
                        <button
                          role="menuitem"
                          onClick={() => setConfirmDeleteId(null)}
                          className="px-2 py-1 text-xs rounded bg-gray-700 hover:bg-gray-600 text-gray-200 transition-colors cursor-pointer"
                        >
                          {t('cancel')}
                        </button>
                      </div>
                    </div>
                  ) : (
                    <>
                      <button
                        role="menuitem"
                        onClick={() => startRename(conv)}
                        className="w-full flex items-center gap-2 px-3 py-2 text-sm text-gray-200 hover:bg-gray-700 transition-colors cursor-pointer"
                      >
                        <Pencil size={16} />
                        {t('rename')}
                      </button>
                      <button
                        role="menuitem"
                        onClick={() => setConfirmDeleteId(conv.id)}
                        className="w-full flex items-center gap-2 px-3 py-2 text-sm text-red-400 hover:bg-gray-700 transition-colors cursor-pointer"
                      >
                        <Trash2 size={16} />
                        {t('delete')}
                      </button>
                    </>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
