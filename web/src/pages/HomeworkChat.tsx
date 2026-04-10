import React, { useState, useEffect, useRef, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import {
  ChevronLeft,
  Send,
  Camera,
  Loader2,
  Bot,
  User,
  X,
  Copy,
  CheckCheck,
  BookOpen,
  Image,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

type HelpLevel = 'hint' | 'explain' | 'walkthrough' | 'answer'

interface Message {
  id: number
  conversation_id: number
  role: 'user' | 'assistant'
  content: string
  help_level?: HelpLevel
  image_path?: string
  created_at: string
}

interface Conversation {
  id: number
  kid_id: number
  subject: string
  created_at: string
  updated_at: string
}

const HELP_LEVELS: HelpLevel[] = ['hint', 'explain', 'walkthrough', 'answer']

export default function HomeworkChat() {
  const { t } = useTranslation('homework')
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [helpLevel, setHelpLevel] = useState<HelpLevel>('hint')
  const [selectedImage, setSelectedImage] = useState<File | null>(null)
  const [imagePreview, setImagePreview] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [sending, setSending] = useState(false)
  const [streamingText, setStreamingText] = useState('')
  const [error, setError] = useState('')

  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const sendAbortRef = useRef<AbortController | null>(null)

  // Abort any in-flight send on unmount
  useEffect(() => {
    return () => { sendAbortRef.current?.abort() }
  }, [])

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  useEffect(() => {
    scrollToBottom()
  }, [messages, streamingText, scrollToBottom])

  // Load conversation and messages
  useEffect(() => {
    if (!id) return
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch(`/api/homework/conversations/${id}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoadMessages'))
        const data = await res.json()
        setConversation(data.conversation)
        setMessages(data.messages ?? [])
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [id, t])

  // Auto-resize textarea
  useEffect(() => {
    const el = inputRef.current
    if (el) {
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 160) + 'px'
    }
  }, [input])

  function handleImageSelect(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setSelectedImage(file)
    const reader = new FileReader()
    reader.onload = () => setImagePreview(reader.result as string)
    reader.readAsDataURL(file)
    // Reset file input so the same file can be re-selected
    e.target.value = ''
  }

  function clearImage() {
    setSelectedImage(null)
    setImagePreview(null)
  }

  async function sendMessage() {
    if ((!input.trim() && !selectedImage) || !id || sending) return
    const content = input.trim()
    const image = selectedImage

    setInput('')
    clearImage()
    setSending(true)
    setStreamingText('')
    setError('')

    // Optimistic user message
    const tempUserMsg: Message = {
      id: -Date.now(),
      conversation_id: Number(id),
      role: 'user',
      content,
      help_level: helpLevel,
      created_at: new Date().toISOString(),
    }
    setMessages(prev => [...prev, tempUserMsg])

    const controller = new AbortController()
    sendAbortRef.current = controller

    try {
      const formData = new FormData()
      formData.append('message', content)
      formData.append('help_level', helpLevel)
      if (image) {
        formData.append('image', image)
      }

      const res = await fetch(`/api/homework/conversations/${id}/messages`, {
        method: 'POST',
        credentials: 'include',
        body: formData,
        signal: controller.signal,
      })

      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('errors.failedToSend'))
      }

      // Read SSE stream
      const reader = res.body?.getReader()
      if (!reader) throw new Error(t('errors.failedToSend'))

      const decoder = new TextDecoder()
      let buffer = ''
      let accumulatedText = ''
      let realUserMsg: Message | null = null

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })

        // Parse SSE events from buffer
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? '' // Keep incomplete line in buffer

        let eventType = ''
        for (const line of lines) {
          if (line.startsWith('event: ')) {
            eventType = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            const data = line.slice(6)
            try {
              const parsed = JSON.parse(data)

              switch (eventType) {
                case 'user_message':
                  realUserMsg = parsed as Message
                  // Replace optimistic message with real one
                  setMessages(prev =>
                    prev.map(m => m.id === tempUserMsg.id ? realUserMsg! : m)
                  )
                  break
                case 'delta':
                  accumulatedText += parsed.text ?? ''
                  setStreamingText(accumulatedText)
                  break
                case 'done': {
                  const assistantMsg = parsed as Message
                  setMessages(prev => [...prev, assistantMsg])
                  setStreamingText('')
                  // Update conversation subject if it changed
                  if (parsed.conversation_id) {
                    setConversation(prev =>
                      prev ? { ...prev, updated_at: new Date().toISOString() } : prev
                    )
                  }
                  break
                }
                case 'error':
                  throw new Error(parsed.error || t('errors.failedToSend'))
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
      // Remove optimistic message and restore draft
      setMessages(prev => prev.filter(m => m.id !== tempUserMsg.id))
      setStreamingText('')
      setInput(content)
      if (image) {
        setSelectedImage(image)
        const reader = new FileReader()
        reader.onload = () => setImagePreview(reader.result as string)
        reader.readAsDataURL(image)
      }
      if (err instanceof Error) setError(err.message)
    } finally {
      sendAbortRef.current = null
      setSending(false)
      inputRef.current?.focus()
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 size={32} className="animate-spin text-gray-400" />
      </div>
    )
  }

  return (
    <div className="flex flex-col h-[calc(100vh-3.5rem)] md:h-screen">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 h-14 border-b border-gray-700 shrink-0 bg-gray-900">
        <button
          onClick={() => navigate('/homework')}
          className="p-1 text-gray-400 hover:text-white cursor-pointer"
          aria-label={t('backToList')}
        >
          <ChevronLeft size={20} />
        </button>
        <BookOpen size={20} className="text-blue-400 shrink-0" />
        <div className="min-w-0">
          <h2 className="text-sm font-medium truncate">
            {conversation?.subject || t('noSubject')}
          </h2>
        </div>
      </div>

      {/* Messages area */}
      <div className="flex-1 overflow-y-auto" role="log" aria-live="polite">
        {messages.length === 0 && !streamingText ? (
          <div className="flex flex-col items-center justify-center h-full text-gray-500 px-4">
            <Bot size={48} className="mb-4 opacity-30" />
            <p className="text-lg font-medium">{t('welcome.title')}</p>
            <p className="text-sm mt-1 text-center">{t('welcome.subtitle')}</p>
          </div>
        ) : (
          <div className="max-w-3xl mx-auto px-4 py-6 space-y-6">
            {messages.map(msg => (
              <MessageBubble key={msg.id} message={msg} />
            ))}
            {sending && streamingText && (
              <div className="flex items-start gap-3">
                <div className="w-8 h-8 rounded-full bg-purple-600/20 flex items-center justify-center shrink-0">
                  <Bot size={16} className="text-purple-400" />
                </div>
                <div className="bg-gray-800 rounded-2xl rounded-tl-sm px-4 py-3 max-w-[85%] min-w-0">
                  <div className="prose prose-invert prose-sm max-w-none break-words">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>
                      {streamingText}
                    </ReactMarkdown>
                  </div>
                </div>
              </div>
            )}
            {sending && !streamingText && (
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
            aria-label={t('dismissError')}
          >
            <X size={14} />
          </button>
        </div>
      )}

      {/* Help level selector */}
      <div className="border-t border-gray-700 bg-gray-900 px-4 pt-3">
        <div className="max-w-3xl mx-auto">
          <div role="radiogroup" aria-label={t('helpLevel.label')}>
            <p className="text-xs text-gray-500 mb-2">{t('helpLevel.label')}</p>
            <div className="flex gap-2 flex-wrap">
              {HELP_LEVELS.map(level => (
                <button
                  key={level}
                  role="radio"
                  aria-checked={helpLevel === level}
                  onClick={() => setHelpLevel(level)}
                  disabled={sending}
                  className={`px-3 py-1.5 rounded-full text-xs font-medium transition-colors cursor-pointer disabled:cursor-not-allowed ${
                    helpLevel === level
                      ? 'bg-blue-600 text-white'
                      : 'bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-gray-300'
                  }`}
                >
                  {t(`helpLevel.${level}`)}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Image preview */}
      {imagePreview && (
        <div className="px-4 pt-2 bg-gray-900">
          <div className="max-w-3xl mx-auto">
            <div className="relative inline-block">
              <img
                src={imagePreview}
                alt={t('imagePreview')}
                className="h-20 rounded-lg object-cover"
              />
              <button
                onClick={clearImage}
                className="absolute -top-2 -right-2 w-5 h-5 bg-gray-700 rounded-full flex items-center justify-center text-gray-300 hover:text-white cursor-pointer"
                aria-label={t('removeImage')}
              >
                <X size={12} />
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Input area */}
      <div className="border-t border-gray-700 bg-gray-900 p-4 pt-3">
        <div className="max-w-3xl mx-auto flex gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            capture="environment"
            onChange={handleImageSelect}
            className="hidden"
            aria-label={t('cameraLabel')}
          />
          <button
            onClick={() => fileInputRef.current?.click()}
            disabled={sending}
            className="self-end p-3 rounded-xl bg-gray-800 hover:bg-gray-700 text-gray-400 hover:text-white transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
            title={t('cameraButton')}
            aria-label={t('cameraButton')}
          >
            {selectedImage ? <Image size={18} className="text-blue-400" /> : <Camera size={18} />}
          </button>
          <textarea
            ref={inputRef}
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={t('input.placeholder')}
            aria-label={t('input.placeholder')}
            rows={1}
            className="flex-1 bg-gray-800 border border-gray-600 rounded-xl px-4 py-3 text-white text-sm resize-none focus:outline-none focus:ring-2 focus:ring-blue-500 placeholder-gray-500 max-h-40 overflow-y-auto"
            style={{ minHeight: '48px' }}
            disabled={sending}
            onInput={e => {
              const el = e.currentTarget
              el.style.height = 'auto'
              el.style.height = Math.min(el.scrollHeight, 160) + 'px'
            }}
          />
          <button
            onClick={sendMessage}
            disabled={(!input.trim() && !selectedImage) || sending}
            className="self-end p-3 rounded-xl bg-blue-600 hover:bg-blue-500 text-white transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
            title={t('input.sendLabel')}
            aria-label={t('input.sendLabel')}
          >
            {sending ? <Loader2 size={18} className="animate-spin" /> : <Send size={18} />}
          </button>
        </div>
      </div>
    </div>
  )
}

function MessageBubble({ message }: { message: Message }) {
  const { t } = useTranslation('homework')
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
        <div className="max-w-[85%]">
          {message.help_level && (
            <p className="text-xs text-gray-500 text-right mb-1">
              {t(`helpLevel.${message.help_level}`)}
            </p>
          )}
          <div className="bg-blue-600 rounded-2xl rounded-tr-sm px-4 py-3">
            <p className="text-sm text-white whitespace-pre-wrap break-words">{message.content}</p>
          </div>
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
                const safeHref = href && /^https?:\/\//i.test(href) ? href : undefined
                if (!safeHref) return <span>{children}</span>
                return (
                  <a
                    href={safeHref}
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
