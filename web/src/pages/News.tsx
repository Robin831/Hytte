import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, SlidersHorizontal, Rows3, Columns3, ChevronDown, Sparkles, Clock } from 'lucide-react'
import { Skeleton } from '../components/ui/skeleton'
import NewsCard from '../components/news/NewsCard'
import NewsFilterDrawer from '../components/news/NewsFilterDrawer'
import { useNews, type NewsArticle } from '../hooks/useNews'

type Layout = 'timeline' | 'columns'
type Tab = 'feed' | 'saved'
type SortMode = 'relevance' | 'newest'

const LAYOUT_KEY = 'news-layout'
const SORT_KEY = 'news-sort'

const dateMs = (a: NewsArticle) => {
  const t = new Date(a.published_at).getTime()
  return isNaN(t) ? 0 : t
}

export default function News() {
  const { t } = useTranslation('news')
  const {
    articles, hidden, rankingEnabled, threshold, loading, error, newCount,
    reload, showNew, markRead, vote, toggleSave,
  } = useNews()

  const [layout, setLayout] = useState<Layout>(() =>
    (localStorage.getItem(LAYOUT_KEY) as Layout) || 'timeline')
  const [sort, setSort] = useState<SortMode>(() =>
    (localStorage.getItem(SORT_KEY) as SortMode) || 'relevance')
  const [tab, setTab] = useState<Tab>('feed')
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [showLow, setShowLow] = useState(false)

  useEffect(() => { localStorage.setItem(LAYOUT_KEY, layout) }, [layout])
  useEffect(() => { localStorage.setItem(SORT_KEY, sort) }, [sort])

  // Relevance sort only applies when ranking is on; otherwise always newest.
  const effectiveSort: SortMode = rankingEnabled ? sort : 'newest'

  const sorted = useMemo(() => {
    const arr = [...articles]
    if (effectiveSort === 'relevance') {
      arr.sort((a, b) => (b.score - a.score) || (dateMs(b) - dateMs(a)))
    } else {
      arr.sort((a, b) => dateMs(b) - dateMs(a))
    }
    return arr
  }, [articles, effectiveSort])

  // Split into main feed and a collapsed low-relevance tail (relevance sort only).
  const { main, low } = useMemo(() => {
    if (effectiveSort !== 'relevance') return { main: sorted, low: [] as NewsArticle[] }
    const main: NewsArticle[] = []
    const low: NewsArticle[] = []
    for (const a of sorted) {
      if (a.score >= 0 && a.score < threshold) low.push(a)
      else main.push(a)
    }
    return { main, low }
  }, [sorted, effectiveSort, threshold])

  const cardProps = {
    scored: rankingEnabled,
    onOpen: markRead,
    onVote: vote,
    onToggleSave: toggleSave,
  }

  const renderList = (list: NewsArticle[]) => {
    if (layout === 'columns') {
      const sources = Array.from(new Set(list.map(a => a.source)))
      return (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {sources.map(src => {
            const items = list.filter(a => a.source === src)
            return (
              <div key={src} className="space-y-3">
                <h2 className="sticky top-0 z-10 flex items-center gap-2 bg-gray-900/95 py-1 text-sm font-semibold text-gray-200 backdrop-blur">
                  <span className="inline-block h-3 w-3 rounded-full" style={{ backgroundColor: items[0]?.source_color }} />
                  {items[0]?.source_name}
                  <span className="text-gray-500 font-normal">{items.length}</span>
                </h2>
                {items.map(a => (
                  <NewsCard key={a.id} article={a} variant="columns" showSource={false} {...cardProps} />
                ))}
              </div>
            )
          })}
        </div>
      )
    }
    return (
      <div className="mx-auto max-w-3xl space-y-3">
        {list.map(a => (
          <NewsCard key={a.id} article={a} variant="timeline" {...cardProps} />
        ))}
      </div>
    )
  }

  return (
    <div className="px-4 py-4 sm:px-6">
      {/* Header */}
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="mr-auto">
          <h1 className="text-xl font-semibold text-white">{t('title')}</h1>
          <p className="flex items-center gap-1.5 text-xs text-gray-500">
            {rankingEnabled && effectiveSort === 'relevance'
              ? <><Sparkles size={12} /> {t('rankedNote')}</>
              : <><Clock size={12} /> {t('chronologicalNote')}</>}
          </p>
        </div>

        {/* tabs */}
        <div className="flex rounded-lg border border-gray-800 p-0.5 text-sm">
          {(['feed', 'saved'] as Tab[]).map(tk => (
            <button
              key={tk}
              onClick={() => setTab(tk)}
              className={`rounded-md px-3 py-1 cursor-pointer transition-colors ${
                tab === tk ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white'
              }`}
            >
              {t(`tabs.${tk}`)}
            </button>
          ))}
        </div>

        {/* sort toggle (feed only, when ranking is available) */}
        {tab === 'feed' && rankingEnabled && (
          <div className="flex rounded-lg border border-gray-800 p-0.5 text-sm">
            <button
              onClick={() => setSort('relevance')}
              title={t('sort.relevance')}
              className={`flex items-center gap-1 rounded-md px-2 py-1 cursor-pointer ${sort === 'relevance' ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white'}`}
            >
              <Sparkles size={15} /> <span className="hidden sm:inline">{t('sort.relevance')}</span>
            </button>
            <button
              onClick={() => setSort('newest')}
              title={t('sort.newest')}
              className={`flex items-center gap-1 rounded-md px-2 py-1 cursor-pointer ${sort === 'newest' ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white'}`}
            >
              <Clock size={15} /> <span className="hidden sm:inline">{t('sort.newest')}</span>
            </button>
          </div>
        )}

        {/* layout toggle (feed only) */}
        {tab === 'feed' && (
          <div className="flex rounded-lg border border-gray-800 p-0.5">
            <button
              onClick={() => setLayout('timeline')}
              title={t('layout.timeline')}
              className={`rounded-md p-1.5 cursor-pointer ${layout === 'timeline' ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white'}`}
            >
              <Rows3 size={18} />
            </button>
            <button
              onClick={() => setLayout('columns')}
              title={t('layout.columns')}
              className={`rounded-md p-1.5 cursor-pointer ${layout === 'columns' ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white'}`}
            >
              <Columns3 size={18} />
            </button>
          </div>
        )}

        <button
          onClick={reload}
          title={t('refresh')}
          className="rounded-lg border border-gray-800 p-1.5 text-gray-400 hover:text-white cursor-pointer"
        >
          <RefreshCw size={18} />
        </button>
        <button
          onClick={() => setDrawerOpen(true)}
          className="flex items-center gap-1.5 rounded-lg border border-gray-800 px-3 py-1.5 text-sm text-gray-300 hover:text-white cursor-pointer"
        >
          <SlidersHorizontal size={16} /> {t('filters.open')}
        </button>
      </div>

      {/* new-articles pill */}
      {tab === 'feed' && newCount > 0 && (
        <div className="sticky top-2 z-20 mb-3 flex justify-center">
          <button
            onClick={showNew}
            className="rounded-full bg-blue-600 px-4 py-1.5 text-sm font-medium text-white shadow-lg hover:bg-blue-500 cursor-pointer"
          >
            ↑ {t('newArticles', { count: newCount })}
          </button>
        </div>
      )}

      {tab === 'feed' ? (
        <FeedBody
          loading={loading}
          error={error}
          main={main}
          low={low}
          showLow={showLow}
          setShowLow={setShowLow}
          renderList={renderList}
          emptyText={t('empty')}
          loadingText={t('loading')}
          lowLabel={t('lowRelevance', { count: low.length })}
        />
      ) : (
        <SavedTab markRead={markRead} vote={vote} toggleSave={toggleSave} scored={false} />
      )}

      <NewsFilterDrawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        hidden={hidden}
        onSaved={() => { setDrawerOpen(false); reload() }}
      />
    </div>
  )
}

