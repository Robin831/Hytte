import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Moon, Sun, Sunrise, Sunset, RefreshCw, Circle, Sparkles, Zap } from 'lucide-react'
import { formatDate as sharedFormatDate, formatTime as sharedFormatTime } from '../utils/formatDate'

type PhaseKey = 'newMoon' | 'waxingCrescent' | 'firstQuarter' | 'waxingGibbous' | 'fullMoon' | 'waningGibbous' | 'lastQuarter' | 'waningCrescent'

interface MoonData {
  phase: string
  illumination: number
  phase_value: number
  moonrise: string | null
  moonset: string | null
  always_up?: boolean
  always_down?: boolean
}

interface SunData {
  sunrise?: string | null
  sunset?: string | null
  solar_noon?: string | null
  day_length_hours: number
  golden_hour_start?: string | null
  golden_hour_end?: string | null
  civil_dawn?: string | null
  civil_dusk?: string | null
}

interface HighlightData {
  type: string
  key: string
  params: Record<string, string>
}

interface PlanetData {
  name: string
  altitude: number
  azimuth: number
  direction: string
  visible: boolean
  status: string
  rise_time: string | null
  set_time: string | null
  magnitude: number
  elongation: number
}

interface NowResponse {
  timestamp: string
  location: { lat: number; lon: number }
  moon: MoonData
  sun: SunData
  planets: PlanetData[]
  highlights: HighlightData[]
}

interface CalendarDay {
  date: string
  phase: string
  illumination: number
  phase_value: number
}

interface CalendarResponse {
  location: { lat: number; lon: number }
  days: number
  calendar: CalendarDay[]
}

interface AuroraData {
  current_kp: number | null
  max_kp_tonight: number | null
  probability: string
  best_time: string
  best_direction: string
  entries: { time_tag: string; kp: number; source: string }[]
  location: {
    lat: number
    lon: number
    geomagnetic_lat: number
    min_kp_for_aurora: number
  }
}

const PHASE_KEY_MAP: Record<string, PhaseKey> = {
  'New Moon': 'newMoon',
  'Waxing Crescent': 'waxingCrescent',
  'First Quarter': 'firstQuarter',
  'Waxing Gibbous': 'waxingGibbous',
  'Full Moon': 'fullMoon',
  'Waning Gibbous': 'waningGibbous',
  'Last Quarter': 'lastQuarter',
  'Waning Crescent': 'waningCrescent',
}

type PlanetKey = 'mercury' | 'venus' | 'mars' | 'jupiter' | 'saturn'

const PLANET_NAME_KEY: Record<string, PlanetKey> = {
  Mercury: 'mercury',
  Venus: 'venus',
  Mars: 'mars',
  Jupiter: 'jupiter',
  Saturn: 'saturn',
}

const PLANET_COLORS: Record<string, { dot: string; bg: string; border: string; text: string }> = {
  Mercury: { dot: '#9ca3af', bg: 'from-gray-800/50 to-gray-900/50', border: 'border-gray-700/30', text: 'text-gray-300' },
  Venus: { dot: '#fbbf24', bg: 'from-yellow-950/30 to-gray-900/50', border: 'border-yellow-900/20', text: 'text-yellow-200' },
  Mars: { dot: '#ef4444', bg: 'from-red-950/30 to-gray-900/50', border: 'border-red-900/20', text: 'text-red-300' },
  Jupiter: { dot: '#f59e0b', bg: 'from-orange-950/30 to-gray-900/50', border: 'border-orange-900/20', text: 'text-orange-200' },
  Saturn: { dot: '#d4a017', bg: 'from-yellow-950/20 to-gray-900/50', border: 'border-yellow-800/20', text: 'text-yellow-300' },
}

