import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { GitMerge } from 'lucide-react'
import type { ReadyToMergePR } from '../hooks/useForgeStatus'

interface ReadyToMergeCardProps {
  prs: ReadyToMergePR[]
  onMerged?: (id: number) => void
}

export default function ReadyToMergeCard({ prs, onMerged }: ReadyToMergeCardProps) {
  const { t } = useTranslation('forge')
  const [merging, setMerging] = useState<Partial<Record<number, boolean>>>({})
  const [errors, setErrors] = useState<Partial<Record<number, string>>>({})

  async function handleMerge(pr: ReadyToMergePR) {
    setMerging(prev => ({ ...prev, [pr.id]: true }))
    setErrors(prev => { const next = { ...prev }; delete next[pr.id]; return next })
    try {
      const res = await fetch(`/api/forge/prs/${pr.id}/merge`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setErrors(prev => ({ ...prev, [pr.id]: (data as { error?: string }).error ?? `HTTP ${res.status}` }))
      } else {
        onMerged?.(pr.id)
      }
    } catch (err) {
      setErrors(prev => ({ ...prev, [pr.id]: err instanceof Error ? err.message : t('readyToMerge.mergeError') }))
    } finally {
      setMerging(prev => ({ ...prev, [pr.id]: false }))
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl border border-green-600/30 overflow-hidden">
      <div className="flex items-center gap-2 px-5 py-4 border-b border-gray-700/50">
        <GitMerge size={18} className={prs.length > 0 ? 'text-green-400 shrink-0' : 'text-gray-500 shrink-0'} />
        <h2 className="text-sm font-medium text-gray-300">{t('readyToMerge.title')}</h2>
        {prs.length > 0 && (
          <span className="ml-auto flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-green-500/20 text-green-400 text-xs font-medium">
            {prs.length}
          </span>
        )}
      </div>

      {prs.length === 0 ? (
        <p className="px-5 py-6 text-sm text-gray-500 text-center">{t('readyToMerge.empty')}</p>
      ) : (
        <div className="divide-y divide-gray-700/40">
          {prs.map(pr => (
            <div key={pr.id} className="px-5 py-4 flex flex-col gap-2 min-h-[44px]">
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-0.5 min-w-0">
                  <span className="text-sm text-white truncate">{pr.title}</span>
                  <span className="text-xs text-gray-500">#{pr.number}</span>
                </div>

                <button
                  type="button"
                  onClick={() => void handleMerge(pr)}
                  disabled={!!merging[pr.id]}
                  aria-label={t('readyToMerge.mergeLabel', { number: pr.number })}
                  className="flex items-center gap-1.5 min-h-[44px] min-w-[44px] px-3 rounded-lg text-sm font-medium transition-colors
                    bg-green-600/20 text-green-300 border border-green-600/30
                    hover:bg-green-600/30 disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
                >
                  <GitMerge size={14} />
                  {t('readyToMerge.merge')}
                </button>
              </div>

              {errors[pr.id] && (
                <p className="text-xs text-red-400">{errors[pr.id]}</p>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
