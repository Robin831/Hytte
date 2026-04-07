import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeft,
  RefreshCw,
  AlertCircle,
  Hammer,
  GitPullRequest,
  ListOrdered,
  Activity,
} from 'lucide-react'
import { useAnvilHealth } from '../hooks/useAnvilHealth'
import { formatDateTime } from '../utils/formatDate'

export default function AnvilsPage() {
  const { t } = useTranslation('forge')
  const { anvils, loading, error, refresh } = useAnvilHealth()

  return (
    <div className="p-4 sm:p-6 max-w-6xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/forge/mezzanine"
          className="text-gray-400 hover:text-white"
          aria-label={t('anvilsPage.backToMezzanine')}
        >
          <ArrowLeft size={20} />
        </Link>
        <h1 className="text-xl font-bold text-white">{t('anvilsPage.title')}</h1>
        <button
          onClick={refresh}
          className="ml-auto text-gray-400 hover:text-white p-1.5 rounded hover:bg-gray-800"
          aria-label={t('anvilsPage.refresh')}
        >
          <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>

      {/* Error */}
      {error && (
        <div className="flex items-center gap-2 text-gray-400 mb-4">
          <AlertCircle size={16} className="text-amber-400 shrink-0" />
          {t('anvilsPage.error')}
        </div>
      )}

      {/* Loading */}
      {loading && anvils.length === 0 && (
        <p className="text-gray-500 text-sm">{t('anvilsPage.loading')}</p>
      )}

      {/* Empty state */}
      {!loading && anvils.length === 0 && !error && (
        <p className="text-gray-500 text-sm">{t('anvilsPage.empty')}</p>
      )}

      {/* Card grid */}
      {anvils.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {anvils.map(anvil => (
            <div
              key={anvil.anvil}
              className="bg-gray-800 rounded-lg border border-gray-700/50 p-4"
            >
              <div className="flex items-center gap-2 mb-3">
                <Hammer size={16} className="text-gray-400 shrink-0" />
                <h2 className="text-sm font-semibold text-white truncate">
                  {anvil.anvil}
                </h2>
              </div>

              <div className="grid grid-cols-3 gap-3 mb-3">
                <div>
                  <div className="flex items-center gap-1 mb-0.5">
                    <Activity size={12} className="text-blue-400" />
                    <span className="text-xs text-gray-500">
                      {t('anvilsPage.activeWorkers')}
                    </span>
                  </div>
                  <p className="text-lg font-semibold text-white tabular-nums">
                    {anvil.active_workers}
                  </p>
                </div>
                <div>
                  <div className="flex items-center gap-1 mb-0.5">
                    <GitPullRequest size={12} className="text-green-400" />
                    <span className="text-xs text-gray-500">
                      {t('anvilsPage.openPRs')}
                    </span>
                  </div>
                  <p className="text-lg font-semibold text-white tabular-nums">
                    {anvil.open_prs}
                  </p>
                </div>
                <div>
                  <div className="flex items-center gap-1 mb-0.5">
                    <ListOrdered size={12} className="text-yellow-400" />
                    <span className="text-xs text-gray-500">
                      {t('anvilsPage.queueDepth')}
                    </span>
                  </div>
                  <p className="text-lg font-semibold text-white tabular-nums">
                    {anvil.queue_depth}
                  </p>
                </div>
              </div>

              <div className="border-t border-gray-700/50 pt-2">
                <span className="text-xs text-gray-500">
                  {t('anvilsPage.lastActivity')}:{' '}
                  <span className="text-gray-400 tabular-nums">
                    {anvil.last_activity
                      ? formatDateTime(anvil.last_activity, {
                          month: 'short',
                          day: 'numeric',
                          hour: '2-digit',
                          minute: '2-digit',
                        })
                      : '\u2014'}
                  </span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
