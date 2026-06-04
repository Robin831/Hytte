import { useTranslation } from 'react-i18next'
import { ThumbsUp, ThumbsDown, Bookmark, BookmarkCheck } from 'lucide-react'
import { timeAgo } from '../../utils/timeAgo'
import type { NewsArticle } from '../../hooks/useNews'

interface NewsCardProps {
  article: NewsArticle
  scored: boolean
  variant: 'timeline' | 'columns'
  onOpen: (id: string) => void
  onVote: (article: NewsArticle, signal: number) => void
  onToggleSave: (article: NewsArticle) => void
  showSource?: boolean
}

function scoreColor(score: number): string {
  if (score >= 70) return 'text-emerald-400'
  if (score >= 40) return 'text-amber-400'
  return 'text-gray-500'
}

export default function NewsCard({
  article, scored, variant, onOpen, onVote, onToggleSave, showSource = true,
}: NewsCardProps) {
  const { t } = useTranslation('news')
  const { t: tc } = useTranslation('common')

  const horizontal = variant === 'timeline'
  const upSelected = article.feedback === 1
  const downSelected = article.feedback === -1

  return (
    <article
      className={`group rounded-xl border border-gray-800 bg-gray-900/60 overflow-hidden transition-colors hover:border-gray-700 ${
        article.read ? 'opacity-60' : ''
      } ${horizontal ? 'sm:flex' : ''}`}
    >
      {article.image_url && (
        <a
          href={article.url}
          target="_blank"
          rel="noopener noreferrer"
          onClick={() => onOpen(article.id)}
          className={`block shrink-0 bg-gray-800 ${
            horizontal ? 'sm:w-44 md:w-52' : 'w-full'
          }`}
        >
          <img
            src={article.image_url}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            className={`w-full object-cover ${horizontal ? 'h-40 sm:h-full' : 'h-40'}`}
          />
        </a>
      )}

      <div className="flex flex-1 flex-col p-3 min-w-0">
        {/* meta row */}
        <div className="flex items-center gap-2 text-xs mb-1.5 flex-wrap">
          {showSource && (
            <span
              className="inline-flex items-center rounded-full px-2 py-0.5 font-medium text-white"
              style={{ backgroundColor: article.source_color || '#374151' }}
            >
              {article.source_name}
            </span>
          )}
          {article.also_in?.map(ref => (
            <span
              key={ref.source}
              className="inline-flex items-center rounded-full px-2 py-0.5 text-gray-300 border border-gray-700"
              title={t('card.alsoIn')}
            >
              {ref.source_name}
            </span>
          ))}
          <span className="text-gray-500">{timeAgo(article.published_at, tc)}</span>
          {scored && article.score >= 0 && (
            <span
              className={`ml-auto font-semibold ${scoreColor(article.score)}`}
              title={article.score_reason}
            >
              {t('card.match', { score: article.score })}
            </span>
          )}
        </div>

        {/* title */}
        <h3 className="text-sm font-semibold leading-snug">
          <a
            href={article.url}
            target="_blank"
            rel="noopener noreferrer"
            onClick={() => onOpen(article.id)}
            className="text-gray-100 hover:text-blue-400 transition-colors"
          >
            {article.title}
          </a>
        </h3>

        {article.summary && (
          <p className="mt-1 text-sm text-gray-400 line-clamp-2">{article.summary}</p>
        )}

        {/* actions */}
        <div className="mt-2 flex items-center gap-1 pt-1">
          <button
            type="button"
            onClick={() => onVote(article, 1)}
            aria-pressed={upSelected}
            title={t('card.more')}
            className={`p-1.5 rounded-md transition-colors cursor-pointer ${
              upSelected
                ? 'bg-emerald-500/20 text-emerald-400 ring-1 ring-inset ring-emerald-400/70 hover:bg-emerald-500/30'
                : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
            }`}
          >
            <ThumbsUp size={16} fill={upSelected ? 'currentColor' : 'none'} />
          </button>
          <button
            type="button"
            onClick={() => onVote(article, -1)}
            aria-pressed={downSelected}
            title={t('card.less')}
            className={`p-1.5 rounded-md transition-colors cursor-pointer ${
              downSelected
                ? 'bg-red-500/20 text-red-400 ring-1 ring-inset ring-red-400/70 hover:bg-red-500/30'
                : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
            }`}
          >
            <ThumbsDown size={16} fill={downSelected ? 'currentColor' : 'none'} />
          </button>
          <button
            type="button"
            onClick={() => onToggleSave(article)}
            aria-pressed={article.saved}
            title={article.saved ? t('card.saved') : t('card.save')}
            className={`ml-auto p-1.5 rounded-md transition-colors cursor-pointer hover:bg-gray-800 ${
              article.saved ? 'text-blue-400' : 'text-gray-500 hover:text-gray-300'
            }`}
          >
            {article.saved ? <BookmarkCheck size={16} /> : <Bookmark size={16} />}
          </button>
        </div>
      </div>
    </article>
  )
}