function MoonPhaseIcon({ phaseValue, size = 120, glow = false }: { phaseValue: number; size?: number; glow?: boolean }) {
  const r = size / 2 - 4
  const cx = size / 2
  const cy = size / 2

  // Phase value: 0 = new, 0.25 = first quarter, 0.5 = full, 0.75 = last quarter
  // We draw the moon as a circle with lit and shadow portions
  const illumination = phaseValue <= 0.5
    ? phaseValue * 2 // 0..1 waxing
    : (1 - phaseValue) * 2 // 1..0 waning

  // The terminator curve: when illumination < 0.5, shadow is on the right for waxing
  // We use an elliptical arc for the terminator
  const isWaxing = phaseValue <= 0.5
  const curveX = r * (2 * illumination - 1) * (isWaxing ? 1 : -1)

  const litColor = '#f5f0c1'
  const shadowColor = '#1a1a2e'

  // For new moon, show a very faint outline
  if (phaseValue < 0.02 || phaseValue > 0.98) {
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-hidden="true">
        {glow && (
          <defs>
            <filter id="moonGlow" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="3" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>
        )}
        <circle cx={cx} cy={cy} r={r} fill={shadowColor} stroke="#444" strokeWidth="1" filter={glow ? 'url(#moonGlow)' : undefined} />
      </svg>
    )
  }

  // For full moon
  if (phaseValue > 0.48 && phaseValue < 0.52) {
    return (
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-hidden="true">
        {glow && (
          <defs>
            <filter id="moonGlowFull" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="8" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>
        )}
        <circle cx={cx} cy={cy} r={r} fill={litColor} filter={glow ? 'url(#moonGlowFull)' : undefined} />
      </svg>
    )
  }

  // General case: draw lit half and terminator
  const topY = cy - r
  const botY = cy + r

  // The lit side is a half-circle + elliptical arc
  // For waxing: lit on the right; for waning: lit on the left
  const litSweep = isWaxing ? 1 : 0

  const termR = Math.max(Math.abs(curveX), 0.1)
  const litPath = isWaxing
    ? `M ${cx} ${topY} A ${r} ${r} 0 0 ${litSweep} ${cx} ${botY} A ${termR} ${r} 0 0 ${illumination > 0.5 ? 1 : 0} ${cx} ${topY}`
    : `M ${cx} ${topY} A ${r} ${r} 0 0 ${litSweep} ${cx} ${botY} A ${termR} ${r} 0 0 ${illumination > 0.5 ? 0 : 1} ${cx} ${topY}`

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-hidden="true">
      {glow && (
        <defs>
          <filter id="moonGlowGen" x="-50%" y="-50%" width="200%" height="200%">
            <feGaussianBlur stdDeviation="6" result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>
      )}
      <circle cx={cx} cy={cy} r={r} fill={shadowColor} />
      <path d={litPath} fill={litColor} filter={glow ? 'url(#moonGlowGen)' : undefined} />
    </svg>
  )
}

function MiniMoonIcon({ phaseValue, size = 24 }: { phaseValue: number; size?: number }) {
  return <MoonPhaseIcon phaseValue={phaseValue} size={size} />
}

const STARS = Array.from({ length: 60 }, () => ({
  x: Math.random() * 100,
  y: Math.random() * 100,
  size: Math.random() * 1.5 + 0.5,
  opacity: Math.random() * 0.5 + 0.2,
  delay: Math.random() * 3,
}))

function StarField() {
  return (
    <div className="absolute inset-0 overflow-hidden pointer-events-none" aria-hidden="true">
      {STARS.map((star, i) => (
        <div
          key={i}
          className="absolute rounded-full bg-white animate-pulse"
          style={{
            left: `${star.x}%`,
            top: `${star.y}%`,
            width: `${star.size}px`,
            height: `${star.size}px`,
            opacity: star.opacity,
            animationDelay: `${star.delay}s`,
            animationDuration: `${2 + star.delay}s`,
          }}
        />
      ))}
    </div>
  )
}

function localFormatTime(iso: string | null | undefined): string {
  if (!iso) return '--:--'
  return sharedFormatTime(iso, { hour: '2-digit', minute: '2-digit' })
}

