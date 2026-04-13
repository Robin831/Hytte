import { useState, useEffect, useRef, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import {
  MessageCircle,
  Send,
  Loader2,
  Bot,
  User,
  X,
  ChevronDown,
  RefreshCw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { DayPlan } from '../../types/stride'

export interface StrideChatMessage {
  id: number
  plan_id: number
  role: 'user' | 'assistant'
  content: string
  plan_modified: boolean
  created_at: string
}

interface StrideChatDrawerProps {
  planId: number
  onPlanUpdated: (plan: DayPlan[]) => void
}

// Shared link safety policy: allow https, mailto, and tel schemes
function isSafeLinkHref(href: string | undefined): href is string {
  return typeof href === 'string' && /^(https?:|mailto:|tel:)/i.test(href)
}

export default function StrideChatDrawer({ planId, onPlanUpdated }: StrideChatDrawerProps) {
  const { t } = useTranslation('stride')

  const [messages, setMessages] = useState<StrideChatMessage[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [streamingText, setStreamingText] = useState('')
  const [error, setError] = useState('')
  const [retrying, setRetrying] = useState(false)

  const [planUpdateWarning, setPlanUpdateWarning] = useState('')

  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const sendAbortRef = useRef<AbortController | null>(null)
  const retryTimeoutRef = useRef<number | null>(null)
  const scrollThrottleTimeoutRef = useRef<number | null>(null)
  const planRetryTimeoutRef = useRef<number | null>(null)
  const hasRetriedPlanUpdate = useRef(false)
  const planRetryErrorRef = useRef<string | null>(null)

  useEffect(() => {
    return () => {
      sendAbortRef.current?.abort()
      if (retryTimeoutRef.current !== null) {
        window.clearTimeout(retryTimeoutRef.current)
      }
      if (scrollThrottleTimeoutRef.current !== null) {
        window.clearTimeout(scrollThrottleTimeoutRef.current)
      }
      if (planRetryTimeoutRef.current !== null) {
        window.clearTimeout(planRetryTimeoutRef.current)
      }
    }
  }, [])

  const isNearBottom = useCallback(() => {
    const container = messagesEndRef.current?.parentElement
    if (!container) return true
    const threshold = 80
    const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight
    return distanceFromBottom <= threshold
  }, [])

  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    messagesEndRef.current?.scrollIntoView({ behavior, block: 'end' })
  }, [])

  useEffect(() => {
    if (!isNearBottom()) return

    const behavior: ScrollBehavior = streamingText ? 'auto' : 'smooth'

    if (scrollThrottleTimeoutRef.current !== null) {
      window.clearTimeout(scrollThrottleTimeoutRef.current)
    }

    if (streamingText) {
      scrollThrottleTimeoutRef.current = window.setTimeout(() => {
        scrollToBottom(behavior)
        scrollThrottleTimeoutRef.current = null
      }, 100)

      return () => {
        if (scrollThrottleTimeoutRef.current !== null) {
          window.clearTimeout(scrollThrottleTimeoutRef.current)
          scrollThrottleTimeoutRef.current = null
        }
      }
    }

    scrollToBottom(behavior)
  }, [messages, streamingText, isNearBottom, scrollToBottom])

  // Load message history on mount and planId changes; data is pre-fetched and
  // displayed once the drawer expands (lazy display, eager fetch).
  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setError('')
      try {
        const res = await fetch(`/api/stride/plans/${planId}/chat`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('chat.error'))
        const data = await res.json()
        setMessages(data.messages ?? [])
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      }
    })()
    return () => { controller.abort() }
  }, [planId, t])

  // Auto-resize textarea
  useEffect(() => {
    const el = inputRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 120) + 'px'
    }
  }, [input])

  async function sendMessage(overrideContent?: string) {
    const content = overrideContent ?? input.trim()
    if (!content) return
    if (!overrideContent && sending) return

    if (!overrideContent) {
      setInput('')
      // Reset per-attempt retry guard so each new user send gets one auto-retry.
      hasRetriedPlanUpdate.current = false
      planRetryErrorRef.current = null
    }
    setSending(true)
    setStreamingText('')
    setError('')
    setRetrying(false)

    const tempUserMsg: StrideChatMessage = {
      id: -Date.now(),
      plan_id: planId,
      role: 'user',
      content,
      plan_modified: false,
      created_at: new Date().toISOString(),
    }
    // System-driven retries are invisible to the user — don't show an
    // optimistic user message bubble for them.
    if (!overrideContent) {
      setMessages(prev => [...prev, tempUserMsg])
    }

    const controller = new AbortController()
    sendAbortRef.current = controller

    try {
      const res = await fetch(`/api/stride/plans/${planId}/chat`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content, ...(overrideContent ? { is_internal: true } : {}) }),
        signal: controller.signal,
      })

      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('chat.error'))
      }

      const reader = res.body?.getReader()
      if (!reader) throw new Error(t('chat.error'))

      const decoder = new TextDecoder()
      let buffer = ''
      let accumulatedText = ''
      let eventType = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })

        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (line.startsWith('event: ')) {
            eventType = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            const data = line.slice(6)
            try {
              const parsed = JSON.parse(data)

              switch (eventType) {
                case 'user_message':
                  if (!overrideContent) {
                    setMessages(prev =>
                      prev.map(m => m.id === tempUserMsg.id ? (parsed as StrideChatMessage) : m)
                    )
                  }
                  break
                case 'delta':
                  accumulatedText += parsed.text ?? ''
                  setStreamingText(accumulatedText)
                  break
                case 'plan_updated':
                  hasRetriedPlanUpdate.current = false
                  setPlanUpdateWarning('')
                  onPlanUpdated(parsed.plan)
                  break
                case 'plan_update_failed':
                  if (!hasRetriedPlanUpdate.current) {
                    hasRetriedPlanUpdate.current = true
                    // Queue a correction retry after this stream finishes
                    planRetryErrorRef.current = parsed.error ?? 'unknown error'
                  } else {
                    setPlanUpdateWarning(t('chat.planUpdateFailed'))
                  }
                  break
                case 'done': {
                  const assistantMsg = parsed as StrideChatMessage
                  setMessages(prev => [...prev, assistantMsg])
                  setStreamingText('')
                  setSending(false)
                  await reader.cancel()
                  return
                }
                case 'error':
                  throw new Error(parsed.error || t('chat.error'))
                case 'retry':
                  setRetrying(true)
                  if (retryTimeoutRef.current !== null) {
                    window.clearTimeout(retryTimeoutRef.current)
                  }
                  retryTimeoutRef.current = window.setTimeout(() => {
                    setRetrying(false)
                    retryTimeoutRef.current = null
                  }, 3000)
                  break
              }
            } catch (parseErr) {
              if (eventType === 'error') {
                throw parseErr
              }
            }
            eventType = ''
          }
        }
      }
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setStreamingText('')
      if (!overrideContent) setInput(content)
      if (err instanceof Error) setError(err.message)
    } finally {
      sendAbortRef.current = null
      setSending(false)
      inputRef.current?.focus()

      // If a plan update failed and we haven't retried yet, send a correction prompt.
      const retryError = planRetryErrorRef.current
      if (retryError) {
        planRetryErrorRef.current = null
        planRetryTimeoutRef.current = window.setTimeout(() => {
          planRetryTimeoutRef.current = null
          sendCorrectionMessage(retryError)
        }, 0)
      }
    }
  }

  function sendCorrectionMessage(validationError: string) {
    const correctionPrompt = `The plan update you provided was invalid: ${validationError}. Please output the corrected plan.`
    sendMessage(correctionPrompt)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  if (!expanded) {
    return (
      <button
        type="button"
        onClick={() => setExpanded(true)}
        className="mt-4 w-full flex items-center justify-center gap-2 px-4 py-3 bg-gray-800 hover:bg-gray-700 border border-gray-700 rounded-xl text-gray-300 hover:text-white transition-colors cursor-pointer"
      >
        <MessageCircle size={18} />
        <span className="text-sm font-medium">{t('chat.title')}</span>
      </button>
    )
  }

  return (
    <div className="mt-4 bg-gray-800 border border-gray-700 rounded-xl overflow-hidden">
      {/* Drawer header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-700">
        <div className="flex items-center gap-2 text-gray-300">
          <MessageCircle size={18} />
          <span className="text-sm font-medium">{t('chat.title')}</span>
        </div>
        <button
          type="button"
          onClick={() => setExpanded(false)}
          className="p-1 text-gray-400 hover:text-white cursor-pointer"
          aria-label={t('chat.collapse')}
        >
          <ChevronDown size={18} />
        </button>
      </div>

      {/* Messages area */}
      <div
        className="overflow-y-auto px-4 py-4 space-y-4 max-h-[50vh] md:max-h-[50vh]"
        style={{ maxHeight: 'min(50vh, 400px)' }}
        role="log"
        aria-live="polite"
      >
        {messages.length === 0 && !streamingText && !sending ? (
          <div className="flex flex-col items-center justify-center py-8 text-gray-500">
            <Bot size={32} className="mb-2 opacity-30" />
            <p className="text-sm text-center">{t('chat.empty')}</p>
          </div>
        ) : (
          <>
            {messages.map(msg => (
              <ChatMessage key={msg.id} message={msg} />
            ))}
            {sending && streamingText && (
              <div className="flex items-start gap-2">
                <div className="w-7 h-7 rounded-full bg-yellow-600/20 flex items-center justify-center shrink-0">
                  <Bot size={14} className="text-yellow-400" />
                </div>
                <div className="bg-gray-700 rounded-2xl rounded-tl-sm px-3 py-2 max-w-[85%] min-w-0">
                  <div className="prose prose-invert prose-sm max-w-none break-words">
                    <ReactMarkdown
                      remarkPlugins={[remarkGfm]}
                      components={{
                        a: ({ href, children, ...props }) => {
                          if (!isSafeLinkHref(href)) return <span>{children}</span>
                          return (
                            <a
                              {...props}
                              href={href}
                              target="_blank"
                              rel="noopener noreferrer"
                            >
                              {children}
                            </a>
                          )
                        },
                      }}
                    >
                      {streamingText}
                    </ReactMarkdown>
                  </div>
                  <span className="inline-block w-1.5 h-4 bg-yellow-400 animate-pulse ml-0.5" />
                </div>
              </div>
            )}
            {sending && !streamingText && (
              <div className="flex items-start gap-2">
                <div className="w-7 h-7 rounded-full bg-yellow-600/20 flex items-center justify-center shrink-0">
                  <Bot size={14} className="text-yellow-400" />
                </div>
                <div className="bg-gray-700 rounded-2xl rounded-tl-sm px-3 py-2">
                  <div className="flex items-center gap-2 text-gray-400">
                    <Loader2 size={14} className="animate-spin" />
                    <span className="text-sm">{t('chat.sending')}</span>
                  </div>
                </div>
              </div>
            )}
          </>
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Retry banner */}
      {retrying && (
        <div className="px-4 py-2 bg-yellow-900/30 border-t border-yellow-800/50 text-yellow-300 text-xs flex items-center gap-2">
          <RefreshCw size={12} className="animate-spin" />
          <span>{t('chat.sessionRetry')}</span>
        </div>
      )}

      {/* Plan update warning banner */}
      {planUpdateWarning && (
        <div className="px-4 py-2 bg-yellow-900/30 border-t border-yellow-800/50 text-yellow-300 text-sm flex items-center justify-between">
          <span>{planUpdateWarning}</span>
          <button
            type="button"
            onClick={() => setPlanUpdateWarning('')}
            className="text-yellow-400 hover:text-yellow-300 cursor-pointer"
            aria-label={t('chat.dismissError')}
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Error banner */}
      {error && (
        <div className="px-4 py-2 bg-red-900/50 border-t border-red-800 text-red-300 text-sm flex items-center justify-between">
          <span>{error}</span>
          <button
            onClick={() => setError('')}
            className="text-red-400 hover:text-red-300 cursor-pointer"
            aria-label={t('chat.dismissError')}
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Input area */}
      <div className="border-t border-gray-700 p-3" style={{ paddingBottom: 'max(0.75rem, env(safe-area-inset-bottom))' }}>
        <div className="flex gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={t('chat.placeholder')}
            aria-label={t('chat.placeholder')}
            rows={1}
            className="flex-1 bg-gray-700 border border-gray-600 rounded-xl px-3 py-2 text-white text-sm resize-none focus:outline-none focus:ring-2 focus:ring-yellow-500 placeholder-gray-500 max-h-28 overflow-y-auto"
            style={{ minHeight: '40px' }}
            disabled={sending}
          />
          <button
            onClick={() => sendMessage()}
            disabled={!input.trim() || sending}
            className="self-end p-2.5 rounded-xl bg-yellow-600 hover:bg-yellow-500 text-white transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
            title={t('chat.send')}
            aria-label={t('chat.send')}
          >
            {sending ? <Loader2 size={16} className="animate-spin" /> : <Send size={16} />}
          </button>
        </div>
      </div>
    </div>
  )
}

function ChatMessage({ message }: { message: StrideChatMessage }) {
  const { t } = useTranslation('stride')
  const isUser = message.role === 'user'

  if (isUser) {
    return (
      <div className="flex items-start gap-2 justify-end">
        <div className="max-w-[85%]">
          <div className="bg-blue-600 rounded-2xl rounded-tr-sm px-3 py-2">
            <p className="text-sm text-white whitespace-pre-wrap break-words">{message.content}</p>
          </div>
        </div>
        <div className="w-7 h-7 rounded-full bg-blue-600/20 flex items-center justify-center shrink-0">
          <User size={14} className="text-blue-400" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex items-start gap-2">
      <div className="w-7 h-7 rounded-full bg-yellow-600/20 flex items-center justify-center shrink-0">
        <Bot size={14} className="text-yellow-400" />
      </div>
      <div className="bg-gray-700 rounded-2xl rounded-tl-sm px-3 py-2 max-w-[85%] min-w-0">
        <div className="prose prose-invert prose-sm max-w-none break-words [&_pre]:overflow-x-auto [&_pre]:max-w-full [&_code]:break-words">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              a({ href, children }: React.ComponentPropsWithoutRef<'a'>) {
                if (!isSafeLinkHref(href)) return <span>{children}</span>
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
                  <code className="bg-gray-600 px-1.5 py-0.5 rounded text-sm">
                    {children}
                  </code>
                )
              },
            }}
          >
            {message.content}
          </ReactMarkdown>
        </div>
        {message.plan_modified && (
          <div className="mt-1.5 inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-yellow-500/15 text-yellow-400 text-xs font-medium border border-yellow-500/30">
            <RefreshCw size={10} />
            {t('chat.planUpdated')}
          </div>
        )}
      </div>
    </div>
  )
}
