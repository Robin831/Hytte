import React, { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Target, Check, X } from 'lucide-react'

type Op = '*' | '/'
type Level = 'unseen' | 'red' | 'yellow' | 'green'

interface Last5Attempt {
  correct: boolean
  response_ms: number
}

interface HeatmapCell {
  a: number
  b: number
  op: Op
  count: number
  correct_count: number
  accuracy_pct: number
  avg_ms: number
  avg_ms_last5: number
  last5: Last5Attempt[]
  level: Level
}

interface HeatmapResponse {
  multiplication: HeatmapCell[][]
  division: HeatmapCell[][]
}

type Tab = 'multiplication' | 'division'

const TABS: Tab[] = ['multiplication', 'division']

// Tailwind classes per mastery level. Keeping them as static strings (rather
// than built dynamically) is what lets the production build safelist them.
const LEVEL_CLASSES: Record<Level, { cell: string; chip: string }> = {
  unseen: {
    cell: 'bg-gray-700/60 border-gray-600 text-gray-300 hover:bg-gray-700',
    chip: 'bg-gray-700 text-gray-300',
  },
  red: {
    cell: 'bg-red-600/70 border-red-500 text-white hover:bg-red-600',
    chip: 'bg-red-600 text-white',
  },
  yellow: {
    cell: 'bg-yellow-500/70 border-yellow-400 text-gray-900 hover:bg-yellow-500',
    chip: 'bg-yellow-500 text-gray-900',
  },
  green: {
    cell: 'bg-emerald-600/70 border-emerald-500 text-white hover:bg-emerald-600',
    chip: 'bg-emerald-600 text-white',
  },
}

function cellProblem(cell: HeatmapCell): string {
  if (cell.op === '*') return `${cell.a}×${cell.b}`
  return `${cell.a * cell.b}÷${cell.b}`
}

function cellAnswer(cell: HeatmapCell): number {
  if (cell.op === '*') return cell.a * cell.b
  return cell.a
}

