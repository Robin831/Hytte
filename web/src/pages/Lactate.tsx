import { useState, useEffect, useCallback } from 'react'
import { useAuth } from '../auth'
import { Activity, ChevronDown, ChevronUp, Timer, Gauge, CircleDot } from 'lucide-react'

interface LactateTest {
  id: number
  date: string
  comment: string
  protocol_type: string
  stages: Stage[]
}

interface Stage {
  stage_number: number
  speed_kmh: number
  lactate_mmol: number
  heart_rate_bpm: number
  rpe: number | null
  notes: string
}

interface ThresholdResult {
  method: string
  speed_kmh: number
  lactate_mmol: number
  heart_rate_bpm: number
  valid: boolean
  reason?: string
}

interface TrainingZone {
  zone: number
  name: string
  description: string
  min_speed_kmh: number
  max_speed_kmh: number
  min_hr: number
  max_hr: number
  lactate_from: number
  lactate_to: number
}

interface ZonesResult {
  system: string
  threshold_speed_kmh: number
  threshold_hr: number
  zones: TrainingZone[]
}

interface RacePrediction {
  name: string
  distance_km: number
  time_seconds: number
  time_formatted: string
  pace_min_km: string
  speed_kmh: number
}

interface TrafficLight {
  stage_number: number
  speed_kmh: number
  lactate_mmol: number
  light: 'green' | 'yellow' | 'red'
  label: string
}

interface Analysis {
  thresholds: ThresholdResult[]
  zones: ZonesResult[]
  predictions: RacePrediction[]
  traffic_lights: TrafficLight[]
  method_used: string
}

const trafficColors = {
  green: { bg: 'bg-green-500/20', border: 'border-green-500/40', text: 'text-green-400', dot: 'bg-green-500' },
  yellow: { bg: 'bg-yellow-500/20', border: 'border-yellow-500/40', text: 'text-yellow-400', dot: 'bg-yellow-500' },
  red: { bg: 'bg-red-500/20', border: 'border-red-500/40', text: 'text-red-400', dot: 'bg-red-500' },
}

const zoneColors = [
  'bg-green-500/20 text-green-400 border-green-500/30',
  'bg-blue-500/20 text-blue-400 border-blue-500/30',
  'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
  'bg-orange-500/20 text-orange-400 border-orange-500/30',
  'bg-red-500/20 text-red-400 border-red-500/30',
]

