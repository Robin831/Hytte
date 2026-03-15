import { useState, useEffect, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth'
import { ArrowLeft, TrendingUp } from 'lucide-react'
import type { LactateTest, Analysis } from '../types/lactate'
import ThresholdTrendsChart from '../components/charts/ThresholdTrendsChart'
import FixedSpeedChart from '../components/charts/FixedSpeedChart'
import ComparisonChart from '../components/charts/ComparisonChart'

interface TestWithAnalysis {
  test: LactateTest
  analysis: Analysis | null
}

type Tab = 'trends' | 'fixed-speed' | 'comparison'

export default function LactateInsights() {
  const { user } = useAuth()
  const [tests, setTests] = useState<LactateTest[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Tab>('trends')
  const [testsWithAnalysis, setTestsWithAnalysis] = useState<TestWithAnalysis[]>([])
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    const load = async () => {
      try {
        const res = await fetch('/api/lactate/tests', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error('Failed to load tests')
        const data = await res.json()
        setTests(data.tests || [])
      } catch (err: unknown) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError('Failed to load tests')
        }
      } finally {
        setLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [user])

  // Fetch analysis for all tests with sufficient stages (for threshold trends)
  const fetchAllAnalysis = useCallback(async (testList: LactateTest[]) => {
    const eligible = testList.filter((t) => t.stages.length >= 2)
    if (eligible.length === 0) return

    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setAnalysisLoading(true)
    try {
      const results: TestWithAnalysis[] = []
      for (const test of eligible) {
        try {
          const res = await fetch(`/api/lactate/tests/${test.id}/analysis`, {
            credentials: 'include',
            signal: controller.signal,
          })
          if (!res.ok) {
            results.push({ test, analysis: null })
            continue
          }
          const analysis = await res.json()
          results.push({ test, analysis })
        } catch (err) {
          if (err instanceof Error && err.name === 'AbortError') return
          results.push({ test, analysis: null })
        }
      }
      setTestsWithAnalysis(results)
    } finally {
      setAnalysisLoading(false)
    }
  }, [])

  useEffect(() => {
    if (tests.length > 0) {
      fetchAllAnalysis(tests)
    }
    return () => { abortRef.current?.abort() }
  }, [tests, fetchAllAnalysis])

  // Build trend data from analyses
  const trendData = testsWithAnalysis
    .filter((ta) => ta.analysis !== null)
    .map((ta) => {
      const a = ta.analysis!
      const primary = a.thresholds.find((t) => t.method === a.method_used && t.valid)
      if (!primary) return null
      const [y, m, d] = ta.test.date.split('-').map(Number)
      const label = new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
      return {
        date: ta.test.date,
        label,
        speed: primary.speed_kmh,
        lactate: primary.lactate_mmol,
        hr: primary.heart_rate_bpm,
      }
    })
    .filter((d): d is NonNullable<typeof d> => d !== null)
    .sort((a, b) => a.date.localeCompare(b.date))

  if (!user) {
    return (
      <div className="p-6">
        <p className="text-gray-400">Sign in to view lactate insights.</p>
      </div>
    )
  }

  const tabs: { key: Tab; label: string }[] = [
    { key: 'trends', label: 'Threshold Trends' },
    { key: 'fixed-speed', label: 'Fixed Speed Tracking' },
    { key: 'comparison', label: 'Test Comparison' },
  ]

  return (
    <div className="max-w-4xl mx-auto p-4 md:p-6">
      <div className="flex items-center gap-3 mb-6">
        <Link to="/lactate" className="text-gray-400 hover:text-white transition-colors" aria-label="Back to lactate tests">
          <ArrowLeft size={20} />
        </Link>
        <TrendingUp size={24} className="text-blue-400" />
        <h1 className="text-2xl font-bold">Lactate Insights</h1>
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 text-red-400">
          {error}
        </div>
      )}

      {loading ? (
        <div className="text-center py-12 text-gray-400">Loading...</div>
      ) : tests.length < 2 ? (
        <div className="bg-gray-800 rounded-xl p-8 text-center">
          <TrendingUp size={40} className="text-gray-600 mx-auto mb-3" />
          <p className="text-gray-400 mb-2">At least 2 tests are needed for insights.</p>
          <p className="text-gray-500 text-sm">
            You have {tests.length} test{tests.length !== 1 ? 's' : ''}. Record more tests to unlock trends and comparisons.
          </p>
        </div>
      ) : (
        <>
          {/* Tab navigation */}
          <div className="flex gap-2 mb-6 overflow-x-auto" role="tablist" aria-label="Lactate insights">
            {tabs.map((t) => (
              <button
                key={t.key}
                role="tab"
                aria-selected={tab === t.key}
                aria-controls={`tabpanel-${t.key}`}
                id={`tab-${t.key}`}
                onClick={() => setTab(t.key)}
                className={`px-4 py-2 text-sm rounded-lg whitespace-nowrap transition-colors cursor-pointer ${
                  tab === t.key
                    ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                    : 'bg-gray-800 text-gray-400 border border-gray-700 hover:text-white hover:border-gray-600'
                }`}
              >
                {t.label}
              </button>
            ))}
          </div>

          {/* Tab content */}
          <div className="bg-gray-800 rounded-xl p-6" role="tabpanel" id={`tabpanel-${tab}`} aria-labelledby={`tab-${tab}`}>
            {tab === 'trends' && (
              <>
                <h2 className="font-semibold mb-4">Threshold Trends Over Time</h2>
                {analysisLoading ? (
                  <div className="text-center py-8 text-gray-400">Analyzing tests...</div>
                ) : (
                  <ThresholdTrendsChart data={trendData} />
                )}
              </>
            )}

            {tab === 'fixed-speed' && (
              <>
                <h2 className="font-semibold mb-4">Lactate at Fixed Speed</h2>
                <p className="text-sm text-gray-500 mb-4">
                  Track how your lactate response at a specific speed changes over time.
                </p>
                <FixedSpeedChart tests={tests} />
              </>
            )}

            {tab === 'comparison' && (
              <>
                <h2 className="font-semibold mb-4">Test Comparison</h2>
                <p className="text-sm text-gray-500 mb-4">
                  Overlay lactate curves from multiple tests to compare results.
                </p>
                <ComparisonChart tests={tests} />
              </>
            )}
          </div>
        </>
      )}
    </div>
  )
}
