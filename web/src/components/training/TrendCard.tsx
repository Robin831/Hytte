import { TrendingUp, TrendingDown, ArrowRight, Minus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { TrendAnalysis } from '../../types/training'

interface TrendSparklineProps {
  direction: TrendAnalysis['fitness_direction']
}

function TrendSparkline({ direction }: TrendSparklineProps) {
  const width = 64
  const height = 28

  if (direction === 'improving') {
    return (
      <svg width={width} height={height} aria-hidden="true">
        <polyline
          points="4,22 16,16 28,12 40,8 60,4"
          fill="none"
          stroke="#22c55e"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle cx="60" cy="4" r="2.5" fill="#22c55e" />
      </svg>
    )
  }
  if (direction === 'declining') {
    return (
      <svg width={width} height={height} aria-hidden="true">
        <polyline
          points="4,4 16,8 28,12 40,18 60,24"
          fill="none"
          stroke="#ef4444"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle cx="60" cy="24" r="2.5" fill="#ef4444" />
      </svg>
    )
  }
  if (direction === 'stable') {
    return (
      <svg width={width} height={height} aria-hidden="true">
        <polyline
          points="4,14 16,12 28,14 40,13 60,14"
          fill="none"
          stroke="#3b82f6"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle cx="60" cy="14" r="2.5" fill="#3b82f6" />
      </svg>
    )
  }
  return (
    <svg width={width} height={height} aria-hidden="true">
      <line
        x1="4" y1="14" x2="60" y2="14"
        stroke="#6b7280"
        strokeWidth="2"
        strokeDasharray="4 3"
        strokeLinecap="round"
      />
    </svg>
  )
}

interface TrendCardProps {
  trendAnalysis: TrendAnalysis
}

export default function TrendCard({ trendAnalysis }: TrendCardProps) {
  const { t } = useTranslation('training')

  const { fitness_direction, comparison_to_recent, notable_changes } = trendAnalysis

  const directionBadge = (() => {
    switch (fitness_direction) {
      case 'improving':
        return (
          <span className="flex items-center gap-1 px-2.5 py-1 bg-green-500/15 border border-green-500/30 text-green-400 text-xs rounded-full font-medium">
            <TrendingUp size={12} /> {t('analysis.trendImproving')}
          </span>
        )
      case 'declining':
        return (
          <span className="flex items-center gap-1 px-2.5 py-1 bg-red-500/15 border border-red-500/30 text-red-400 text-xs rounded-full font-medium">
            <TrendingDown size={12} /> {t('analysis.trendDeclining')}
          </span>
        )
      case 'stable':
        return (
          <span className="flex items-center gap-1 px-2.5 py-1 bg-blue-500/15 border border-blue-500/30 text-blue-400 text-xs rounded-full font-medium">
            <ArrowRight size={12} /> {t('analysis.trendStable')}
          </span>
        )
      default:
        return (
          <span className="flex items-center gap-1 px-2.5 py-1 bg-gray-500/15 border border-gray-500/30 text-gray-400 text-xs rounded-full font-medium">
            <Minus size={12} /> {t('analysis.trendInsufficientData')}
          </span>
        )
    }
  })()

  return (
    <div className="bg-gray-800 rounded-xl p-5 mb-6">
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-sm font-semibold text-gray-400">{t('analysis.trendTitle')}</h2>
        <TrendSparkline direction={fitness_direction} />
      </div>
      <div className="flex items-center gap-2 mb-3">
        {directionBadge}
      </div>
      {comparison_to_recent && (
        <p className="text-sm text-gray-300 bg-gray-700/50 rounded-lg px-3 py-2 mb-3">
          {comparison_to_recent}
        </p>
      )}
      {notable_changes && notable_changes.length > 0 && (
        <div>
          <p className="text-xs text-gray-500 mb-1.5">{t('analysis.trendNotableChanges')}</p>
          <ul className="space-y-1">
            {notable_changes.map((change, i) => (
              <li key={i} className="flex items-start gap-2 text-sm text-gray-300">
                <span className="text-purple-400 mt-0.5 shrink-0">•</span>
                {change}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
