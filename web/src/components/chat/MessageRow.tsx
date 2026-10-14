import React, { memo, useEffect, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { vscDarkPlus } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { Bot, CheckCheck, Copy, Loader2, Pencil, RotateCcw, User } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { MessageRowProps } from './types'

// Shared styling for the per-message action buttons (Copy / Regenerate / Edit):
// always visible on touch layouts, revealed on hover or keyboard focus on
// desktop so the bubbles stay uncluttered.
const MESSAGE_ACTIONS_CLASS =
  'mt-2 flex items-center gap-3 opacity-100 sm:opacity-0 sm:group-hover:opacity-100 sm:focus-within:opacity-100 transition-opacity'

// Hoisted so ReactMarkdown receives the same references on every render.
const REMARK_PLUGINS = [remarkGfm]

const CODE_BLOCK_STYLE: React.CSSProperties = {
  margin: 0,
  borderRadius: '0.5rem',
  fontSize: '0.8125rem',
}

const MARKDOWN_COMPONENTS: Components = {
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
          customStyle={CODE_BLOCK_STYLE}
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
}

// One chat message. Exported unwrapped for tests; the default export is
// memoized so a streamed token only re-renders the bubble whose message
// object changed, not every earlier (markdown + highlighted) message.
export function MessageRow({
  message,
  pending = false,
  showRegenerate = false,
  showEdit = false,
  actionsDisabled = false,
  onRegenerate,
  onEdit,
}: MessageRowProps) {
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

  if (pending) {
    return (
      <div className="flex items-start gap-3">
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

  if (isUser) {
    return (
      <div className="flex items-start gap-3 justify-end group">
        <div className="bg-blue-600 rounded-2xl rounded-tr-sm px-4 py-3 max-w-[85%] min-w-0">
          <p className="text-sm text-white whitespace-pre-wrap break-words">{message.content}</p>
          {showEdit && (
            <div className={`${MESSAGE_ACTIONS_CLASS} justify-end`}>
              <button
                onClick={() => onEdit?.(message)}
                disabled={actionsDisabled}
                className="text-blue-200 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-blue-600 cursor-pointer"
                title={t('input.editMessage')}
                aria-label={t('input.editMessage')}
              >
                <Pencil size={14} />
              </button>
            </div>
          )}
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
          <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>
            {message.content}
          </ReactMarkdown>
        </div>
        <div className={MESSAGE_ACTIONS_CLASS}>
          <button
            onClick={copyContent}
            className="focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-800 text-gray-500 hover:text-gray-300 cursor-pointer"
            title={t('input.copyMessage')}
            aria-label={t('input.copyMessage')}
          >
            {copied ? <CheckCheck size={14} className="text-green-400" /> : <Copy size={14} />}
          </button>
          {showRegenerate && (
            <button
              onClick={() => onRegenerate?.(message)}
              disabled={actionsDisabled}
              className="focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-purple-500 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-800 text-gray-500 hover:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              title={t('input.regenerate')}
              aria-label={t('input.regenerate')}
            >
              <RotateCcw size={14} />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

export default memo(MessageRow)
