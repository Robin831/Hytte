import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus, BookOpen, Loader2, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime as fmtTime } from '../utils/formatDate'

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

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 size={32} className="animate-spin text-gray-400" />
      </div>
    )
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

      {conversations.length === 0 ? (
        <div className="text-center text-gray-500 py-12">
          <BookOpen size={48} className="mx-auto mb-4 opacity-30" />
          <p className="text-lg">{t('empty.noConversations')}</p>
          <p className="text-sm mt-1">{t('empty.startNew')}</p>
        </div>
      ) : (
        <div className="space-y-2">
          {conversations.map(conv => (
            <button
              key={conv.id}
              onClick={() => navigate(`/homework/${conv.id}`)}
              className="w-full flex items-center gap-3 px-4 py-3 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors text-left cursor-pointer"
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
          ))}
        </div>
      )}
    </div>
  )
}
