import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

export interface SourceRef {
  source: string
  source_name: string
  url: string
}

export interface NewsArticle {
  id: string
  source: string
  source_name: string
  source_color: string
  title: string
  url: string
  summary: string
  image_url: string
  published_at: string
  categories: string[]
  read: boolean
  saved: boolean
  feedback: number
  score: number
  score_reason: string
  also_in: SourceRef[] | null
}

export interface HiddenArticle extends NewsArticle {
  reason: string
}

export interface NewsSource {
  key: string
  name: string
  feed_url: string
  color: string
  enabled: boolean
}

export interface NewsSettings {
  sources: NewsSource[]
  block_keywords: string[]
  block_categories: string[]
  hide_paywalled: boolean
  llm_scoring: boolean
  score_threshold: number
  layout: string
}

interface ArticlesResponse {
  articles: NewsArticle[]
  hidden: HiddenArticle[]
  scored: boolean
  score_threshold: number
  layout: string
  generated_at: string
}

// How often to silently re-poll for fresh articles while the page is open.
const REFRESH_MS = 120_000

export function useNews() {
  const { t } = useTranslation('news')
  const [articles, setArticles] = useState<NewsArticle[]>([])
  const [hidden, setHidden] = useState<HiddenArticle[]>([])
  const [scored, setScored] = useState(false)
  const [threshold, setThreshold] = useState(25)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [newCount, setNewCount] = useState(0)

  // Latest poll result not yet shown (the "N new articles" pill promotes it).
  const pending = useRef<ArticlesResponse | null>(null)
  const knownIds = useRef<Set<string>>(new Set())

  const apply = useCallback((data: ArticlesResponse) => {
    setArticles(data.articles ?? [])
    setHidden(data.hidden ?? [])
    setScored(data.scored)
    setThreshold(data.score_threshold)
    knownIds.current = new Set((data.articles ?? []).map(a => a.id))
    pending.current = null
    setNewCount(0)
  }, [])

  const load = useCallback(async (silent: boolean) => {
    try {
      const res = await fetch('/api/news/articles', { credentials: 'include' })
      if (!res.ok) throw new Error(t('errors.load'))
      const data: ArticlesResponse = await res.json()
      if (silent && knownIds.current.size > 0) {
        const fresh = (data.articles ?? []).filter(a => !knownIds.current.has(a.id))
        if (fresh.length > 0) {
          pending.current = data
          setNewCount(fresh.length)
        }
      } else {
        apply(data)
      }
      setError('')
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : t('errors.load'))
    } finally {
      if (!silent) setLoading(false)
    }
  }, [apply, t])

  // Initial load + background polling.
  useEffect(() => {
    load(false)
    const id = window.setInterval(() => load(true), REFRESH_MS)
    return () => window.clearInterval(id)
  }, [load])

  const showNew = useCallback(() => {
    if (pending.current) apply(pending.current)
  }, [apply])

  const patch = useCallback((id: string, fields: Partial<NewsArticle>) => {
    setArticles(prev => prev.map(a => (a.id === id ? { ...a, ...fields } : a)))
  }, [])

  const markRead = useCallback((id: string) => {
    patch(id, { read: true })
    fetch('/api/news/read', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ article_id: id }),
    }).catch(() => { /* non-critical */ })
  }, [patch])

  const vote = useCallback((article: NewsArticle, signal: number) => {
    const next = article.feedback === signal ? 0 : signal
    patch(article.id, { feedback: next })
    fetch('/api/news/feedback', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        article_id: article.id,
        signal: next,
        title: article.title,
        summary: article.summary,
        source: article.source,
      }),
    }).catch(() => { /* non-critical */ })
  }, [patch])

  const toggleSave = useCallback((article: NewsArticle) => {
    const next = !article.saved
    patch(article.id, { saved: next })
    fetch('/api/news/saved', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ saved: next, article }),
    }).catch(() => { /* non-critical */ })
  }, [patch])

  return {
    articles, hidden, scored, threshold, loading, error, newCount,
    reload: () => load(false), showNew, markRead, vote, toggleSave,
  }
}
