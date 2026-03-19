import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { FlaskConical, TrendingUp, TrendingDown, Minus } from 'lucide-react'
import { useAuth } from '../../auth'
import Widget from '../Widget'

interface LactateTest {
  id: number
  date: string
  comment: string
}

interface ThresholdResult {
  method: string
  speed_kmh: number
  lactate_mmol: number
  heart_rate_bpm: number
  valid: boolean
}

function formatPace(speedKmh: number): string {
  if (speedKmh <= 0) return '-'
  const secPerKm = 3600 / speedKmh
  const min = Math.floor(secPerKm / 60)
  const sec = Math.round(secPerKm % 60)
  return `${min}:${sec.toString().padStart(2, '0')}/km`
}

export default function LactateSummaryWidget() {
  const { user } = useAuth()
  const [latestTest, setLatestTest] = useState<LactateTest | null>(null)
  const [latestThresholds, setLatestThresholds] = useState<ThresholdResult[]>([])
  const [prevThresholds, setPrevThresholds] = useState<ThresholdResult[]>([])
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    async function loadData() {
      try {
        const testsRes = await fetch('/api/lactate/tests', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!testsRes.ok) { setLoaded(true); return }
        const data = await testsRes.json()
        const tests: LactateTest[] = data?.tests ?? []
        if (tests.length === 0) { setLoaded(true); return }

        setLatestTest(tests[0])

        const thresholdFetches = [
          fetch(`/api/lactate/tests/${tests[0].id}/thresholds`, {
            credentials: 'include',
            signal: controller.signal,
          }).then(r => r.ok ? r.json() : null),
        ]
        if (tests.length > 1) {
          thresholdFetches.push(
            fetch(`/api/lactate/tests/${tests[1].id}/thresholds`, {
              credentials: 'include',
              signal: controller.signal,
            }).then(r => r.ok ? r.json() : null),
          )
        }

        const results = await Promise.all(thresholdFetches)
        if (results[0]?.thresholds) setLatestThresholds(results[0].thresholds)
        if (results[1]?.thresholds) setPrevThresholds(results[1].thresholds)
        setLoaded(true)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('LactateSummaryWidget fetch error:', err)
        setLoaded(true)
      }
    }

    loadData()

    return () => { controller.abort() }
  }, [user])

  if (!user || !loaded) return null
  if (loaded && !latestTest) return null

  const obla = latestThresholds.find(t => t.method === 'OBLA' && t.valid)
  const prevObla = prevThresholds.find(t => t.method === 'OBLA' && t.valid)

  const speedDelta = obla && prevObla ? obla.speed_kmh - prevObla.speed_kmh : 0
  const TrendIcon = speedDelta > 0.1 ? TrendingUp : speedDelta < -0.1 ? TrendingDown : Minus
  const trendColor = speedDelta > 0.1 ? 'text-green-400' : speedDelta < -0.1 ? 'text-red-400' : 'text-gray-400'

  return (
    <Widget title="Lactate Fitness">
      <div className="flex items-start gap-3 mb-3">
        <FlaskConical size={20} className="text-purple-400 mt-0.5" />
        <div className="flex-1">
          <p className="text-xs text-gray-400 mb-1">
            Latest test: {new Date(latestTest!.date).toLocaleDateString(undefined, {
              year: 'numeric',
              month: 'short',
              day: 'numeric',
            })}
          </p>
          {latestTest!.comment && (
            <p className="text-xs text-gray-500 truncate">{latestTest!.comment}</p>
          )}
        </div>
      </div>

      {obla && (
        <div className="grid grid-cols-3 gap-3 mb-3">
          <div>
            <p className="text-xs text-gray-500">LT2 Speed</p>
            <p className="text-sm font-semibold tabular-nums">{obla.speed_kmh.toFixed(1)} km/h</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">LT2 Pace</p>
            <p className="text-sm font-semibold tabular-nums">{formatPace(obla.speed_kmh)}</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">LT2 HR</p>
            <p className="text-sm font-semibold tabular-nums">{obla.heart_rate_bpm} bpm</p>
          </div>
        </div>
      )}

      {prevObla && obla && (
        <div className={`flex items-center gap-1.5 text-xs ${trendColor} mb-3`}>
          <TrendIcon size={14} />
          <span>
            {speedDelta > 0 ? '+' : ''}{speedDelta.toFixed(1)} km/h vs previous test
          </span>
        </div>
      )}

      <Link
        to="/lactate"
        className="text-xs text-blue-400 hover:text-blue-300"
      >
        View all tests →
      </Link>
    </Widget>
  )
}