function localFormatDate(iso: string): string {
  return sharedFormatDate(iso, { weekday: 'short', day: 'numeric', month: 'short' })
}

function formatShortDate(iso: string): string {
  return sharedFormatDate(iso + 'T12:00:00', { day: 'numeric' })
}

function formatWeekday(iso: string): string {
  return sharedFormatDate(iso + 'T12:00:00', { weekday: 'narrow' })
}

function findNextPhase(calendar: CalendarDay[], targetPhases: string[]): CalendarDay | null {
  // Skip the first day (today), find the next occurrence
  for (let i = 1; i < calendar.length; i++) {
    if (targetPhases.includes(calendar[i].phase)) {
      return calendar[i]
    }
  }
  return null
}

const HIGHLIGHT_STYLES: Record<string, { bg: string; border: string; text: string }> = {
  moon_conjunction: { bg: 'from-indigo-950/40 to-gray-900/50', border: 'border-indigo-800/30', text: 'text-indigo-200' },
  planet_conjunction: { bg: 'from-purple-950/40 to-gray-900/50', border: 'border-purple-800/30', text: 'text-purple-200' },
  opposition: { bg: 'from-amber-950/40 to-gray-900/50', border: 'border-amber-800/30', text: 'text-amber-200' },
  bright_planet: { bg: 'from-yellow-950/40 to-gray-900/50', border: 'border-yellow-800/30', text: 'text-yellow-200' },
}

function resolveHighlightText(h: HighlightData, t: TFunction<readonly ['skywatch', 'common']>): string {
  const params: Record<string, string> = { ...h.params }
  // Resolve planet name keys to translated names.
  if (params.planetKey) {
    params.planet = t(`skywatch:planets.${params.planetKey}` as never)
    delete params.planetKey
  }
  if (params.planet1Key) {
    params.planet1 = t(`skywatch:planets.${params.planet1Key}` as never)
    delete params.planet1Key
  }
  if (params.planet2Key) {
    params.planet2 = t(`skywatch:planets.${params.planet2Key}` as never)
    delete params.planet2Key
  }
  if (params.directionKey) {
    params.direction = t(`skywatch:directions.${params.directionKey}` as never)
    delete params.directionKey
  }
  return t(`skywatch:${h.key}` as never, params)
}

function formatDayLength(hours: number, t: TFunction<readonly ['skywatch', 'common']>): string {
  const totalMinutes = Math.round(hours * 60)
  const h = Math.floor(totalMinutes / 60)
  const m = totalMinutes % 60
  return t('skywatch:sun.dayLengthValue', { hours: h, minutes: m })
}

const AURORA_PROBABILITY_STYLES: Record<string, { bg: string; border: string; kpColor: string; label: string }> = {
  likely: { bg: 'from-green-950/40 to-gray-900/50', border: 'border-green-800/30', kpColor: 'text-green-400', label: 'text-green-300' },
  possible: { bg: 'from-yellow-950/40 to-gray-900/50', border: 'border-yellow-800/30', kpColor: 'text-yellow-400', label: 'text-yellow-300' },
  unlikely: { bg: 'from-gray-900/50 to-gray-950/50', border: 'border-gray-700/30', kpColor: 'text-gray-400', label: 'text-gray-400' },
  unknown: { bg: 'from-gray-900/50 to-gray-950/50', border: 'border-gray-700/30', kpColor: 'text-gray-500', label: 'text-gray-500' },
}

