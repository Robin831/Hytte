import React, { useState, useEffect, useRef, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import {
  Plus,
  Trash2,
  Send,
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
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface Conversation {
  id: number
  user_id: number
  title: string
  model: string
  created_at: string
  updated_at: string
}

interface Message {
  id: number
  conversation_id: number
  role: 'user' | 'assistant'
  content: string
  created_at: string
}

export default function Chat() {
  const { t } = useTranslation('chat')
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [activeConversation, setActiveConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [loadingConversations, setLoadingConversations] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  const [sendingConversationId, setSendingConversationId] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [showSidebar, setShowSidebar] = useState(true)
  const [renamingId, setRenamingId] = useState<number | null>(null)
  const [renameTitle, setRenameTitle] = useState('')
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  useEffect(() => {
    scrollToBottom()
  }, [messages, scrollToBottom])

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
          setError(err.message)
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
    const controller = new AbortController()
    ;(async () => {
      if (activeConversationId === null) {
        setMessages([])
        setLoadingMessages(false)
        setError('')
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
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoadingMessages(false)
      }
    })()
    return () => { controller.abort() }
  }, [activeConversationId, t])

  // Resize textarea when input changes (including programmatic clears)
  useEffect(() => {
    const el = inputRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 160) + 'px'
    }
  }, [input])

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
        body: JSON.stringify({}),
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
      setError('')
      inputRef.current?.focus()
    } catch (err) {
      if (err instanceof Error) setError(err.message)
    }
  }

  async function deleteConversation(id: number) {
    setError('')
    try {
      const res = await fetch(`/api/chat/conversations/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToDelete'))
      setConversations(prev => prev.filter(c => c.id !== id))
      if (activeConversation?.id === id) {
        setActiveConversation(null)
        setMessages([])
      }
      setDeletingId(null)
      setError('')
    } catch (err) {
      if (err instanceof Error) setError(err.message)
    }
  }

  async function renameConversation(id: number, title: string) {
    if (!title.trim()) {
      setRenamingId(null)
      return
    }
    setError('')
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
      setError('')
    } catch (err) {
      if (err instanceof Error) setError(err.message)
    }
  }

  async function sendMessage() {
    if (!input.trim() || !activeConversation || sendingConversationId === activeConversation.id) return
    const content = input.trim()
    // Capture conversation id at send time to guard against mid-flight switches
    const sentConversationId = activeConversation.id
    setInput('')
    setSendingConversationId(sentConversationId)
    setError('')

    // Optimistic: add user message immediately
    const tempUserMsg: Message = {
      id: -Date.now(),
      conversation_id: sentConversationId,
      role: 'user',
      content,
      created_at: new Date().toISOString(),
    }
    setMessages(prev => [...prev, tempUserMsg])

    try {
      const res = await fetch(`/api/chat/conversations/${sentConversationId}/messages`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToSend'))
      }
      const data = await res.json()
      const userMsg = data.user_message as Message
      const assistantMsg = data.assistant_message as Message

      // Only update messages if the user hasn't switched conversations
      setActiveConversation(current => {
        if (current?.id !== sentConversationId) return current
        setMessages(prev => [
          ...prev.filter(m => m.id !== tempUserMsg.id),
          userMsg,
          assistantMsg,
        ])
        return current
      })

      // Refresh conversation list to pick up auto-title updates (non-fatal)
      try {
        const convRes = await fetch('/api/chat/conversations', { credentials: 'include' })
        if (convRes.ok) {
          const convData = await convRes.json()
          setConversations(convData.conversations ?? [])
          const updated = (convData.conversations ?? []).find(
            (c: Conversation) => c.id === sentConversationId
          )
          if (updated) {
            setActiveConversation(current =>
              current?.id === sentConversationId ? updated : current
            )
          }
        }
      } catch (refreshErr) {
        // Non-fatal: log but don't roll back successful send
        console.error('Failed to refresh conversations after sending message', refreshErr)
      }
    } catch (err) {
      // Remove optimistic message on error and restore draft
      setMessages(prev => prev.filter(m => m.id !== tempUserMsg.id))
      setInput(content)
      if (err instanceof Error) setError(err.message)
    } finally {
      setSendingConversationId(null)
      inputRef.current?.focus()
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  function selectConversation(conv: Conversation) {
    setActiveConversation(conv)
    setShowSidebar(false)
    setError('')
  }

  function formatTime(dateStr: string): string {
    const date = new Date(dateStr)
    const now = new Date()

    // Use local dates for day comparison to avoid UTC boundary issues
    const dateLocal = new Date(date.getFullYear(), date.getMonth(), date.getDate())
    const nowLocal = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    const diffDays = Math.round((nowLocal.getTime() - dateLocal.getTime()) / (1000 * 60 * 60 * 24))

    if (diffDays === 0) {
      return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    }
    if (diffDays === 1) return t('yesterday')
    if (diffDays < 7) {
      return date.toLocaleDateString(undefined, { weekday: 'short' })
    }
    return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }

  const conversationTitle = (conv: Conversation) => conv.title || t('newConversation')

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
          {activeConversation ? (
            <div className="min-w-0">
              <h2 className="text-sm font-medium truncate">
                {conversationTitle(activeConversation)}
              </h2>
              <p className="text-xs text-gray-500">{activeConversation.model}</p>
            </div>
          ) : (
            <h2 className="text-sm font-medium text-gray-400">{t('header.selectOrStart')}</h2>
          )}
        </div>

        {/* Messages area */}
        <div className="flex-1 overflow-y-auto">
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
              {messages.map(msg => (
                <MessageBubble key={msg.id} message={msg} />
              ))}
              {sendingConversationId === activeConversation?.id && (
                <div className="flex items-start gap-3">
                  <div className="w-8 h-8 rounded-full bg-purple-600/20 flex items-center justify-center shrink-0">
                    <Bot size={16} className="text-purple-400" />
                  </div>
                  <div className="bg-gray-800 rounded-2xl rounded-tl-sm px-4 py-3">
                    <div className="flex items-center gap-2 text-gray-400">
                      <Loader2 size={14} className="animate-spin" />
                      <span className="text-sm">{t('thinking')}</span>
                    </div>
                  </div>
                </div>
              )}
              <div ref={messagesEndRef} />
            </div>
          )}
        </div>

        {/* Error banner */}
        {error && (
          <div className="px-4 py-2 bg-red-900/50 border-t border-red-800 text-red-300 text-sm flex items-center justify-between">
            <span>{error}</span>
            <button
              onClick={() => setError('')}
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
                disabled={sendingConversationId === activeConversation?.id}
                onInput={e => {
                  const el = e.currentTarget
                  el.style.height = 'auto'
                  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
                }}
              />
              <button
                onClick={sendMessage}
                disabled={!input.trim() || sendingConversationId === activeConversation?.id}
                className="self-end p-3 rounded-xl bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
                title={t('input.sendLabel')}
              >
                {sendingConversationId === activeConversation?.id ? <Loader2 size={18} className="animate-spin" /> : <Send size={18} />}
              </button>
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
