import { useState, useEffect, useCallback, useRef } from 'react'
import {
  Loader2,
  BookOpen,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  MessageSquare,
  User,
  Bot,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatDate } from '../utils/formatDate'

interface FamilyChild {
  id: number
  parent_id: number
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface ConversationSummary {
  id: number
  kid_id: number
  subject: string
  created_at: string
  updated_at: string
  message_count: number
  help_levels: Record<string, number>
  repeated_answer_alert: boolean
}

interface ReviewData {
  conversations: ConversationSummary[]
  total_messages: number
  help_level_totals: Record<string, number>
  help_level_averages: Record<string, number>
  average_messages_per_conversation: number
}

interface Message {
  id: number
  conversation_id: number
  role: 'user' | 'assistant'
  content: string
  help_level?: string
  created_at: string
}

const HELP_LEVELS = ['hint', 'explain', 'walkthrough', 'answer'] as const

const LEVEL_COLORS: Record<string, string> = {
  hint: 'bg-green-600',
  explain: 'bg-blue-600',
  walkthrough: 'bg-yellow-600',
  answer: 'bg-red-600',
}

export default function HomeworkParentReview() {
  const { t } = useTranslation('homework')
  const [children, setChildren] = useState<FamilyChild[]>([])
  const [reviews, setReviews] = useState<Record<number, ReviewData>>({})
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})
  const [loading, setLoading] = useState(true)
  const [reviewLoading, setReviewLoading] = useState<Record<number, boolean>>({})
  const [error, setError] = useState('')
  const [transcript, setTranscript] = useState<{
    childId: number
    conversationId: number
    subject: string
    messages: Message[]
  } | null>(null)
  const [transcriptLoading, setTranscriptLoading] = useState(false)
  const reviewsRef = useRef(reviews)
  reviewsRef.current = reviews
  const reviewAbortRef = useRef<AbortController | null>(null)
  const transcriptAbortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/family/children', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('review.errors.failedToLoad'))
        const data = await res.json()
        setChildren(data.children ?? [])
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

  const loadReview = useCallback(async (childId: number) => {
    if (reviewsRef.current[childId]) return
    reviewAbortRef.current?.abort()
    const controller = new AbortController()
    reviewAbortRef.current = controller
    setReviewLoading(prev => ({ ...prev, [childId]: true }))
    try {
      const res = await fetch(`/api/homework/children/${childId}/review`, {
        credentials: 'include',
        signal: controller.signal,
      })
      if (!res.ok) throw new Error(t('review.errors.failedToLoadReview'))
      const data = await res.json()
      setReviews(prev => ({ ...prev, [childId]: data.review }))
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setError(err.message)
    } finally {
      if (!controller.signal.aborted) {
        setReviewLoading(prev => ({ ...prev, [childId]: false }))
      }
    }
  }, [t])

  function toggleChild(childId: number) {
    const isExpanding = !expanded[childId]
    setExpanded(prev => ({ ...prev, [childId]: isExpanding }))
    if (isExpanding) {
      loadReview(childId)
    }
  }

  async function openTranscript(childId: number, conv: ConversationSummary) {
    transcriptAbortRef.current?.abort()
    const controller = new AbortController()
    transcriptAbortRef.current = controller
    setTranscriptLoading(true)
    try {
      const res = await fetch(
        `/api/homework/children/${childId}/conversations/${conv.id}`,
        { credentials: 'include', signal: controller.signal },
      )
      if (!res.ok) throw new Error(t('review.errors.failedToLoadTranscript'))
      const data = await res.json()
      setTranscript({
        childId,
        conversationId: conv.id,
        subject: conv.subject,
        messages: data.messages ?? [],
      })
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') setError(err.message)
    } finally {
      if (!controller.signal.aborted) setTranscriptLoading(false)
    }
  }

  function helpLevelLabel(level: string): string {
    const key = `helpLevel.${level}` as const
    return t(key, level)
  }

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 size={32} className="animate-spin text-gray-400" />
      </div>
    )
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <h1 className="text-xl font-semibold mb-6">{t('review.title')}</h1>

      {error && (
        <div className="mb-4 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm flex items-center justify-between" role="alert">
          <span>{error}</span>
          <button onClick={() => setError('')} className="ml-2 cursor-pointer" aria-label={t('dismissError')}>
            <X size={16} />
          </button>
        </div>
      )}

      {children.length === 0 ? (
        <div className="text-center text-gray-500 py-12">
          <BookOpen size={48} className="mx-auto mb-4 opacity-30" />
          <p className="text-lg">{t('review.noChildren')}</p>
        </div>
      ) : (
        <div className="space-y-3">
          {children.map(child => {
            const isExpanded = expanded[child.child_id]
            const review = reviews[child.child_id]
            const isLoading = reviewLoading[child.child_id]
            const alertCount = review
              ? review.conversations.filter(c => c.repeated_answer_alert).length
              : 0

            return (
              <div key={child.id} className="bg-gray-800 rounded-lg overflow-hidden">
                <button
                  onClick={() => toggleChild(child.child_id)}
                  className="w-full flex items-center gap-3 px-4 py-3 hover:bg-gray-700 transition-colors text-left cursor-pointer"
                >
                  <span className="text-2xl" role="img" aria-label={child.nickname}>
                    {child.avatar_emoji}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="font-medium">{child.nickname}</p>
                    {review && (
                      <p className="text-xs text-gray-400">
                        {t('review.conversationCount', { count: review.conversations.length })}
                        {' \u00b7 '}
                        {t('review.messageCount', { count: review.total_messages })}
                      </p>
                    )}
                  </div>
                  {alertCount > 0 && (
                    <span className="flex items-center gap-1 px-2 py-0.5 bg-red-900/50 text-red-300 rounded text-xs">
                      <AlertTriangle size={14} />
                      {alertCount}
                    </span>
                  )}
                  {isExpanded ? <ChevronDown size={20} /> : <ChevronRight size={20} />}
                </button>

                {isExpanded && (
                  <div className="border-t border-gray-700 px-4 py-3">
                    {isLoading ? (
                      <div className="flex justify-center py-4">
                        <Loader2 size={20} className="animate-spin text-gray-400" />
                      </div>
                    ) : review ? (
                      <>
                        {/* Help level summary */}
                        {Object.keys(review.help_level_totals).length > 0 && (
                          <div className="mb-4">
                            <p className="text-xs text-gray-400 mb-2">{t('review.helpLevelBreakdown')}</p>
                            <div className="flex flex-wrap gap-2">
                              {HELP_LEVELS.map(level => {
                                const count = review.help_level_totals[level] ?? 0
                                if (count === 0) return null
                                return (
                                  <span
                                    key={level}
                                    className={`px-2 py-0.5 rounded text-xs text-white ${LEVEL_COLORS[level]}`}
                                  >
                                    {helpLevelLabel(level)}: {count}
                                  </span>
                                )
                              })}
                            </div>
                          </div>
                        )}

                        {/* Conversations list */}
                        {review.conversations.length === 0 ? (
                          <p className="text-sm text-gray-500">{t('review.noConversations')}</p>
                        ) : (
                          <div className="space-y-1">
                            {review.conversations.map(conv => (
                              <button
                                key={conv.id}
                                onClick={() => openTranscript(child.child_id, conv)}
                                className="w-full flex items-center gap-3 px-3 py-2 hover:bg-gray-700 rounded-lg transition-colors text-left cursor-pointer"
                              >
                                <MessageSquare size={16} className="shrink-0 text-gray-500" />
                                <div className="flex-1 min-w-0">
                                  <div className="flex items-center gap-2">
                                    <p className="text-sm truncate">
                                      {conv.subject || t('noSubject')}
                                    </p>
                                    {conv.repeated_answer_alert && (
                                      <span
                                        className="flex items-center gap-1 px-1.5 py-0.5 bg-red-900/50 text-red-300 rounded text-xs shrink-0"
                                        title={t('review.repeatedAnswerAlert')}
                                        aria-label={t('review.repeatedAnswerAlert')}
                                      >
                                        <AlertTriangle size={12} />
                                      </span>
                                    )}
                                  </div>
                                  <div className="flex items-center gap-2 text-xs text-gray-500 mt-0.5">
                                    <span>{formatDate(conv.created_at, { month: 'short', day: 'numeric' })}</span>
                                    <span>\u00b7</span>
                                    <span>{t('review.messages', { count: conv.message_count })}</span>
                                    {Object.entries(conv.help_levels).length > 0 && (
                                      <>
                                        <span>\u00b7</span>
                                        <span className="flex gap-1">
                                          {HELP_LEVELS.map(level => {
                                            const c = conv.help_levels[level]
                                            if (!c) return null
                                            return (
                                              <span
                                                key={level}
                                                className={`inline-block w-2 h-2 rounded-full ${LEVEL_COLORS[level]}`}
                                                title={`${helpLevelLabel(level)}: ${c}`}
                                                aria-label={`${helpLevelLabel(level)}: ${c}`}
                                              />
                                            )
                                          })}
                                        </span>
                                      </>
                                    )}
                                  </div>
                                </div>
                              </button>
                            ))}
                          </div>
                        )}
                      </>
                    ) : null}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {/* Transcript modal */}
      {(transcript || transcriptLoading) && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="transcript-title">
          <div className="bg-gray-800 rounded-lg w-full max-w-2xl max-h-[80vh] flex flex-col">
            <div className="flex items-center justify-between px-4 py-3 border-b border-gray-700">
              <h2 id="transcript-title" className="font-medium truncate">
                {transcript?.subject || t('noSubject')}
              </h2>
              <button
                onClick={() => setTranscript(null)}
                className="p-1 hover:bg-gray-700 rounded cursor-pointer"
                aria-label={t('review.closeTranscript')}
              >
                <X size={20} />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-4 space-y-3">
              {transcriptLoading ? (
                <div className="flex justify-center py-8">
                  <Loader2 size={24} className="animate-spin text-gray-400" />
                </div>
              ) : transcript?.messages.length === 0 ? (
                <p className="text-sm text-gray-500 text-center py-8">
                  {t('review.noMessages')}
                </p>
              ) : (
                transcript?.messages.map(msg => (
                  <div
                    key={msg.id}
                    className={`flex gap-3 ${msg.role === 'user' ? '' : 'bg-gray-900/50 -mx-2 px-2 py-2 rounded-lg'}`}
                  >
                    <div className="shrink-0 mt-0.5">
                      {msg.role === 'user' ? (
                        <User size={16} className="text-blue-400" />
                      ) : (
                        <Bot size={16} className="text-green-400" />
                      )}
                    </div>
                    <div className="flex-1 min-w-0">
                      {msg.help_level && (
                        <span className={`inline-block px-1.5 py-0.5 rounded text-xs text-white mb-1 ${LEVEL_COLORS[msg.help_level] ?? 'bg-gray-600'}`}>
                          {helpLevelLabel(msg.help_level)}
                        </span>
                      )}
                      <p className="text-sm whitespace-pre-wrap break-words">{msg.content}</p>
                      <p className="text-xs text-gray-600 mt-1">
                        {formatDate(msg.created_at, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                      </p>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
