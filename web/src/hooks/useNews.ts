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
// When the server reports background scoring is running (or some articles are
// still unscored), refetch this soon to pick up freshly computed scores.
const SCORING_REFETCH_MS = 14_000
// Cap how many consecutive score-chasing refetches we do so we never poll
// forever if the ranker can't score a few stragglers. ~8 × 14s ≈ 2 min, enough
// to score a full 100-article feed in 40-article batches several times over.
const MAX_SCORE_CHASES = 8
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
  // Remembered scores by article ID, so a score never visibly disappears when a
  // revalidation arrives before background scoring has (re)computed it.
  const scoresRef = useRef<Map<string, { score: number; reason: string }>>(new Map())
  const chaseCount = useRef(0)

  // mergeScores records any fresh scores into memory and backfills still-unscored
  // articles from previously remembered scores.
  const mergeScores = useCallback((data: ArticlesResponse): ArticlesResponse => {
    const mem = scoresRef.current
    const articles = (data.articles ?? []).map(a => {
      if (a.score >= 0) {
        mem.set(a.id, { score: a.score, reason: a.score_reason })
        return a
      }
      const remembered = mem.get(a.id)
      return remembered ? { ...a, score: remembered.score, score_reason: remembered.reason } : a
    })
    return { ...data, articles }
  }, [])

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
    if (!silent) chaseCount.current = 0 // a manual/initial load restarts the chase budget
    try {
      const res = await fetch('/api/news/articles', { credentials: 'include' })
      if (!res.ok) throw new Error(t('errors.load'))
      const data = mergeScores(await res.json() as ArticlesResponse)
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
      // Keep refetching while the server is still scoring or some articles are
      // not yet scored (the feed is scored in batches), so relevance fills in
      // without waiting for the full poll interval — bounded by MAX_SCORE_CHASES.
      const unscored = (data.articles ?? []).filter(a => a.score < 0).length
      const keepChasing = data.ranking_enabled && (data.scoring_pending || unscored > 0)
      if (scoringTimer.current) window.clearTimeout(scoringTimer.current)
      if (keepChasing && chaseCount.current < MAX_SCORE_CHASES) {
        chaseCount.current += 1
        scoringTimer.current = window.setTimeout(() => loadRef.current(true), SCORING_REFETCH_MS)
      } else {
        chaseCount.current = 0
      }
    } catch (err) {
      if (!silent) setError(err instanceof Error ? err.message : t('errors.load'))
    } finally {
      if (!silent) setLoading(false)
    }
  }, [apply, mergeScores, t])

  // Revalidate on mount, then poll. (Cache hydration happens in the initial
  // state above, so there's no synchronous setState here.)
  useEffect(() => {
    // Seed score memory from the hydrated cache so the first revalidation can
    // backfill any articles the server hasn't re-scored yet.
    const cached = readCache()
    cached?.articles?.forEach(a => {
      if (a.score >= 0) scoresRef.current.set(a.id, { score: a.score, reason: a.score_reason })
    })

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
    // Apply the vote to this article's score immediately so it reorders without
    // waiting for the server: 👍 → top, 👎 → bottom, clear → back to model score.
    if (next > 0) {
      scoresRef.current.set(article.id, { score: 100, reason: 'you liked this' })
      patch(article.id, { feedback: next, score: 100, score_reason: 'you liked this' })
    } else if (next < 0) {
      scoresRef.current.set(article.id, { score: 0, reason: 'you hid this' })
      patch(article.id, { feedback: next, score: 0, score_reason: 'you hid this' })
    } else {
      scoresRef.current.delete(article.id)
      patch(article.id, { feedback: next, score: -1, score_reason: '' })
    }
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