function formatMs(ms: number): string {
  if (!isFinite(ms) || ms <= 0) return '—'
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

function levelKey(level: Level): 'heatmap.legend.unseen' | 'heatmap.legend.red' | 'heatmap.legend.yellow' | 'heatmap.legend.green' {
  return `heatmap.legend.${level}` as const
}

export default function MathHeatmap() {
  const { t } = useTranslation('regnemester')
  const uid = useId()
  const panelId = `${uid}-panel`

  const [tab, setTab] = useState<Tab>('multiplication')
  const [data, setData] = useState<HeatmapResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<{ row: number; col: number } | null>(null)

  const tabRefs = useRef<(HTMLButtonElement | null)[]>([])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError('')
    fetch('/api/math/stats', { credentials: 'include', signal: controller.signal })
      .then(res => {
        if (!res.ok) throw new Error('fetch failed')
        return res.json() as Promise<HeatmapResponse>
      })
      .then(json => {
        if (!controller.signal.aborted) {
          setData(json)
          setLoading(false)
        }
      })
      .catch(err => {
        if (controller.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) return
        setError(t('heatmap.errorLoad'))
        setLoading(false)
      })
    return () => { controller.abort() }
  }, [t])

  // Reset selection when switching tabs so the detail panel doesn't show
  // stale data from the previous operation's cell.
  useEffect(() => { setSelected(null) }, [tab])

  const grid = useMemo(() => {
    if (!data) return null
    return tab === 'multiplication' ? data.multiplication : data.division
  }, [data, tab])

  const selectedCell: HeatmapCell | null = useMemo(() => {
    if (!grid || !selected) return null
    const row = grid[selected.row]
    if (!row) return null
    return row[selected.col] ?? null
  }, [grid, selected])

  const handleTabKeyDown = (e: React.KeyboardEvent, currentTab: Tab) => {
    const idx = TABS.indexOf(currentTab)
    let next: number | null = null
    if (e.key === 'ArrowRight') next = (idx + 1) % TABS.length
    else if (e.key === 'ArrowLeft') next = (idx - 1 + TABS.length) % TABS.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = TABS.length - 1
    if (next === null) return
    e.preventDefault()
    setTab(TABS[next])
    tabRefs.current[next]?.focus()
  }

  const tabLabel = (key: Tab): string =>
    key === 'multiplication' ? t('heatmap.tabMultiplication') : t('heatmap.tabDivision')

  return (
    <div className="max-w-4xl mx-auto p-4 sm:p-6 space-y-5">
      <div className="flex items-center gap-3">
        <Link
          to="/math"
          aria-label={t('back')}
          className="text-gray-400 hover:text-white transition-colors"
        >
          <ArrowLeft size={20} />
        </Link>
        <Target size={24} className="text-blue-400 shrink-0" />
        <h1 className="text-2xl sm:text-3xl font-bold text-white">
          {t('heatmap.title')}
        </h1>
      </div>

      <p className="text-sm text-gray-400">{t('heatmap.intro')}</p>

      <div className="flex gap-1 bg-gray-800/60 rounded-lg border border-gray-700 p-1" role="tablist" aria-label={t('heatmap.tabsLabel')}>
        {TABS.map((key, i) => (
          <button
            key={key}
            ref={el => { tabRefs.current[i] = el }}
            type="button"
            role="tab"
            id={`${uid}-tab-${key}`}
            aria-selected={tab === key}
            aria-controls={panelId}
            tabIndex={tab === key ? 0 : -1}
            onClick={() => setTab(key)}
            onKeyDown={e => handleTabKeyDown(e, key)}
            className={`flex-1 py-2 px-3 rounded-md text-sm font-medium transition-colors ${
              tab === key
                ? 'bg-blue-500/20 text-blue-300 border border-blue-500/30'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {tabLabel(key)}
          </button>
        ))}
      </div>

      <Legend />

      <div
        id={panelId}
        role="tabpanel"
        aria-labelledby={`${uid}-tab-${tab}`}
        className="space-y-5"
      >
        {loading && (
          <div className="h-80 rounded-lg bg-gray-800 animate-pulse" aria-hidden="true" />
        )}

        {!loading && error && (
          <div className="rounded border border-red-500/50 bg-red-500/10 px-3 py-2 text-sm text-red-300">
            {error}
          </div>
        )}

        {!loading && !error && grid && (
          <>
            <HeatmapGrid
              grid={grid}
              selected={selected}
              onSelect={(row, col) => setSelected({ row, col })}
              op={tab === 'multiplication' ? '*' : '/'}
            />
            <CellDetail cell={selectedCell} />
          </>
        )}
      </div>
    </div>
  )
}

function Legend() {
  const { t } = useTranslation('regnemester')
  const items: Level[] = ['unseen', 'red', 'yellow', 'green']
  return (
    <div className="flex flex-wrap gap-2" aria-label={t('heatmap.legend.label')}>
      {items.map(level => (
        <span
          key={level}
          className={`inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs font-medium ${LEVEL_CLASSES[level].chip}`}
        >
          <span className="inline-block w-2.5 h-2.5 rounded-full bg-current opacity-80" aria-hidden="true" />
          {t(levelKey(level))}
        </span>
      ))}
    </div>
  )
}

interface GridProps {
  grid: HeatmapCell[][]
  selected: { row: number; col: number } | null
  onSelect: (row: number, col: number) => void
  op: Op
}

function HeatmapGrid({ grid, selected, onSelect, op }: GridProps) {
  const { t } = useTranslation('regnemester')
  const headerSymbol = op === '*' ? '×' : '÷'
  const rowLabel = op === '*' ? t('heatmap.axisA') : t('heatmap.axisQuotient')

  return (
    <div className="overflow-x-auto">
      <table
        className="border-separate border-spacing-1 mx-auto"
        aria-label={t('heatmap.gridLabel', { op: headerSymbol })}
      >
        <thead>
          <tr>
            <th scope="col" className="sr-only">{rowLabel}</th>
            <th
              scope="col"
              aria-hidden="true"
              className="w-8 sm:w-10 text-center text-xs text-gray-400 font-medium"
            >
              {headerSymbol}
            </th>
            {grid[0].map((_, col) => (
              <th
                key={col}
                scope="col"
                className="w-10 sm:w-12 md:w-14 text-center text-xs text-gray-400 font-medium tabular-nums"
              >
                {col + 1}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {grid.map((row, rowIdx) => (
            <tr key={rowIdx}>
              <th
                scope="row"
                className="w-8 sm:w-10 text-center text-xs text-gray-400 font-medium tabular-nums"
              >
                {rowIdx + 1}
              </th>
              <td aria-hidden="true" className="w-0" />
              {row.map((cell, colIdx) => {
                const isSelected = selected?.row === rowIdx && selected?.col === colIdx
                const levelClasses = LEVEL_CLASSES[cell.level]
                const problem = cellProblem(cell)
                const answer = cellAnswer(cell)
                const ariaLabel = t('heatmap.cellAria', {
                  problem: problem.replace('×', ' × ').replace('÷', ' ÷ '),
                  answer,
                  level: t(levelKey(cell.level)),
                  count: cell.count,
                })
                return (
                  <td key={colIdx} className="p-0">
                    <button
                      type="button"
                      onClick={() => onSelect(rowIdx, colIdx)}
                      aria-label={ariaLabel}
                      aria-pressed={isSelected}
                      className={`w-10 sm:w-12 md:w-14 h-10 sm:h-12 md:h-14 rounded border transition-colors flex items-center justify-center text-[11px] sm:text-xs md:text-sm font-semibold tabular-nums focus:outline-none focus:ring-2 focus:ring-blue-400 ${
                        levelClasses.cell
                      } ${isSelected ? 'ring-2 ring-blue-300' : ''}`}
                    >
                      {problem}
                    </button>
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function CellDetail({ cell }: { cell: HeatmapCell | null }) {
  const { t } = useTranslation('regnemester')
  if (!cell) {
    return (
      <div className="rounded-lg border border-gray-700 bg-gray-800/60 p-4 text-sm text-gray-400">
        {t('heatmap.detail.prompt')}
      </div>
    )
  }
  const problem = cell.op === '*'
    ? `${cell.a} × ${cell.b}`
    : `${cell.a * cell.b} ÷ ${cell.b}`
  const answer = cellAnswer(cell)
  const levelClasses = LEVEL_CLASSES[cell.level]

  return (
    <div className="rounded-lg border border-gray-700 bg-gray-800/60 p-4">
      <div className="flex flex-wrap items-center gap-3 mb-3">
        <h2 className="text-lg font-semibold text-white tabular-nums">
          {problem} = <span className="text-blue-300">{answer}</span>
        </h2>
        <span className={`inline-flex items-center gap-1.5 rounded px-2 py-0.5 text-xs font-medium ${levelClasses.chip}`}>
          {t(levelKey(cell.level))}
        </span>
      </div>

      {cell.count === 0 ? (
        <p className="text-sm text-gray-400">{t('heatmap.detail.noAttempts')}</p>
      ) : (
        <div className="space-y-3">
          <dl className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
            <Stat label={t('heatmap.detail.attempts')} value={String(cell.count)} />
            <Stat
              label={t('heatmap.detail.accuracy')}
              value={`${Math.round(cell.accuracy_pct)}%`}
              hint={t('heatmap.detail.accuracyHint')}
            />
            <Stat label={t('heatmap.detail.avgTime')} value={formatMs(cell.avg_ms)} />
            <Stat
              label={t('heatmap.detail.recentAvg')}
              value={formatMs(cell.avg_ms_last5)}
              hint={t('heatmap.detail.recentAvgHint')}
            />
          </dl>

          <div>
            <div className="text-xs uppercase tracking-wide text-gray-400 mb-1.5">
              {t('heatmap.detail.sparkline')}
            </div>
            <Sparkline attempts={cell.last5} />
          </div>
        </div>
      )}
    </div>
  )
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-gray-400" title={hint}>
        {label}
      </dt>
      <dd className="text-lg font-semibold text-white tabular-nums">{value}</dd>
    </div>
  )
}

function Sparkline({ attempts }: { attempts: Last5Attempt[] }) {
  const { t } = useTranslation('regnemester')
  const slots: (Last5Attempt | null)[] = []
  for (let i = 0; i < 5; i++) slots.push(attempts[i] ?? null)
  return (
    <ol className="flex items-center gap-1.5" aria-label={t('heatmap.detail.sparklineLabel')}>
      {slots.map((attempt, i) => {
        if (!attempt) {
          return (
            <li
              key={i}
              aria-label={t('heatmap.detail.noAttempt')}
              className="w-7 h-7 rounded border border-dashed border-gray-600 bg-transparent"
            />
          )
        }
        const Icon = attempt.correct ? Check : X
        const bg = attempt.correct ? 'bg-emerald-600/80' : 'bg-red-600/80'
        const label = attempt.correct
          ? t('heatmap.detail.correctAttempt', { ms: formatMs(attempt.response_ms) })
          : t('heatmap.detail.wrongAttempt', { ms: formatMs(attempt.response_ms) })
        return (
          <li
            key={i}
            aria-label={label}
            title={label}
            className={`w-7 h-7 rounded flex items-center justify-center text-white ${bg}`}
          >
            <Icon size={16} aria-hidden="true" />
          </li>
        )
      })}
    </ol>
  )
}