interface FeedBodyProps {
  loading: boolean
  error: string
  main: NewsArticle[]
  low: NewsArticle[]
  showLow: boolean
  setShowLow: (v: boolean) => void
  renderList: (list: NewsArticle[]) => React.ReactNode
  emptyText: string
  loadingText: string
  lowLabel: string
}

function FeedBody({ loading, error, main, low, showLow, setShowLow, renderList, emptyText, loadingText, lowLabel }: FeedBodyProps) {
  if (loading) {
    return (
      <div className="mx-auto max-w-3xl space-y-3" aria-label={loadingText}>
        {Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-28 w-full" />
        ))}
      </div>
    )
  }
  if (error) return <div className="rounded-lg border border-red-900 bg-red-950/40 p-4 text-sm text-red-300">{error}</div>
  if (main.length === 0 && low.length === 0) return <p className="py-12 text-center text-gray-500">{emptyText}</p>

  return (
    <>
      {renderList(main)}
      {low.length > 0 && (
        <div className="mx-auto mt-4 max-w-3xl">
          <button
            onClick={() => setShowLow(!showLow)}
            className="flex w-full items-center justify-center gap-2 rounded-lg border border-gray-800 py-2 text-sm text-gray-400 hover:text-white cursor-pointer"
          >
            <ChevronDown size={16} className={`transition-transform ${showLow ? 'rotate-180' : ''}`} />
            {lowLabel}
          </button>
          {showLow && <div className="mt-3">{renderList(low)}</div>}
        </div>
      )}
    </>
  )
}

interface SavedTabProps {
  markRead: (id: string) => void
  vote: (a: NewsArticle, s: number) => void
  toggleSave: (a: NewsArticle) => void
  scored: boolean
}

function SavedTab({ markRead, vote, toggleSave, scored }: SavedTabProps) {
  const { t } = useTranslation('news')
  const [items, setItems] = useState<NewsArticle[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const res = await fetch('/api/news/saved', { credentials: 'include', signal: controller.signal })
        const data: { articles: NewsArticle[] } = res.ok ? await res.json() : { articles: [] }
        setItems(data.articles ?? [])
      } catch {
        /* aborted or failed — leave list empty */
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [])

  const handleUnsave = (a: NewsArticle) => {
    toggleSave(a)
    setItems(prev => prev.filter(x => x.id !== a.id))
  }

  if (loading) {
    return (
      <div className="mx-auto max-w-3xl space-y-3">
        {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-28 w-full" />)}
      </div>
    )
  }
  if (items.length === 0) return <p className="py-12 text-center text-gray-500">{t('emptySaved')}</p>

  return (
    <div className="mx-auto max-w-3xl space-y-3">
      {items.map(a => (
        <NewsCard
          key={a.id}
          article={{ ...a, saved: true }}
          variant="timeline"
          scored={scored}
          onOpen={markRead}
          onVote={vote}
          onToggleSave={handleUnsave}
        />
      ))}
    </div>
  )
}