export default function Lactate() {
  const { user } = useAuth()
  const [tests, setTests] = useState<LactateTest[]>([])
  const [selectedTestId, setSelectedTestId] = useState<number | null>(null)
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [loading, setLoading] = useState(true)
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [error, setError] = useState('')
  const [expandedSection, setExpandedSection] = useState<string | null>('thresholds')
  const [selectedMethod, setSelectedMethod] = useState<string>('')
  const [activeZoneSystem, setActiveZoneSystem] = useState(0)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    const load = async () => {
      try {
        const res = await fetch('/api/lactate/tests', { credentials: 'include', signal: controller.signal })
        if (!res.ok) throw new Error('Failed to load tests')
        const data = await res.json()
        setTests(data.tests || [])
      } catch (err: unknown) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError('Failed to load lactate tests')
        }
      } finally {
        setLoading(false)
      }
    }
    load()
    return () => controller.abort()
  }, [user])

  const fetchAnalysis = useCallback(async (testId: number, method?: string) => {
    setAnalysisLoading(true)
    setError('')
    try {
      const params = method ? `?method=${encodeURIComponent(method)}` : ''
      const res = await fetch(`/api/lactate/tests/${testId}/analysis${params}`, {
        credentials: 'include',
      })
      if (!res.ok) throw new Error('Failed to load analysis')
      const data: Analysis = await res.json()
      setAnalysis(data)
    } catch {
      setError('Failed to load analysis')
    } finally {
      setAnalysisLoading(false)
    }
  }, [])

  const handleTestSelect = (testId: number) => {
    setSelectedTestId(testId)
    setSelectedMethod('')
    setActiveZoneSystem(0)
    setExpandedSection('thresholds')
    fetchAnalysis(testId)
  }

  const handleMethodChange = (method: string) => {
    setSelectedMethod(method)
    if (selectedTestId !== null) {
      fetchAnalysis(selectedTestId, method)
    }
  }

  const toggleSection = (section: string) => {
    setExpandedSection(expandedSection === section ? null : section)
  }

  if (!user) {
    return (
      <div className="p-6">
        <p className="text-gray-400">Sign in to view lactate test analysis.</p>
      </div>
    )
  }

  return (
    <div className="max-w-5xl mx-auto p-4 md:p-6">
      <div className="flex items-center gap-3 mb-6">
        <Activity size={24} className="text-blue-400" />
        <h1 className="text-2xl font-bold">Lactate Analysis</h1>
      </div>

      {error && (
        <div className="bg-red-500/10 border border-red-500/30 rounded-lg p-4 mb-6 text-red-400">
          {error}
        </div>
      )}

      {/* Test selector */}
      <div className="bg-gray-800 rounded-xl p-4 mb-6">
        <label className="block text-sm text-gray-400 mb-2">Select a test</label>
        {loading ? (
          <p className="text-gray-500">Loading tests...</p>
        ) : tests.length === 0 ? (
          <p className="text-gray-500">No lactate tests found. Create one first.</p>
        ) : (
          <select
            value={selectedTestId ?? ''}
            onChange={(e) => e.target.value && handleTestSelect(Number(e.target.value))}
            className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            aria-label="Select lactate test"
          >
            <option value="">Choose a test...</option>
            {tests.map((t) => (
              <option key={t.id} value={t.id}>
                {new Date(t.date).toLocaleDateString(undefined, {
                  year: 'numeric', month: 'short', day: 'numeric',
                })}
                {t.comment ? ` - ${t.comment}` : ''}
              </option>
            ))}
          </select>
        )}
      </div>

      {analysisLoading && (
        <div className="text-center py-12 text-gray-400">Loading analysis...</div>
      )}

      {analysis && !analysisLoading && (
        <>
          {/* Method selector */}
          {analysis.thresholds.filter((t) => t.valid).length > 1 && (
            <div className="bg-gray-800 rounded-xl p-4 mb-6">
              <label className="block text-sm text-gray-400 mb-2">Threshold method</label>
              <select
                value={selectedMethod}
                onChange={(e) => handleMethodChange(e.target.value)}
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                aria-label="Select threshold method"
              >
                <option value="">Auto (first valid)</option>
                {analysis.thresholds
                  .filter((t) => t.valid)
                  .map((t) => (
                    <option key={t.method} value={t.method}>
                      {t.method} ({t.speed_kmh.toFixed(1)} km/h, {t.lactate_mmol.toFixed(1)} mmol/L)
                    </option>
                  ))}
              </select>
              {analysis.method_used && (
                <p className="text-xs text-gray-500 mt-1">
                  Using: {analysis.method_used}
                </p>
              )}
            </div>
          )}

          {/* Thresholds section */}
          <CollapsibleSection
            title="Threshold Results"
            icon={<Gauge size={20} />}
            isOpen={expandedSection === 'thresholds'}
            onToggle={() => toggleSection('thresholds')}
          >
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {analysis.thresholds.map((t) => (
                <div
                  key={t.method}
                  className={`rounded-lg border p-4 ${
                    t.valid
                      ? t.method === analysis.method_used
                        ? 'border-blue-500/50 bg-blue-500/10'
                        : 'border-gray-700 bg-gray-800/50'
                      : 'border-gray-700/50 bg-gray-800/30 opacity-60'
                  }`}
                >
                  <div className="flex items-center justify-between mb-2">
                    <span className="font-medium text-sm">{t.method}</span>
                    {t.valid ? (
                      <span className="text-xs bg-green-500/20 text-green-400 px-2 py-0.5 rounded-full">Valid</span>
                    ) : (
                      <span className="text-xs bg-gray-600/20 text-gray-500 px-2 py-0.5 rounded-full">N/A</span>
                    )}
                  </div>
                  {t.valid ? (
                    <div className="space-y-1 text-sm">
                      <p><span className="text-gray-400">Speed:</span> {t.speed_kmh.toFixed(2)} km/h</p>
                      <p><span className="text-gray-400">Lactate:</span> {t.lactate_mmol.toFixed(2)} mmol/L</p>
                      {t.heart_rate_bpm > 0 && (
                        <p><span className="text-gray-400">HR:</span> {t.heart_rate_bpm} bpm</p>
                      )}
                    </div>
                  ) : (
                    <p className="text-xs text-gray-500">{t.reason}</p>
                  )}
                </div>
              ))}
            </div>
          </CollapsibleSection>

          {/* Traffic Light section */}
          {analysis.traffic_lights.length > 0 && (
            <CollapsibleSection
              title="Stage Traffic Lights"
              icon={<CircleDot size={20} />}
              isOpen={expandedSection === 'traffic'}
              onToggle={() => toggleSection('traffic')}
            >
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-gray-400 border-b border-gray-700">
                      <th className="text-left py-2 pr-4">Stage</th>
                      <th className="text-left py-2 pr-4">Speed</th>
                      <th className="text-left py-2 pr-4">Lactate</th>
                      <th className="text-left py-2 pr-4">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {analysis.traffic_lights.map((tl) => {
                      const colors = trafficColors[tl.light]
                      return (
                        <tr key={tl.stage_number} className="border-b border-gray-800">
                          <td className="py-2 pr-4">{tl.stage_number}</td>
                          <td className="py-2 pr-4">{tl.speed_kmh.toFixed(1)} km/h</td>
                          <td className="py-2 pr-4">{tl.lactate_mmol.toFixed(1)} mmol/L</td>
                          <td className="py-2 pr-4">
                            <span className={`inline-flex items-center gap-2 px-2 py-1 rounded-md ${colors.bg} ${colors.border} border`}>
                              <span className={`w-2.5 h-2.5 rounded-full ${colors.dot}`} />
                              <span className={`text-xs font-medium ${colors.text}`}>{tl.label}</span>
                            </span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </CollapsibleSection>
          )}

          {/* Training Zones section */}
          {analysis.zones && analysis.zones.length > 0 && (
            <CollapsibleSection
              title="Training Zones"
              icon={<Activity size={20} />}
              isOpen={expandedSection === 'zones'}
              onToggle={() => toggleSection('zones')}
            >
              {/* Zone system tabs */}
              <div className="flex gap-2 mb-4">
                {analysis.zones.map((zr, idx) => (
                  <button
                    key={zr.system}
                    onClick={() => setActiveZoneSystem(idx)}
                    className={`px-3 py-1.5 text-sm rounded-lg transition-colors cursor-pointer ${
                      activeZoneSystem === idx
                        ? 'bg-blue-500/20 text-blue-400 border border-blue-500/40'
                        : 'bg-gray-700 text-gray-400 border border-gray-600 hover:text-white'
                    }`}
                  >
                    {zr.system === 'olympiatoppen' ? 'Olympiatoppen' : 'Norwegian'}
                  </button>
                ))}
              </div>

              {(() => {
                const zoneIdx = activeZoneSystem < analysis.zones.length ? activeZoneSystem : 0
                const zr = analysis.zones[zoneIdx]
                return (
                  <div className="space-y-2">
                    <p className="text-xs text-gray-500 mb-3">
                      Based on threshold: {zr.threshold_speed_kmh.toFixed(1)} km/h
                      {zr.threshold_hr > 0 && ` / ${zr.threshold_hr} bpm`}
                    </p>
                    {zr.zones.map((z) => (
                      <div
                        key={z.zone}
                        className={`rounded-lg border p-3 ${zoneColors[z.zone - 1] || 'border-gray-700 bg-gray-800'}`}
                      >
                        <div className="flex items-center justify-between mb-1">
                          <span className="font-medium text-sm">{z.name}</span>
                          <span className="text-xs opacity-75">{z.description}</span>
                        </div>
                        <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs opacity-80">
                          <span>Speed: {z.min_speed_kmh.toFixed(1)}-{z.max_speed_kmh.toFixed(1)} km/h</span>
                          {z.max_hr > 0 && (
                            <span>HR: {z.min_hr}-{z.max_hr} bpm</span>
                          )}
                          <span>Lactate: {z.lactate_from.toFixed(1)}-{z.lactate_to >= 20 ? '20+' : z.lactate_to.toFixed(1)} mmol/L</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )
              })()}
            </CollapsibleSection>
          )}

          {/* Race Predictions section */}
          {analysis.predictions && analysis.predictions.length > 0 && (
            <CollapsibleSection
              title="Race Predictions"
              icon={<Timer size={20} />}
              isOpen={expandedSection === 'predictions'}
              onToggle={() => toggleSection('predictions')}
            >
              <p className="text-xs text-gray-500 mb-3">
                Based on Riegel's formula using threshold speed as ~60 min race pace
              </p>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {analysis.predictions.map((p) => (
                  <div key={p.name} className="bg-gray-800/50 border border-gray-700 rounded-lg p-4">
                    <div className="text-sm font-medium mb-2">{p.name}</div>
                    <div className="text-2xl font-bold text-blue-400 mb-2">{p.time_formatted}</div>
                    <div className="flex justify-between text-xs text-gray-400">
                      <span>{p.pace_min_km}</span>
                      <span>{p.speed_kmh.toFixed(1)} km/h</span>
                    </div>
                  </div>
                ))}
              </div>
            </CollapsibleSection>
          )}
        </>
      )}
    </div>
  )
}

function CollapsibleSection({
  title,
  icon,
  isOpen,
  onToggle,
  children,
}: {
  title: string
  icon: React.ReactNode
  isOpen: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <div className="bg-gray-800 rounded-xl mb-4 overflow-hidden">
      <button
        onClick={onToggle}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-gray-700/50 transition-colors cursor-pointer"
      >
        <span className="text-blue-400">{icon}</span>
        <span className="font-semibold flex-1">{title}</span>
        {isOpen ? <ChevronUp size={18} className="text-gray-400" /> : <ChevronDown size={18} className="text-gray-400" />}
      </button>
      {isOpen && <div className="px-4 pb-4">{children}</div>}
    </div>
  )
}
