import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Moon, Sun, Sunrise, Sunset, RefreshCw } from 'lucide-react'
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

interface NowResponse {
  timestamp: string
  location: { lat: number; lon: number }
  moon: MoonData
  sun: SunData
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

function formatDayLength(hours: number, t: TFunction<readonly ['skywatch', 'common']>): string {
  const totalMinutes = Math.round(hours * 60)
  const h = Math.floor(totalMinutes / 60)
  const m = totalMinutes % 60
  return t('skywatch:sun.dayLengthValue', { hours: h, minutes: m })
}

export default function SkyWatchPage() {
  const { t } = useTranslation(['skywatch', 'common'])

  const [now, setNow] = useState<NowResponse | null>(null)
  const [calendar, setCalendar] = useState<CalendarResponse | null>(null)
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

        // Fetch yesterday's sun data for day length comparison
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
        } catch {
          // Non-critical — just skip the comparison
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
              return (
                <div
                  key={day.date}
                  role="listitem"
                  aria-label={`${dayPhaseLabel} — ${Math.round(day.illumination)}%`}
                  className={`flex flex-col items-center shrink-0 w-10 py-2 rounded-lg transition-colors ${
                    isToday ? 'bg-indigo-900/40 ring-1 ring-indigo-500/50' : 'hover:bg-gray-800/50'
                  }`}
                  title={`${dayPhaseLabel} — ${Math.round(day.illumination)}%`}
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
                  {dayLengthDiff >= 0 ? '+' : ''}{Math.round(dayLengthDiff * 60)}{t('skywatch:sun.minutesVsYesterday')}
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