function AuroraCard({ aurora, t }: { aurora: AuroraData; t: TFunction<readonly ['skywatch', 'common']> }) {
  const styles = AURORA_PROBABILITY_STYLES[aurora.probability] || AURORA_PROBABILITY_STYLES.unknown
  const kpDisplay = aurora.current_kp != null ? aurora.current_kp.toFixed(1) : '--'
  const maxKpDisplay = aurora.max_kp_tonight != null ? aurora.max_kp_tonight.toFixed(1) : '--'

  return (
    <div className={`bg-gradient-to-b ${styles.bg} rounded-2xl p-4 sm:p-5 border ${styles.border}`}>
      <h3 className="text-sm font-medium text-gray-300 mb-3 flex items-center gap-2">
        <Zap size={16} />
        {t('skywatch:aurora.title')}
      </h3>

      {/* Kp index hero */}
      <div className="text-center mb-4">
        <div className={`text-4xl font-bold ${styles.kpColor}`}>
          Kp {kpDisplay}
        </div>
        <div className={`text-sm mt-1 ${styles.label}`}>
          {t(`skywatch:aurora.probability_${aurora.probability}` as never)}
        </div>
      </div>

      {/* Details grid */}
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div className="bg-gray-900/40 rounded-xl p-3">
          <div className="text-gray-500 text-xs mb-1">{t('skywatch:aurora.maxKpTonight')}</div>
          <div className={`font-medium ${styles.kpColor}`}>Kp {maxKpDisplay}</div>
        </div>
        <div className="bg-gray-900/40 rounded-xl p-3">
          <div className="text-gray-500 text-xs mb-1">{t('skywatch:aurora.minKpNeeded')}</div>
          <div className="text-white font-medium">Kp {aurora.location.min_kp_for_aurora}</div>
        </div>
        <div className="bg-gray-900/40 rounded-xl p-3">
          <div className="text-gray-500 text-xs mb-1">{t('skywatch:aurora.bestTime')}</div>
          <div className="text-white font-medium">{aurora.best_time}</div>
        </div>
        <div className="bg-gray-900/40 rounded-xl p-3">
          <div className="text-gray-500 text-xs mb-1">{t('skywatch:aurora.bestDirection')}</div>
          <div className="text-white font-medium">{t(`skywatch:directions.${aurora.best_direction}` as never)}</div>
        </div>
      </div>

      {/* Geomagnetic info */}
      <div className="mt-3 text-xs text-gray-500 text-center">
        {t('skywatch:aurora.geomagneticLat', { degrees: aurora.location.geomagnetic_lat })}
      </div>
    </div>
  )
}

