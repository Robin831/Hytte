import React, { useState, useEffect, useRef, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import {
  Plus,
  Trash2,
  Send,
  Square,
  MessageSquare,
  Pencil,
  Check,
  X,
  ChevronLeft,
  Loader2,
  Bot,
  User,
  Copy,
  CheckCheck,
  ArrowDown,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate, formatTime as fmtTime } from '../utils/formatDate'
import { useChatStream } from '../hooks/useChatStream'
import type { Conversation, Message } from '../hooks/useChatStream'

// Distance from the bottom (px) within which the messages view is considered
// "pinned": new streamed tokens keep the view glued to the latest message.
// Past this, the user has scrolled up to read history and we leave them be.
const PIN_THRESHOLD = 80

// MODEL_OPTIONS is the fixed allowlist of Claude models offered in the header
// dropdown. Keep in sync with the backend allowlist (internal/chat/handlers.go
// SupportedModels). The default for new conversations is Sonnet.
const MODEL_OPTIONS = ['claude-opus-4-8', 'claude-sonnet-4-6', 'claude-haiku-4-5'] as const
const DEFAULT_MODEL = 'claude-sonnet-4-6'

// modelLabelKey maps a model ID to its i18n label key by family so any
// supported variant (e.g. an older Opus) still shows a friendly name.
function modelLabelKey(model: string): string {
  if (model.startsWith('claude-opus')) return 'models.opus'
  if (model.startsWith('claude-sonnet')) return 'models.sonnet'
  if (model.startsWith('claude-haiku')) return 'models.haiku'
  return ''
}

export default function Chat() {
  const { t } = useTranslation('chat')
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeConversation, setActiveConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [localError, setLocalError] = useState('')
  const [loadingConversations, setLoadingConversations] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  // Track every conversation with an in-flight send so concurrent sends in
  // different conversations don't clobber each other's pending state. Kept
  // immutably (a fresh Set on each update) so React re-renders fire.
  const [sendingConversationIds, setSendingConversationIds] = useState<Set<number>>(new Set())
  const [showSidebar, setShowSidebar] = useState(true)
  const [renamingId, setRenamingId] = useState<number | null>(null)
  const [renameTitle, setRenameTitle] = useState('')
  // Model chosen for the next new conversation (used when none is active).
  const [newConversationModel, setNewConversationModel] = useState<string>(DEFAULT_MODEL)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)
  // Whether the messages view is glued to the bottom. `isPinned` drives the
  // "jump to latest" button; `isPinnedRef` mirrors it so the streaming-token
  // scroll effect reads the latest value without re-subscribing.
  const [isPinned, setIsPinned] = useState(true)
  const isPinnedRef = useRef(true)
  // Set when the next scroll-to-bottom should jump instantly (no smooth
  // animation) — used on conversation switch / initial load so the button
  // does not flash while a long history animates into view.
  const instantScrollRef = useRef(false)
  // Suppresses unpin detection in the scroll handler while a smooth
  // jump-to-latest scroll is in flight — cleared once the container
  // actually reaches the pinned zone.
  const jumpingRef = useRef(false)
  // Track locally deleted conversation IDs so we don't resurrect them if a
  // send response arrives after the user deleted the conversation mid-flight.
  const deletedConversationIds = useRef<Set<number>>(new Set())

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    messagesEndRef.current?.scrollIntoView({ behavior })
  }, [])

  const jumpToLatest = useCallback(() => {
    jumpingRef.current = true
    isPinnedRef.current = true
    setIsPinned(true)
    scrollToBottom('smooth')
  }, [scrollToBottom])

  const addSendingConversation = useCallback((id: number) => {
    setSendingConversationIds(prev => {
      const next = new Set(prev)
      next.add(id)
      return next
    })
  }, [])

  const removeSendingConversation = useCallback((id: number) => {
    setSendingConversationIds(prev => {
      const next = new Set(prev)
      next.delete(id)
      return next
    })
  }, [])

  // Re-pin the messages view to the bottom — used when a send begins so the
  // user's new message and the streamed reply stay in view.
  const pinToBottom = useCallback(() => {
    isPinnedRef.current = true
    setIsPinned(true)
  }, [])

  const focusInput = useCallback(() => {
    inputRef.current?.focus()
  }, [])

  // SSE streaming state machine: optimistic placeholders, frame parsing, the
  // three error-recovery branches, abort handling, and the post-stream
  // conversation-list refetch all live in the hook. Chat keeps only UI state.
  const { send, stop, error, clearError } = useChatStream({
    activeConversation,
    setActiveConversation,
    setMessages,
    setConversations,
    sendingConversationIds,
    addSendingConversation,
    removeSendingConversation,
    deletedConversationIds,
    setInput,
    pinToBottom,
    focusInput,
  })

  // Keep the view glued to the latest message only while the user is pinned
  // near the bottom. If they have scrolled up to read history, leave their
  // position untouched so streamed tokens no longer yank them back down.
  useEffect(() => {
    if (isPinnedRef.current) {
      scrollToBottom(instantScrollRef.current ? 'auto' : 'smooth')
    }
    instantScrollRef.current = false
  }, [messages, scrollToBottom])

  // Track how far the messages container is from the bottom so we know whether
  // to auto-scroll and whether to show the "jump to latest" button.
  useEffect(() => {
    const el = scrollContainerRef.current
    if (!el) return
    const handleScroll = () => {
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
      const pinned = distanceFromBottom <= PIN_THRESHOLD
      if (jumpingRef.current) {
        if (pinned) {
          jumpingRef.current = false
        } else {
          return
        }
      }
      if (pinned !== isPinnedRef.current) {
        isPinnedRef.current = pinned
        setIsPinned(pinned)
      }
    }
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [])

  // On conversation switch, re-pin to the bottom and scroll instantly so the
  // latest message is shown without animating a long history.
  useEffect(() => {
    isPinnedRef.current = true
    setIsPinned(true) // eslint-disable-line react-hooks/set-state-in-effect -- resetting derived state on key change
    instantScrollRef.current = true
  }, [activeConversation?.id])

  // Load conversations
  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/chat/conversations', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoad'))
        const data = await res.json()
        setConversations(data.conversations ?? [])
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setLocalError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoadingConversations(false)
      }
    })()
    return () => { controller.abort() }
  }, [t])

  // Load messages when conversation changes
  const activeConversationId = activeConversation?.id ?? null
  useEffect(() => {
    // Cancel any in-flight streaming send when switching conversations so the
    // partial tokens do not bleed into the newly selected conversation.
    stop()
    clearError()

    const controller = new AbortController()
    ;(async () => {
      if (activeConversationId === null) {
        setMessages([])
        setLoadingMessages(false)
        setLocalError('')
        return
      }
      setLoadingMessages(true)
      try {
        const res = await fetch(`/api/chat/conversations/${activeConversationId}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoadMessages'))
        const data = await res.json()
        setMessages(data.messages ?? [])
        setLocalError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setLocalError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoadingMessages(false)
      }
    })()
    return () => { controller.abort() }
  }, [activeConversationId, t, stop, clearError])

  // Resize textarea when input changes (including programmatic clears)
  useEffect(() => {
    const el = inputRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 160) + 'px'
    }
  }, [input])

  // Abort any in-flight streaming send when the component unmounts so the
  // background fetch does not keep running with a stale state ref.
  useEffect(() => {
    return () => { stop() }
  }, [stop])

  // Focus rename input when entering rename mode
  useEffect(() => {
    if (renamingId !== null) {
      renameInputRef.current?.focus()
      renameInputRef.current?.select()
    }
  }, [renamingId])

  async function createConversation() {
    try {
      const res = await fetch('/api/chat/conversations', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: newConversationModel }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToCreate'))
      }
      const data = await res.json()
      const conv = data.conversation as Conversation
      setConversations(prev => [conv, ...prev])
      setActiveConversation(conv)
      setShowSidebar(false)
      setLocalError('')
      inputRef.current?.focus()
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
    }
  }

  async function updateModel(id: number, model: string) {
    setError('')
    const prevModel = activeConversation?.model
    if (model === prevModel) return
    // Optimistically reflect the choice so the dropdown updates immediately.
    setActiveConversation(current => (current && current.id === id ? { ...current, model } : current))
    setConversations(prev => prev.map(c => (c.id === id ? { ...c, model } : c)))
    try {
      const res = await fetch(`/api/chat/conversations/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model }),
      })
      if (!res.ok) throw new Error(t('errors.failedToUpdateModel'))
      const data = await res.json()
      const updated = data.conversation as Conversation
      setConversations(prev => prev.map(c => (c.id === id ? updated : c)))
      setActiveConversation(current => (current?.id === id ? updated : current))
    } catch (err) {
      // Revert the optimistic update on failure.
      if (prevModel !== undefined) {
        setActiveConversation(current => (current && current.id === id ? { ...current, model: prevModel } : current))
        setConversations(prev => prev.map(c => (c.id === id ? { ...c, model: prevModel } : c)))
      }
      if (err instanceof Error) setError(err.message)
    }
  }

  async function deleteConversation(id: number) {
    setLocalError('')
    try {
      const res = await fetch(`/api/chat/conversations/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToDelete'))
      deletedConversationIds.current.add(id)
      setConversations(prev => prev.filter(c => c.id !== id))
      if (activeConversation?.id === id) {
        setActiveConversation(null)
        setMessages([])
      }
      setDeletingId(null)
      setLocalError('')
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
    }
  }

  async function renameConversation(id: number, title: string) {
    if (!title.trim()) {
      setRenamingId(null)
      return
    }
    setLocalError('')
    try {
      const res = await fetch(`/api/chat/conversations/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: title.trim() }),
      })
      if (!res.ok) throw new Error(t('errors.failedToRename'))
      const data = await res.json()
      const updated = data.conversation as Conversation
      setConversations(prev => prev.map(c => c.id === id ? updated : c))
      if (activeConversation?.id === id) {
        setActiveConversation(updated)
      }
      setRenamingId(null)
      setLocalError('')
    } catch (err) {
      if (err instanceof Error) setLocalError(err.message)
    }
  }

  // Thin wrapper around the hook's send: the trimmed input is the message
  // content; the hook owns the optimistic rows, streaming, and reconciliation.
  function handleSend() {
    send(input.trim())
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  function selectConversation(conv: Conversation) {
    setActiveConversation(conv)
    setShowSidebar(false)
    clearError()
    setLocalError('')
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

  const conversationTitle = (conv: Conversation) => conv.title || t('newConversation')

  // Options for the header model dropdown. If the current selection isn't one
  // of the fixed options (e.g. an existing conversation on an older variant),
  // prepend it so the select still reflects the real value.
  const modelOptionsFor = (current: string): string[] =>
    MODEL_OPTIONS.includes(current as (typeof MODEL_OPTIONS)[number])
      ? [...MODEL_OPTIONS]
      : [current, ...MODEL_OPTIONS]

  const modelLabel = (model: string): string => {
    const key = modelLabelKey(model)
    return key ? t(key) : model
  }

  const selectedModel = activeConversation ? activeConversation.model : newConversationModel
  const modelSelectDisabled =
    activeConversation != null && sendingConversationIds.has(activeConversation.id)
  const onModelChange = (model: string) => {
    if (activeConversation) {
      updateModel(activeConversation.id, model)
    } else {
      setNewConversationModel(model)
    }
  }

  const modelSelector = (
    <select
      value={selectedModel}
      onChange={e => onModelChange(e.target.value)}
      disabled={modelSelectDisabled}
      aria-label={t('header.modelLabel')}
      title={t('header.modelLabel')}
      className="shrink-0 bg-gray-800 border border-gray-600 rounded-lg px-2 py-1 text-xs text-gray-200 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
    >
      {modelOptionsFor(selectedModel).map(m => (
        <option key={m} value={m}>
          {modelLabel(m)}
        </option>
      ))}
    </select>
  )

  // Conversation sidebar panel
  const sidebarContent = (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between p-4 border-b border-gray-700">
        <h2 className="text-lg font-semibold">{t('title')}</h2>
        <button
          onClick={createConversation}
          className="p-2 rounded-lg bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer"
          title={t('newChat')}
          aria-label={t('newChat')}
        >
          <Plus size={18} />
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {loadingConversations ? (
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
                activeConversation?.id === conv.id
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-300 hover:bg-gray-800'
              }`}
              onClick={() => {
                if (renamingId !== conv.id && deletingId !== conv.id) {
                  selectConversation(conv)
                }
              }}
              onKeyDown={e => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  if (renamingId !== conv.id && deletingId !== conv.id) {
                    selectConversation(conv)
                  }
                }
              }}
            >
              {renamingId === conv.id ? (
                <form
                  className="flex-1 flex items-center gap-1"
                  onSubmit={e => {
                    e.preventDefault()
                    renameConversation(conv.id, renameTitle)
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
                      deleteConversation(conv.id)
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
                    <p className="text-sm truncate">{conversationTitle(conv)}</p>
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
  )

  return (
    <div className="flex h-[calc(100vh-3.5rem)] md:h-screen">
      {/* Conversation sidebar — desktop always visible, mobile toggled */}
      <div
        className={`${
          showSidebar ? 'flex' : 'hidden'
        } md:flex flex-col w-full md:w-72 lg:w-80 border-r border-gray-700 shrink-0 bg-gray-900`}
      >
        {sidebarContent}
      </div>

      {/* Main chat area */}
      <div
        className={`${
          showSidebar ? 'hidden' : 'flex'
        } md:flex flex-col flex-1 min-w-0`}
      >
        {/* Chat header */}
        <div className="flex items-center gap-3 px-4 h-14 border-b border-gray-700 shrink-0 bg-gray-900">
          <button
            onClick={() => setShowSidebar(true)}
            className="md:hidden p-1 text-gray-400 hover:text-white cursor-pointer"
            aria-label={t('conversation.backLabel')}
          >
            <ChevronLeft size={20} />
          </button>
          <div className="min-w-0 flex-1">
            {activeConversation ? (
              <h2 className="text-sm font-medium truncate">
                {conversationTitle(activeConversation)}
              </h2>
            ) : (
              <h2 className="text-sm font-medium text-gray-400 truncate">{t('header.selectOrStart')}</h2>
            )}
          </div>
          {modelSelector}
        </div>

        {/* Messages area */}
        <div className="relative flex-1 min-h-0">
        <div ref={scrollContainerRef} className="h-full overflow-y-auto">
          {!activeConversation ? (
            <div className="flex flex-col items-center justify-center h-full text-gray-500">
              <Bot size={48} className="mb-4 opacity-30" />
              <p className="text-lg font-medium">{t('welcome.title')}</p>
              <p className="text-sm mt-1">{t('welcome.subtitle')}</p>
              <button
                onClick={createConversation}
                className="mt-4 flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer"
              >
                <Plus size={16} />
                {t('newChat')}
              </button>
            </div>
          ) : loadingMessages ? (
            <div className="flex justify-center py-12">
              <Loader2 size={32} className="animate-spin text-gray-400" />
            </div>
          ) : messages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-gray-500">
              <Bot size={40} className="mb-3 opacity-30" />
              <p className="text-sm">{t('emptyMessages')}</p>
            </div>
          ) : (
            <div className="max-w-3xl mx-auto px-4 py-6 space-y-6">
              {messages.map(msg => {
                // While streaming, the assistant placeholder is the last
                // assistant row with negative id. Show the "streaming…"
                // indicator only when its content is still empty so once
                // tokens arrive the user sees the live text.
                const isStreamingPlaceholder =
                  activeConversation != null &&
                  sendingConversationIds.has(activeConversation.id) &&
                  msg.role === 'assistant' &&
                  msg.id < 0 &&
                  msg.content === ''
                if (isStreamingPlaceholder) {
                  return (
                    <div key={msg.id} className="flex items-start gap-3">
                      <div className="w-8 h-8 rounded-full bg-purple-600/20 flex items-center justify-center shrink-0">
                        <Bot size={16} className="text-purple-400" />
                      </div>
                      <div className="bg-gray-800 rounded-2xl rounded-tl-sm px-4 py-3">
                        <div className="flex items-center gap-2 text-gray-400">
                          <Loader2 size={14} className="animate-spin" />
                          <span className="text-sm">{t('streamingIndicator')}</span>
                        </div>
                      </div>
                    </div>
                  )
                }
                return <MessageBubble key={msg.id} message={msg} />
              })}
              <div ref={messagesEndRef} />
            </div>
          )}
        </div>
          {/* Jump to latest — shown only while scrolled away from the bottom */}
          {activeConversation && messages.length > 0 && !isPinned && (
            <button
              onClick={jumpToLatest}
              className="absolute bottom-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-3 py-2 rounded-full bg-gray-800 hover:bg-gray-700 text-gray-200 text-xs font-medium shadow-lg border border-gray-700 transition-colors cursor-pointer"
              aria-label={t('jumpToLatest')}
            >
              <ArrowDown size={14} />
              {t('jumpToLatest')}
            </button>
          )}
        </div>

        {/* Error banner */}
        {(error || localError) && (
          <div className="px-4 py-2 bg-red-900/50 border-t border-red-800 text-red-300 text-sm flex items-center justify-between">
            <span>{error || localError}</span>
            <button
              onClick={() => { clearError(); setLocalError('') }}
              className="text-red-400 hover:text-red-300 cursor-pointer"
              aria-label={t('input.dismissError')}
            >
              <X size={14} />
            </button>
          </div>
        )}

        {/* Input area */}
        {activeConversation && (
          <div className="border-t border-gray-700 bg-gray-900 p-4">
            <div className="max-w-3xl mx-auto flex gap-2">
              <textarea
                ref={inputRef}
                value={input}
                onChange={e => setInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={t('input.placeholder')}
                rows={1}
                className="flex-1 bg-gray-800 border border-gray-600 rounded-xl px-4 py-3 text-white text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 placeholder-gray-500 max-h-40 overflow-y-auto"
                style={{ minHeight: '48px' }}
                disabled={sendingConversationIds.has(activeConversation.id)}
                onInput={e => {
                  const el = e.currentTarget
                  el.style.height = 'auto'
                  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
                }}
              />
              {sendingConversationIds.has(activeConversation.id) ? (
                <button
                  onClick={stop}
                  className="self-end p-3 rounded-xl bg-red-600 hover:bg-red-500 text-white transition-colors cursor-pointer shrink-0"
                  title={t('input.stopStreaming')}
                  aria-label={t('input.stopStreaming')}
                  data-testid="chat-stop-button"
                >
                  <Square size={18} />
                </button>
              ) : (
                <button
                  onClick={handleSend}
                  disabled={!input.trim()}
                  className="self-end p-3 rounded-xl bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
                  title={t('input.sendLabel')}
                  aria-label={t('input.sendLabel')}
                  data-testid="chat-send-button"
                >
                  <Send size={18} />
                </button>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function MessageBubble({ message }: { message: Message }) {
  const { t } = useTranslation('chat')
  const isUser = message.role === 'user'
  const [copied, setCopied] = useState(false)
  const timeoutRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current)
      }
    }
  }, [])

  async function copyContent() {
    try {
      await navigator.clipboard.writeText(message.content)
      setCopied(true)
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current)
      }
      timeoutRef.current = window.setTimeout(() => {
        setCopied(false)
        timeoutRef.current = null
      }, 2000)
    } catch {
      // clipboard API may not be available
    }
  }

  if (isUser) {
    return (
      <div className="flex items-start gap-3 justify-end">
        <div className="bg-blue-600 rounded-2xl rounded-tr-sm px-4 py-3 max-w-[85%]">
          <p className="text-sm text-white whitespace-pre-wrap break-words">{message.content}</p>
        </div>
        <div className="w-8 h-8 rounded-full bg-blue-600/20 flex items-center justify-center shrink-0">
          <User size={16} className="text-blue-400" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex items-start gap-3 group">
      <div className="w-8 h-8 rounded-full bg-purple-600/20 flex items-center justify-center shrink-0">
        <Bot size={16} className="text-purple-400" />
      </div>
      <div className="bg-gray-800 rounded-2xl rounded-tl-sm px-4 py-3 max-w-[85%] min-w-0">
        <div className="prose prose-invert prose-sm max-w-none break-words [&_pre]:overflow-x-auto [&_pre]:max-w-full [&_code]:break-words">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              a({ href, children }: React.ComponentPropsWithoutRef<'a'>) {
                return (
                  <a
                    href={href}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-blue-400 hover:text-blue-300 underline"
                  >
                    {children}
                  </a>
                )
              },
              code({ className, children }: React.ComponentPropsWithoutRef<'code'> & { node?: unknown }) {
                const match = /language-(\S+)/.exec(className || '')
                const codeStr = String(children).replace(/\n$/, '')
                const isBlock = codeStr.includes('\n') || match !== null
                if (isBlock) {
                  return (
                    <SyntaxHighlighter
                      style={vscDarkPlus as unknown as { [key: string]: React.CSSProperties }}
                      language={match?.[1] || 'text'}
                      PreTag="div"
                      customStyle={{
                        margin: 0,
                        borderRadius: '0.5rem',
                        fontSize: '0.8125rem',
                      }}
                    >
                      {codeStr}
                    </SyntaxHighlighter>
                  )
                }
                return (
                  <code className="bg-gray-700 px-1.5 py-0.5 rounded text-sm">
                    {children}
                  </code>
                )
              },
            }}
          >
            {message.content}
          </ReactMarkdown>
        </div>
        <button
          onClick={copyContent}
          className="mt-2 opacity-100 sm:opacity-0 sm:group-hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-800 transition-opacity text-gray-500 hover:text-gray-300 cursor-pointer"
          title={t('input.copyMessage')}
          aria-label={t('input.copyMessage')}
        >
          {copied ? <CheckCheck size={14} className="text-green-400" /> : <Copy size={14} />}
        </button>
      </div>
    </div>
  )
}
