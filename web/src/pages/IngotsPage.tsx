import { useState, useMemo, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeft,
  Search,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Loader2,
  Ban,
  TrendingUp,
  Clock,
  BarChart3,
} from 'lucide-react'
import { useIngots } from '../hooks/useIngots'
import { formatDateTime } from '../utils/formatDate'

const PAGE_SIZE = 50

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60)
    const s = Math.round(seconds % 60)
    return s > 0 ? `${m}m ${s}s` : `${m}m`
  }
  const h = Math.floor(seconds / 3600)
  const m = Math.round((seconds % 3600) / 60)
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

const statusConfig: Record<string, { icon: typeof CheckCircle2; color: string; dotColor: string }> = {
  done: { icon: CheckCircle2, color: 'text-green-400', dotColor: 'bg-green-500' },
  failed: { icon: XCircle, color: 'text-red-400', dotColor: 'bg-red-500' },
  running: { icon: Loader2, color: 'text-blue-400', dotColor: 'bg-blue-500' },
  pending: { icon: Clock, color: 'text-yellow-400', dotColor: 'bg-yellow-500' },
  cancelled: { icon: Ban, color: 'text-gray-400', dotColor: 'bg-gray-500' },
}

export default function IngotsPage() {
  const { t } = useTranslation('forge')

  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [page, setPage] = useState(1)

  const params = useMemo(
    () => ({
      search,
      status: statusFilter,
      from,
      to,
      page,
      pageSize: PAGE_SIZE,
    }),
    [search, statusFilter, from, to, page],
  )

  const { ingots, total, metrics, loading, error, refresh } = useIngots(params)

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const handleSearch = useCallback(() => {
    setSearch(searchInput)
    setPage(1)
  }, [searchInput])

  const handleSearchKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleSearch()
    },
    [handleSearch],
  )

  const handleClearFilters = useCallback(() => {
    setSearch('')
    setSearchInput('')
    setStatusFilter('')
    setFrom('')
    setTo('')
    setPage(1)
  }, [])

  const hasFilters = search || statusFilter || from || to

  return (
    <div className="p-4 sm:p-6 max-w-6xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/forge/mezzanine"
          className="text-gray-400 hover:text-white"
          aria-label={t('ingotsPage.backToMezzanine')}
        >
          <ArrowLeft size={20} />
        </Link>
        <h1 className="text-xl font-bold text-white">{t('ingotsPage.title')}</h1>
        <button
          onClick={refresh}
          className="ml-auto text-gray-400 hover:text-white p-1.5 rounded hover:bg-gray-800"
          aria-label={t('ingotsPage.refresh')}
        >
          <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>

      {/* Metrics cards */}
      {metrics && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
          <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-3">
            <div className="flex items-center gap-2 mb-1">
              <BarChart3 size={14} className="text-gray-500" />
              <span className="text-xs text-gray-500">{t('ingotsPage.metrics.totalBeads')}</span>
            </div>
            <p className="text-lg font-semibold text-white tabular-nums">{metrics.total_beads}</p>
          </div>
          <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-3">
            <div className="flex items-center gap-2 mb-1">
              <TrendingUp size={14} className="text-green-500" />
              <span className="text-xs text-gray-500">{t('ingotsPage.metrics.successRate')}</span>
            </div>
            <p className="text-lg font-semibold text-white tabular-nums">
              {(metrics.success_rate * 100).toFixed(1)}%
            </p>
            <p className="text-xs text-gray-500">
              {t('ingotsPage.metrics.successOf', {
                success: metrics.success_count,
                total: metrics.success_count + metrics.failure_count,
              })}
            </p>
          </div>
          <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-3">
            <div className="flex items-center gap-2 mb-1">
              <Clock size={14} className="text-blue-500" />
              <span className="text-xs text-gray-500">{t('ingotsPage.metrics.avgDuration')}</span>
            </div>
            <p className="text-lg font-semibold text-white tabular-nums">
              {metrics.avg_duration_seconds > 0
                ? formatDuration(metrics.avg_duration_seconds)
                : '\u2014'}
            </p>
          </div>
          <div className="bg-gray-800 rounded-lg border border-gray-700/50 p-3">
            <div className="flex items-center gap-2 mb-1">
              <Loader2 size={14} className="text-yellow-500" />
              <span className="text-xs text-gray-500">{t('ingotsPage.metrics.active')}</span>
            </div>
            <p className="text-lg font-semibold text-white tabular-nums">
              {metrics.running_count}
            </p>
          </div>
        </div>
      )}

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3 mb-4">
        <div className="relative flex-1">
          <Search
            size={14}
            className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500 pointer-events-none"
          />
          <input
            type="text"
            value={searchInput}
            onChange={e => setSearchInput(e.target.value)}
            onKeyDown={handleSearchKeyDown}
            onBlur={handleSearch}
            placeholder={t('ingotsPage.searchPlaceholder')}
            className="w-full pl-8 pr-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-200 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            aria-label={t('ingotsPage.searchLabel')}
          />
        </div>

        <select
          value={statusFilter}
          onChange={e => {
            setStatusFilter(e.target.value)
            setPage(1)
          }}
          className="px-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-300 focus:outline-none focus:ring-1 focus:ring-blue-500"
          aria-label={t('ingotsPage.statusFilterLabel')}
        >
          <option value="">{t('ingotsPage.allStatuses')}</option>
          <option value="done">{t('ingotsPage.statusDone')}</option>
          <option value="failed">{t('ingotsPage.statusFailed')}</option>
          <option value="running">{t('ingotsPage.statusRunning')}</option>
          <option value="pending">{t('ingotsPage.statusPending')}</option>
          <option value="cancelled">{t('ingotsPage.statusCancelled')}</option>
        </select>

        <div className="flex items-center gap-2">
          <input
            type="date"
            value={from}
            onChange={e => {
              setFrom(e.target.value)
              setPage(1)
            }}
            className="px-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-300 focus:outline-none focus:ring-1 focus:ring-blue-500"
            aria-label={t('ingotsPage.fromDate')}
          />
          <span className="text-gray-500 text-sm">&ndash;</span>
          <input
            type="date"
            value={to}
            onChange={e => {
              setTo(e.target.value)
              setPage(1)
            }}
            className="px-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-300 focus:outline-none focus:ring-1 focus:ring-blue-500"
            aria-label={t('ingotsPage.toDate')}
          />
        </div>

        {hasFilters && (
          <button
            onClick={handleClearFilters}
            className="text-xs text-gray-400 hover:text-white px-3 py-2 rounded-lg border border-gray-700 hover:bg-gray-800"
          >
            {t('ingotsPage.clearFilters')}
          </button>
        )}
      </div>

      {/* Error */}
      {error && (
        <div className="flex items-center gap-2 text-gray-400 mb-4">
          <AlertCircle size={16} className="text-amber-400 shrink-0" />
          {t('ingotsPage.error')}
        </div>
      )}

      {/* Results summary */}
      <div className="text-xs text-gray-500 mb-2">
        {t('ingotsPage.showing', {
          from: total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1,
          to: Math.min(page * PAGE_SIZE, total),
          total,
        })}
      </div>

      {/* Table */}
      <div className="bg-gray-800 rounded-lg border border-gray-700/50 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-700/50">
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3">
                  {t('ingotsPage.colBead')}
                </th>
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden sm:table-cell">
                  {t('ingotsPage.colTitle')}
                </th>
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3">
                  {t('ingotsPage.colStatus')}
                </th>
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden md:table-cell">
                  {t('ingotsPage.colPhase')}
                </th>
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden sm:table-cell">
                  {t('ingotsPage.colDuration')}
                </th>
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden md:table-cell">
                  {t('ingotsPage.colStarted')}
                </th>
                <th scope="col" className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden lg:table-cell">
                  {t('ingotsPage.colAnvil')}
                </th>
              </tr>
            </thead>
            <tbody>
              {loading && ingots.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-gray-500">
                    {t('ingotsPage.loading')}
                  </td>
                </tr>
              ) : ingots.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-gray-500">
                    {t('ingotsPage.empty')}
                  </td>
                </tr>
              ) : (
                ingots.map((ingot, i) => {
                  const cfg = statusConfig[ingot.status] ?? statusConfig.pending
                  const StatusIcon = cfg.icon
                  return (
                    <tr
                      key={ingot.worker_id}
                      className={`border-b border-gray-700/30 ${i % 2 === 0 ? '' : 'bg-gray-800/50'}`}
                    >
                      <td className="py-2 px-3 whitespace-nowrap">
                        <Link
                          to={`/forge/mezzanine?bead=${ingot.bead_id}`}
                          className="text-xs text-cyan-400 hover:text-cyan-300 hover:underline font-mono"
                        >
                          {ingot.bead_id}
                        </Link>
                      </td>
                      <td className="py-2 px-3 hidden sm:table-cell">
                        <p className="text-gray-300 text-sm truncate max-w-xs">
                          {ingot.title || '\u2014'}
                        </p>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap">
                        <span className="inline-flex items-center gap-1.5">
                          <StatusIcon
                            size={14}
                            className={`${cfg.color} shrink-0 ${ingot.status === 'running' ? 'animate-spin' : ''}`}
                          />
                          <span className="text-xs font-medium text-gray-300">
                            {ingot.status}
                          </span>
                        </span>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap hidden md:table-cell">
                        <span className="text-xs text-gray-400">{ingot.phase || '\u2014'}</span>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap hidden sm:table-cell">
                        <span className="text-xs text-gray-400 tabular-nums">
                          {ingot.duration_seconds != null
                            ? formatDuration(ingot.duration_seconds)
                            : ingot.status === 'running' || ingot.status === 'pending'
                              ? '\u2026'
                              : '\u2014'}
                        </span>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap hidden md:table-cell">
                        <span className="text-xs text-gray-400 tabular-nums">
                          {formatDateTime(ingot.started_at, {
                            month: 'short',
                            day: 'numeric',
                            hour: '2-digit',
                            minute: '2-digit',
                          })}
                        </span>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap hidden lg:table-cell">
                        <span className="text-xs text-gray-500">{ingot.anvil || '\u2014'}</span>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-4">
          <button
            onClick={() => setPage(p => Math.max(1, p - 1))}
            disabled={page <= 1}
            className="flex items-center gap-1 px-3 py-1.5 text-sm rounded-lg border border-gray-700 text-gray-300 hover:bg-gray-800 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <ChevronLeft size={14} />
            {t('ingotsPage.prev')}
          </button>
          <span className="text-sm text-gray-400">
            {t('ingotsPage.pageOf', { page, total: totalPages })}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            className="flex items-center gap-1 px-3 py-1.5 text-sm rounded-lg border border-gray-700 text-gray-300 hover:bg-gray-800 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {t('ingotsPage.next')}
            <ChevronRight size={14} />
          </button>
        </div>
      )}
    </div>
  )
}
