import { useState, useMemo, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Search, RefreshCw, ChevronLeft, ChevronRight, AlertCircle } from 'lucide-react'
import { useEventsPage } from '../hooks/useEventsPage'
import { formatDateTime } from '../utils/formatDate'

const PAGE_SIZE = 50

function classifyLevel(type: string, message: string, level?: string): 'success' | 'failure' | 'info' {
  const t = type?.toLowerCase() ?? ''
  const l = level?.toLowerCase() ?? ''
  const m = message?.toLowerCase() ?? ''

  if (l === 'error' || t.includes('fail') || t.includes('error') || m.includes('failed')) {
    return 'failure'
  }
  if (
    l === 'success' ||
    t.includes('pass') ||
    t.includes('merged') ||
    t.includes('done') ||
    t.includes('success') ||
    t.includes('complete')
  ) {
    return 'success'
  }
  return 'info'
}

const levelDotStyles: Record<string, string> = {
  success: 'bg-green-500',
  failure: 'bg-red-500',
  info: 'bg-blue-500',
}

export default function EventsPage() {
  const { t } = useTranslation('forge')

  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [page, setPage] = useState(1)

  const params = useMemo(
    () => ({
      search,
      type: typeFilter,
      from,
      to,
      page,
      pageSize: PAGE_SIZE,
    }),
    [search, typeFilter, from, to, page],
  )

  const { events, total, loading, error, refresh } = useEventsPage(params)

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
    setTypeFilter('')
    setFrom('')
    setTo('')
    setPage(1)
  }, [])

  const hasFilters = search || typeFilter || from || to

  return (
    <div className="p-4 sm:p-6 max-w-6xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/forge/mezzanine"
          className="text-gray-400 hover:text-white"
          aria-label={t('eventsPage.backToMezzanine')}
        >
          <ArrowLeft size={20} />
        </Link>
        <h1 className="text-xl font-bold text-white">{t('eventsPage.title')}</h1>
        <button
          onClick={refresh}
          className="ml-auto text-gray-400 hover:text-white p-1.5 rounded hover:bg-gray-800"
          aria-label={t('eventsPage.refresh')}
        >
          <RefreshCw size={16} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3 mb-4">
        {/* Search */}
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
            placeholder={t('eventsPage.searchPlaceholder')}
            className="w-full pl-8 pr-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-200 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            aria-label={t('eventsPage.searchLabel')}
          />
        </div>

        {/* Type filter */}
        <select
          value={typeFilter}
          onChange={e => {
            setTypeFilter(e.target.value)
            setPage(1)
          }}
          className="px-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-300 focus:outline-none focus:ring-1 focus:ring-blue-500"
          aria-label={t('eventsPage.typeFilterLabel')}
        >
          <option value="">{t('eventsPage.allTypes')}</option>
          <option value="worker_start">{t('eventsPage.typeWorkerStart')}</option>
          <option value="worker_done">{t('eventsPage.typeWorkerDone')}</option>
          <option value="worker_fail">{t('eventsPage.typeWorkerFail')}</option>
          <option value="pr_opened">{t('eventsPage.typePrOpened')}</option>
          <option value="pr_merged">{t('eventsPage.typePrMerged')}</option>
          <option value="dispatch">{t('eventsPage.typeDispatch')}</option>
        </select>

        {/* Date range */}
        <div className="flex items-center gap-2">
          <input
            type="date"
            value={from}
            onChange={e => {
              setFrom(e.target.value)
              setPage(1)
            }}
            className="px-3 py-2 text-sm rounded-lg bg-gray-800 border border-gray-700 text-gray-300 focus:outline-none focus:ring-1 focus:ring-blue-500"
            aria-label={t('eventsPage.fromDate')}
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
            aria-label={t('eventsPage.toDate')}
          />
        </div>

        {hasFilters && (
          <button
            onClick={handleClearFilters}
            className="text-xs text-gray-400 hover:text-white px-3 py-2 rounded-lg border border-gray-700 hover:bg-gray-800"
          >
            {t('eventsPage.clearFilters')}
          </button>
        )}
      </div>

      {/* Error */}
      {error && (
        <div className="flex items-center gap-2 text-gray-400 mb-4">
          <AlertCircle size={16} className="text-amber-400 shrink-0" />
          {t('eventsPage.error')}
        </div>
      )}

      {/* Results summary */}
      <div className="text-xs text-gray-500 mb-2">
        {t('eventsPage.showing', {
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
                <th
                  scope="col"
                  className="text-left text-xs text-gray-500 font-medium py-2.5 px-3"
                >
                  {t('eventsPage.colTimestamp')}
                </th>
                <th
                  scope="col"
                  className="text-left text-xs text-gray-500 font-medium py-2.5 px-3"
                >
                  {t('eventsPage.colType')}
                </th>
                <th
                  scope="col"
                  className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden sm:table-cell"
                >
                  {t('eventsPage.colSource')}
                </th>
                <th
                  scope="col"
                  className="text-left text-xs text-gray-500 font-medium py-2.5 px-3"
                >
                  {t('eventsPage.colSummary')}
                </th>
                <th
                  scope="col"
                  className="text-left text-xs text-gray-500 font-medium py-2.5 px-3 hidden md:table-cell"
                >
                  {t('eventsPage.colBead')}
                </th>
              </tr>
            </thead>
            <tbody>
              {loading && events.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-3 py-8 text-center text-gray-500">
                    {t('eventsPage.loading')}
                  </td>
                </tr>
              ) : events.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-3 py-8 text-center text-gray-500">
                    {t('eventsPage.empty')}
                  </td>
                </tr>
              ) : (
                events.map((event, i) => {
                  const level = classifyLevel(event.type, event.message, event.level)
                  return (
                    <tr
                      key={event.id}
                      className={`border-b border-gray-700/30 ${i % 2 === 0 ? '' : 'bg-gray-800/50'}`}
                    >
                      <td className="py-2 px-3 whitespace-nowrap">
                        <span className="text-xs text-gray-400 tabular-nums">
                          {formatDateTime(event.timestamp, {
                            month: 'short',
                            day: 'numeric',
                            hour: '2-digit',
                            minute: '2-digit',
                            second: '2-digit',
                          })}
                        </span>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap">
                        <span className="inline-flex items-center gap-1.5">
                          <span
                            className={`h-2 w-2 rounded-full shrink-0 ${levelDotStyles[level]}`}
                          />
                          <span className="text-xs font-medium text-gray-300">
                            {event.type}
                          </span>
                        </span>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap hidden sm:table-cell">
                        <span className="text-xs text-gray-500">
                          {event.anvil || '\u2014'}
                        </span>
                      </td>
                      <td className="py-2 px-3">
                        <p className="text-gray-300 text-sm truncate max-w-xs sm:max-w-md">
                          {event.message}
                        </p>
                      </td>
                      <td className="py-2 px-3 whitespace-nowrap hidden md:table-cell">
                        {event.bead_id ? (
                          <Link
                            to={`/forge/mezzanine?bead=${event.bead_id}`}
                            className="text-xs text-cyan-400 hover:text-cyan-300 hover:underline font-mono"
                          >
                            {event.bead_id}
                          </Link>
                        ) : (
                          <span className="text-xs text-gray-600">&mdash;</span>
                        )}
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
            {t('eventsPage.prev')}
          </button>
          <span className="text-sm text-gray-400">
            {t('eventsPage.pageOf', { page, total: totalPages })}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            className="flex items-center gap-1 px-3 py-1.5 text-sm rounded-lg border border-gray-700 text-gray-300 hover:bg-gray-800 disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {t('eventsPage.next')}
            <ChevronRight size={14} />
          </button>
        </div>
      )}
    </div>
  )
}
