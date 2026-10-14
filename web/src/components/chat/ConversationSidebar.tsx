import { memo, useEffect, useRef, useState } from 'react'
import { Check, Loader2, MessageSquare, Pencil, Plus, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime as fmtTime } from '../../utils/formatDate'
import type { ConversationSidebarProps } from './types'

// Conversation list with the new-chat button and the inline rename / delete
// confirmation UI. Rename and delete state is purely presentational, so it
// lives here; the parent owns the requests and the conversation data.
function ConversationSidebar({
  visibleOnMobile,
  conversations,
  activeId,
  loading,
  onSelect,
  onNew,
  onRename,
  onDelete,
}: ConversationSidebarProps) {
  const { t } = useTranslation('chat')
  const [renamingId, setRenamingId] = useState<number | null>(null)
  const [renameTitle, setRenameTitle] = useState('')
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)

  // Focus rename input when entering rename mode
  useEffect(() => {
    if (renamingId !== null) {
      renameInputRef.current?.focus()
      renameInputRef.current?.select()
    }
  }, [renamingId])

  async function submitRename(id: number, title: string) {
    if (await onRename(id, title)) setRenamingId(null)
  }

  async function confirmDelete(id: number) {
    if (await onDelete(id)) setDeletingId(null)
  }

  function formatTime(dateStr: string): string {
    const date = new Date(dateStr)
    const now = new Date()

    // Use local dates for day comparison to avoid UTC boundary issues
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
    // Desktop always visible, mobile toggled
    <div
      className={`${
        visibleOnMobile ? 'flex' : 'hidden'
      } md:flex flex-col w-full md:w-72 lg:w-80 border-r border-gray-700 shrink-0 bg-gray-900`}
    >
      <div className="flex flex-col h-full">
        <div className="flex items-center justify-between p-4 border-b border-gray-700">
          <h2 className="text-lg font-semibold">{t('title')}</h2>
          <button
            onClick={onNew}
            className="p-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer"
            title={t('newChat')}
            aria-label={t('newChat')}
          >
            <Plus size={18} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {loading ? (
            <div className="flex justify-center py-8">
              <Loader2 size={24} className="animate-spin text-gray-400" />
            </div>
          ) : conversations.length === 0 ? (
            <div className="text-center text-gray-500 py-8 px-4">
              <MessageSquare size={32} className="mx-auto mb-3 opacity-50" />
              <p className="text-sm">{t('empty.noConversations')}</p>
              <p className="text-xs mt-1">{t('empty.startNew')}</p>
            </div>
          ) : (
            conversations.map(conv => (
              <div
                key={conv.id}
                role="button"
                tabIndex={0}
                className={`group flex items-center gap-2 px-3 py-3 mx-2 my-0.5 rounded-lg cursor-pointer transition-colors ${
                  activeId === conv.id
                    ? 'bg-gray-700 text-white'
                    : 'text-gray-300 hover:bg-gray-800'
                }`}
                onClick={() => {
                  if (renamingId !== conv.id && deletingId !== conv.id) {
                    onSelect(conv)
                  }
                }}
                onKeyDown={e => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    if (renamingId !== conv.id && deletingId !== conv.id) {
                      onSelect(conv)
                    }
                  }
                }}
              >
                {renamingId === conv.id ? (
                  <form
                    className="flex-1 flex items-center gap-1"
                    onSubmit={e => {
                      e.preventDefault()
                      submitRename(conv.id, renameTitle)
                    }}
                  >
                    <input
                      ref={renameInputRef}
                      value={renameTitle}
                      onChange={e => setRenameTitle(e.target.value)}
                      aria-label={t('conversation.renameLabel')}
                      className="flex-1 bg-gray-600 border border-gray-500 rounded px-2 py-1 text-sm text-white focus:outline-none focus:ring-1 focus:ring-blue-500"
                      onKeyDown={e => {
                        if (e.key === 'Escape') setRenamingId(null)
                      }}
                    />
                    <button
                      type="submit"
                      className="p-1 text-green-400 hover:text-green-300 cursor-pointer"
                    >
                      <Check size={14} />
                    </button>
                    <button
                      type="button"
                      onClick={() => setRenamingId(null)}
                      className="p-1 text-gray-400 hover:text-gray-300 cursor-pointer"
                    >
                      <X size={14} />
                    </button>
                  </form>
                ) : deletingId === conv.id ? (
                  <div className="flex-1 flex items-center gap-2">
                    <span className="text-sm text-red-400 truncate">{t('conversation.confirmDelete')}</span>
                    <button
                      onClick={e => {
                        e.stopPropagation()
                        confirmDelete(conv.id)
                      }}
                      className="p-1 text-red-400 hover:text-red-300 cursor-pointer"
                    >
                      <Check size={14} />
                    </button>
                    <button
                      onClick={e => {
                        e.stopPropagation()
                        setDeletingId(null)
                      }}
                      className="p-1 text-gray-400 hover:text-gray-300 cursor-pointer"
                    >
                      <X size={14} />
                    </button>
                  </div>
                ) : (
                  <>
                    <MessageSquare size={16} className="shrink-0 text-gray-500" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm truncate">{conv.title || t('newConversation')}</p>
                      <p className="text-xs text-gray-500">{formatTime(conv.updated_at)}</p>
                    </div>
                    <div className="flex sm:hidden sm:group-hover:flex sm:group-focus-within:flex items-center gap-0.5 shrink-0">
                      <button
                        onClick={e => {
                          e.stopPropagation()
                          setRenamingId(conv.id)
                          setRenameTitle(conv.title)
                        }}
                        className="p-1 text-gray-400 hover:text-white cursor-pointer"
                        title={t('conversation.rename')}
                      >
                        <Pencil size={14} />
                      </button>
                      <button
                        onClick={e => {
                          e.stopPropagation()
                          setDeletingId(conv.id)
                        }}
                        className="p-1 text-gray-400 hover:text-red-400 cursor-pointer"
                        title={t('conversation.delete')}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </>
                )}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}

export default memo(ConversationSidebar)