export default function SkyWatchPage() {
  const { t } = useTranslation(['skywatch', 'common'])

  const [now, setNow] = useState<NowResponse | null>(null)
  const [calendar, setCalendar] = useState<CalendarResponse | null>(null)
  const [aurora, setAurora] = useState<AuroraData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [yesterdayLength, setYesterdayLength] = useState<number | null>(null)
  const [retryCount, setRetryCount] = useState(0)
  const calendarScrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const controller = new AbortController()
    const signal = controller.signal
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const [nowRes, calRes] = await Promise.all([
          fetch('/api/skywatch/now', { credentials: 'include', signal }),
          fetch('/api/skywatch/moon?days=30', { credentials: 'include', signal }),
        ])

        if (!nowRes.ok || !calRes.ok) {
          throw new Error(t('skywatch:error'))
        }

        const [nowData, calData] = await Promise.all([
          nowRes.json() as Promise<NowResponse>,
          calRes.json() as Promise<CalendarResponse>,
        ])

        setNow(nowData)
        setCalendar(calData)

        // Aurora fetch is non-critical — don't block the page on failure.
        if (!signal.aborted) {
          try {
            const auroraRes = await fetch('/api/skywatch/aurora', { credentials: 'include', signal })
            if (auroraRes.ok) {
              const auroraData = await auroraRes.json() as AuroraData
              setAurora(auroraData)
            }
          } catch (e) {
            if (e instanceof DOMException && e.name === 'AbortError') throw e
          }
        }

        // Fetch yesterday's sun data for day length comparison
        if (!signal.aborted) {
          try {
            const yesterday = new Date()
            yesterday.setDate(yesterday.getDate() - 1)
            const yDate = `${yesterday.getFullYear()}-${String(yesterday.getMonth() + 1).padStart(2, '0')}-${String(yesterday.getDate()).padStart(2, '0')}`
            const yRes = await fetch(
              `/api/skywatch/now?lat=${nowData.location.lat}&lon=${nowData.location.lon}&date=${yDate}`,
              { credentials: 'include', signal }
            )
            if (yRes.ok) {
              const yData = await yRes.json() as NowResponse
              setYesterdayLength(yData.sun.day_length_hours)
            }
          } catch (e) {
            if (e instanceof DOMException && e.name === 'AbortError') throw e
          }
        }
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('common:unknownError'))
      } finally {
        setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [t, retryCount])

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="text-gray-400 animate-pulse">{t('skywatch:loading')}</div>
      </div>
    )
  }

  if (error || !now || !calendar) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="text-center">
          <p className="text-red-400 mb-4">{error || t('skywatch:error')}</p>
          <button
            onClick={() => setRetryCount(c => c + 1)}
            className="px-4 py-2 bg-gray-800 rounded-lg text-gray-300 hover:bg-gray-700 transition-colors cursor-pointer"
          >
            {t('common:actions.refresh')}
          </button>
        </div>
      </div>
    )
  }

  const phaseKey = PHASE_KEY_MAP[now.moon.phase] || 'newMoon'
  const nextFull = findNextPhase(calendar.calendar, ['Full Moon'])
  const nextNew = findNextPhase(calendar.calendar, ['New Moon'])

  const dayLengthDiff = yesterdayLength != null
    ? now.sun.day_length_hours - yesterdayLength
    : null

  return (
    <div className="min-h-screen bg-gray-950 relative">
      <StarField />

      <div className="relative z-10 max-w-2xl mx-auto px-4 py-6 sm:py-10 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold text-white">{t('skywatch:title')}</h1>
          <button
            onClick={() => setRetryCount(c => c + 1)}
            className="p-2 text-gray-400 hover:text-white transition-colors cursor-pointer"
            aria-label={t('common:actions.refresh')}
          >
            <RefreshCw size={18} />
          </button>
        </div>

        {/* Moon Hero Section */}
        <div className="bg-gradient-to-b from-indigo-950/50 to-gray-900/50 rounded-2xl p-6 sm:p-8 border border-indigo-900/30 text-center">
          <div className="flex justify-center mb-4">
            <div className="animate-pulse" style={{ animationDuration: '4s' }}>
              <MoonPhaseIcon phaseValue={now.moon.phase_value} size={140} glow />
            </div>
          </div>

          <h2 className="text-xl sm:text-2xl font-semibold text-indigo-100 mb-1">
            {t(`skywatch:phases.${phaseKey}`)}
          </h2>

          <p className="text-indigo-300/80 text-sm mb-4">
            {t('skywatch:moon.illumination', { percent: Math.round(now.moon.illumination) })}
          </p>

          <div className="grid grid-cols-2 gap-4 text-sm">
            <div className="bg-indigo-950/40 rounded-xl p-3">
              <div className="text-indigo-400 text-xs mb-1">{t('skywatch:moon.moonrise')}</div>
              <div className="text-white font-medium">
                {now.moon.always_up
                  ? t('skywatch:moon.alwaysUp')
                  : now.moon.always_down
                    ? t('skywatch:moon.alwaysDown')
                    : localFormatTime(now.moon.moonrise)}
              </div>
            </div>
            <div className="bg-indigo-950/40 rounded-xl p-3">
              <div className="text-indigo-400 text-xs mb-1">{t('skywatch:moon.moonset')}</div>
              <div className="text-white font-medium">
                {now.moon.always_up
                  ? t('skywatch:moon.alwaysUp')
                  : now.moon.always_down
                    ? t('skywatch:moon.alwaysDown')
                    : localFormatTime(now.moon.moonset)}
              </div>
            </div>
          </div>

          {/* Next full/new moon */}
          <div className="mt-4 flex justify-center gap-6 text-xs text-indigo-300/70">
            {nextFull && (
              <span>
                {t('skywatch:moon.nextFullMoon')}: {localFormatDate(nextFull.date + 'T12:00:00')}
              </span>
            )}
            {nextNew && (
              <span>
                {t('skywatch:moon.nextNewMoon')}: {localFormatDate(nextNew.date + 'T12:00:00')}
              </span>
            )}
          </div>
        </div>

        {/* Moon Calendar - Horizontal scrollable 30 days */}
        <div className="bg-gray-900/50 rounded-2xl p-4 border border-gray-800/50">
          <h3 className="text-sm font-medium text-gray-300 mb-3 flex items-center gap-2">
            <Moon size={16} />
            {t('skywatch:moon.calendar')}
          </h3>
          <div
            ref={calendarScrollRef}
            role="list"
            aria-label={t('skywatch:moon.calendar')}
            className="flex gap-1 overflow-x-auto pb-2 scrollbar-thin"
          >
            {calendar.calendar.map((day, i) => {
              const isToday = i === 0
              const dayPhaseKey = PHASE_KEY_MAP[day.phase] || 'newMoon'
              const dayPhaseLabel = t(`skywatch:phases.${dayPhaseKey}`)
              const dayDateLabel = new Date(day.date).toLocaleDateString(undefined, {
                weekday: 'long',
                month: 'long',
                day: 'numeric',
              })
              const dayAriaLabel = `${dayDateLabel} — ${dayPhaseLabel} — ${Math.round(day.illumination)}%`
              return (
                <div
                  key={day.date}
                  role="listitem"
                  aria-label={dayAriaLabel}
                  className={`flex flex-col items-center shrink-0 w-10 py-2 rounded-lg transition-colors ${
                    isToday ? 'bg-indigo-900/40 ring-1 ring-indigo-500/50' : 'hover:bg-gray-800/50'
                  }`}
                  title={dayAriaLabel}
                >
                  <span className="text-[10px] text-gray-500 mb-1">
                    {formatWeekday(day.date)}
                  </span>
                  <MiniMoonIcon phaseValue={day.phase_value} size={20} />
                  <span className={`text-[10px] mt-1 ${isToday ? 'text-indigo-300 font-medium' : 'text-gray-500'}`}>
                    {formatShortDate(day.date)}
                  </span>
                </div>
              )
            })}
          </div>
        </div>

        {/* Planets Section */}
        {now.planets && now.planets.length > 0 && (
          <div className="bg-gray-900/50 rounded-2xl p-4 sm:p-5 border border-gray-800/50">
            <h3 className="text-sm font-medium text-gray-300 mb-3 flex items-center gap-2">
              <Circle size={16} />
              {t('skywatch:planets.title')}
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              {now.planets.map((planet) => {
                const colors = PLANET_COLORS[planet.name] || PLANET_COLORS.Mercury
                const nameKey = PLANET_NAME_KEY[planet.name] || 'mercury'
                return (
                  <div
                    key={planet.name}
                    className={`bg-gradient-to-b ${colors.bg} rounded-xl p-3 border ${colors.border}`}
                  >
                    <div className="flex items-center gap-2 mb-2">
                      <div
                        className="w-3 h-3 rounded-full shrink-0"
                        style={{ backgroundColor: colors.dot }}
                      />
                      <span className={`font-medium ${colors.text}`}>
                        {t(`skywatch:planets.${nameKey}`)}
                      </span>
                      <span className="ml-auto text-xs text-gray-500">
                        {t('skywatch:planets.magnitude', { value: planet.magnitude })}
                      </span>
                    </div>
                    <div className="text-sm">
                      {planet.status === 'visible_now' ? (
                        <div className="text-green-400 font-medium">
                          {t('skywatch:planets.visibleNow')}
                        </div>
                      ) : planet.status === 'rises_at' && planet.rise_time ? (
                        <div className="text-gray-400">
                          {t('skywatch:planets.risesAt', { time: localFormatTime(planet.rise_time) })}
                        </div>
                      ) : (
                        <div className="text-gray-500">
                          {t('skywatch:planets.notVisible')}
                        </div>
                      )}
                      <div className="flex items-center gap-3 mt-1 text-xs text-gray-500">
                        {planet.altitude > 0 && (
                          <span>{t('skywatch:planets.altitude', { degrees: planet.altitude })}</span>
                        )}
                        {planet.altitude > 0 && (
                          <span>{t(`skywatch:directions.${planet.direction}` as never)}</span>
                        )}
                        <span>{t('skywatch:planets.elongation', { degrees: planet.elongation })}</span>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* Tonight's Highlights */}
        {now.highlights && now.highlights.length > 0 && (
          <div className="bg-gray-900/50 rounded-2xl p-4 sm:p-5 border border-gray-800/50">
            <h3 className="text-sm font-medium text-gray-300 mb-3 flex items-center gap-2">
              <Sparkles size={16} />
              {t('skywatch:highlights.title')}
            </h3>
            <div className="space-y-2">
              {now.highlights.map((highlight) => {
                const styles = HIGHLIGHT_STYLES[highlight.type] || HIGHLIGHT_STYLES.bright_planet
                const sortedParamsEntries = Object.entries(highlight.params).sort(([a], [b]) => a.localeCompare(b))
                const stableKey = `${highlight.type}-${highlight.key}-${JSON.stringify(sortedParamsEntries)}`
                return (
                  <div
                    key={stableKey}
                    className={`bg-gradient-to-r ${styles.bg} rounded-xl px-4 py-3 border ${styles.border}`}
                  >
                    <p className={`text-sm ${styles.text}`}>
                      {resolveHighlightText(highlight, t)}
                    </p>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* Aurora Card */}
        {aurora && (
          <AuroraCard aurora={aurora} t={t} />
        )}

        {/* Sun Card */}
        <div className="bg-gradient-to-b from-amber-950/30 to-gray-900/50 rounded-2xl p-5 border border-amber-900/20">
          <h3 className="text-sm font-medium text-amber-200/80 mb-4 flex items-center gap-2">
            <Sun size={16} />
            {t('skywatch:sun.title')}
          </h3>

          <div className="grid grid-cols-2 gap-3 text-sm">
            {/* Sunrise */}
            <div className="flex items-center gap-2">
              <Sunrise size={16} className="text-amber-400 shrink-0" />
              <div>
                <div className="text-gray-400 text-xs">{t('skywatch:sun.sunrise')}</div>
                <div className="text-white font-medium">{localFormatTime(now.sun.sunrise)}</div>
              </div>
            </div>

            {/* Sunset */}
            <div className="flex items-center gap-2">
              <Sunset size={16} className="text-orange-400 shrink-0" />
              <div>
                <div className="text-gray-400 text-xs">{t('skywatch:sun.sunset')}</div>
                <div className="text-white font-medium">{localFormatTime(now.sun.sunset)}</div>
              </div>
            </div>

            {/* Day length */}
            <div>
              <div className="text-gray-400 text-xs">{t('skywatch:sun.dayLength')}</div>
              <div className="text-white font-medium">
                {formatDayLength(now.sun.day_length_hours, t)}
              </div>
              {dayLengthDiff != null && (
                <div className={`text-xs mt-0.5 ${dayLengthDiff >= 0 ? 'text-green-400' : 'text-orange-400'}`}>
                  {t(
                    dayLengthDiff >= 0
                      ? 'skywatch:sun.dayLengthDiffLonger'
                      : 'skywatch:sun.dayLengthDiffShorter',
                    {
                      count: Math.abs(Math.round(dayLengthDiff * 60)),
                      minutes: Math.abs(Math.round(dayLengthDiff * 60)),
                    }
                  )}
                </div>
              )}
            </div>

            {/* Golden hour */}
            <div>
              <div className="text-gray-400 text-xs">{t('skywatch:sun.goldenHour')}</div>
              <div className="text-amber-200 font-medium">
                {localFormatTime(now.sun.golden_hour_start)} – {localFormatTime(now.sun.golden_hour_end)}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
