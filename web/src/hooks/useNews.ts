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
  ranking_enabled: boolean
  scoring_pending: boolean
  score_threshold: number
  layout: string
  generated_at: string
}

// How often to silently re-poll for fresh articles while the page is open.
const REFRESH_MS = 120_000
// When the server reports background scoring is running, refetch this soon to
// pick up the freshly computed relevance scores.
const SCORING_REFETCH_MS = 14_000
// localStorage key + max age for the stale-while-revalidate cache.
const CACHE_KEY = 'news-feed-cache-v1'
const CACHE_MAX_AGE_MS = 30 * 60_000

// readCache returns the cached feed response if present and fresh enough.
function readCache(): ArticlesResponse | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return null
    const { ts, data } = JSON.parse(raw)
    if (Date.now() - ts < CACHE_MAX_AGE_MS && data?.articles) return data as ArticlesResponse
  } catch { /* corrupt cache — ignore */ }
  return null
}

export function useNews() {
  const { t } = useTranslation('news')

  // Hydrate initial state from the cache so re-entering the page is instant.
  // Lazy initializers run once on first render (the blessed pattern — no ref
  // access during render, no setState in an effect).
  const [articles, setArticles] = useState<NewsArticle[]>(() => readCache()?.articles ?? [])
  const [hidden, setHidden] = useState<HiddenArticle[]>(() => readCache()?.hidden ?? [])
  const [rankingEnabled, setRankingEnabled] = useState(() => readCache()?.ranking_enabled ?? false)
  const [threshold, setThreshold] = useState(() => readCache()?.score_threshold ?? 25)
  const [loading, setLoading] = useState(() => readCache() === null)
  const [error, setError] = useState('')
  const [newCount, setNewCount] = useState(0)

  // Latest poll result not yet shown (the "N new articles" pill promotes it).
  // knownIds is repopulated by the mount load(false) before any silent poll runs.
  const pending = useRef<ArticlesResponse | null>(null)
  const knownIds = useRef<Set<string>>(new Set())
  const scoringTimer = useRef<number | null>(null)
  const loadRef = useRef<(silent: boolean) => void>(() => {})

  const apply = useCallback((data: ArticlesResponse) => {
    setArticles(data.articles ?? [])
    setHidden(data.hidden ?? [])
    setRankingEnabled(data.ranking_enabled)
    setThreshold(data.score_threshold)
    knownIds.current = new Set((data.articles ?? []).map(a => a.id))
    pending.current = null
    setNewCount(0)
    try {
      localStorage.setItem(CACHE_KEY, JSON.stringify({ ts: Date.now(), data }))
    } catch { /* quota — ignore */ }
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
        } else {
          // No new articles, but scores may have changed — refresh in place.
          apply(data)
        }
      } else {
        apply(data)
      }
      setError('')
      // If the server is still scoring in the background, refetch soon so the
      // relevance scores appear without waiting for the full poll interval.
      if (data.scoring_pending) {
        if (scoringTimer.current) window.clearTimeout(scoringTimer.current)
        scoringTimer.current = window.setTimeout(() => loadRef.current(true), SCORING_REFETCH_MS)
      }
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : t('errors.load'))
    } finally {
      if (!silent) setLoading(false)
    }
  }, [apply, t])

  // Revalidate on mount, then poll. (Cache hydration happens in the initial
  // state above, so there's no synchronous setState here.)
  useEffect(() => {
    loadRef.current = load
    load(false)
    const id = window.setInterval(() => load(true), REFRESH_MS)
    return () => {
      window.clearInterval(id)
      if (scoringTimer.current) window.clearTimeout(scoringTimer.current)
    }
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
    articles, hidden, rankingEnabled, threshold, loading, error, newCount,
    reload: () => load(false), showNew, markRead, vote, toggleSave,
  }
}
