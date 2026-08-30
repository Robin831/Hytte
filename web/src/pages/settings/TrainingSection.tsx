import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  secToMMSS,
  mmssToSec,
  computeDefaultZoneDrafts,
  ZONE_NAME_KEYS,
  type PreferenceSectionProps,
} from './types'

interface TrainingSectionProps extends PreferenceSectionProps {
  queuePreference: (key: string, value: string) => void
  flushPreferences: () => void
  // Whether the user has the `stride` feature. The Stride switches are hidden
  // without it — an "Enable Stride" toggle for a user who has no Stride page
  // would be misleading.
  hasStride: boolean
}

// Initialize zone drafts from stored boundaries or computed defaults.
function initialZoneDrafts(prefs: Record<string, string>): Array<{ min: string; max: string }> {
  if (prefs.zone_boundaries) {
    try {
      const parsed = JSON.parse(prefs.zone_boundaries)
      const stored = Array.isArray(parsed) ? parsed : []
      const validEntries = stored.filter(
        (z: unknown) =>
          z !== null &&
          typeof z === 'object' &&
          typeof (z as Record<string, unknown>).zone === 'number' &&
          typeof (z as Record<string, unknown>).min_bpm === 'number' &&
          typeof (z as Record<string, unknown>).max_bpm === 'number',
      ) as Array<{ zone: number; min_bpm: number; max_bpm: number }>
      const zones = validEntries.map((z) => z.zone)
      const uniqueZones = new Set(zones)
      const expectedZones = [1, 2, 3, 4, 5]
      const hasAllExpectedZones =
        validEntries.length === expectedZones.length &&
        uniqueZones.size === expectedZones.length &&
        expectedZones.every((z) => uniqueZones.has(z))
      if (hasAllExpectedZones) {
        const sorted = [...validEntries].sort((a, b) => a.zone - b.zone)
        return sorted.map((z) => ({ min: String(z.min_bpm), max: String(z.max_bpm) }))
      } else {
        const mhr = parseInt(prefs.max_hr || '')
        if (!isNaN(mhr) && mhr >= 100) return computeDefaultZoneDrafts(mhr)
      }
    } catch {
      const mhr = parseInt(prefs.max_hr || '')
      if (!isNaN(mhr) && mhr >= 100) return computeDefaultZoneDrafts(mhr)
    }
  } else {
    const mhr = parseInt(prefs.max_hr || '')
    if (!isNaN(mhr) && mhr >= 100) return computeDefaultZoneDrafts(mhr)
  }
  return []
}

function TrainingSection({ preferences, saving, savePreference, savePreferences, queuePreference, flushPreferences, hasStride }: TrainingSectionProps) {
  const { t } = useTranslation(['settings', 'common'])
  const [maxHRDraft, setMaxHRDraft] = useState<string>(preferences.max_hr || '')
  const [thresholdHRDraft, setThresholdHRDraft] = useState<string>(preferences.threshold_hr || '')
  const [thresholdPaceDraft, setThresholdPaceDraft] = useState<string>(secToMMSS(preferences.threshold_pace || ''))
  const [restingHRDraft, setRestingHRDraft] = useState<string>(preferences.resting_hr || '')
  const [easyPaceMinDraft, setEasyPaceMinDraft] = useState<string>(secToMMSS(preferences.easy_pace_min || ''))
  const [easyPaceMaxDraft, setEasyPaceMaxDraft] = useState<string>(secToMMSS(preferences.easy_pace_max || ''))
  const [strideCustomPromptDraft, setStrideCustomPromptDraft] = useState(preferences.stride_custom_prompt || '')
  const [strideTreadmillCalibrationDraft, setStrideTreadmillCalibrationDraft] = useState(
    preferences.stride_treadmill_calibration || ''
  )
  // The numeric Stride drafts hold *only* an unsaved edit: null means "show the
  // stored preference". A useState initializer runs once, so seeding them from
  // `preferences` would strand the inputs empty whenever the preference map
  // arrives or changes after mount; deriving the displayed value keeps them on
  // the same source of truth as the toggle above, which reads `preferences`.
  const [strideAvailableDaysDraft, setStrideAvailableDaysDraft] = useState<string | null>(null)
  const [strideWeeklyDistanceCapDraft, setStrideWeeklyDistanceCapDraft] = useState<string | null>(null)
  const strideAvailableDaysValue = strideAvailableDaysDraft ?? (preferences.stride_available_days || '')
  const strideWeeklyDistanceCapValue =
    strideWeeklyDistanceCapDraft ?? (preferences.stride_weekly_distance_cap || '')
  const [zoneDrafts, setZoneDrafts] = useState<Array<{ min: string; max: string }>>(() => initialZoneDrafts(preferences))
  const [zoneError, setZoneError] = useState<string | null>(null)
  const [autoDetecting, setAutoDetecting] = useState(false)
  const [autoDetectError, setAutoDetectError] = useState<string | null>(null)

  const autoDetectFromLactate = async () => {
    setAutoDetecting(true)
    setAutoDetectError(null)
    try {
      const listRes = await fetch('/api/lactate/tests', { credentials: 'include' })
      if (!listRes.ok) throw new Error('failed to load lactate tests')
      const listData = await listRes.json()
      const tests: Array<{ id: number }> = listData.tests || []
      if (tests.length === 0) {
        setAutoDetectError(t('training.autoDetectFailed'))
        return
      }
      const testId = tests[0].id
      const threshRes = await fetch(`/api/lactate/tests/${testId}/thresholds`, { credentials: 'include' })
      if (!threshRes.ok) throw new Error('failed to load thresholds')
      const threshData = await threshRes.json()
      const thresholds: Array<{ valid: boolean; heart_rate_bpm: number; speed_kmh: number }> = threshData.thresholds || []
      const best = thresholds.find((tr) => tr.valid)
      if (!best) {
        setAutoDetectError(t('training.autoDetectFailed'))
        return
      }
      const newHR = best.heart_rate_bpm > 0 ? String(best.heart_rate_bpm) : ''
      const newPaceSec = best.speed_kmh > 0 ? String(Math.round(3600 / best.speed_kmh)) : ''
      const newPaceDisplay = secToMMSS(newPaceSec)
      if (newHR) setThresholdHRDraft(newHR)
      if (newPaceDisplay) setThresholdPaceDraft(newPaceDisplay)
      const prefsToSave: Record<string, string> = {}
      if (newHR) prefsToSave.threshold_hr = newHR
      if (newPaceSec) prefsToSave.threshold_pace = newPaceSec
      if (Object.keys(prefsToSave).length > 0) {
        await savePreferences(prefsToSave)
      }
    } catch {
      setAutoDetectError(t('training.autoDetectFailed'))
    } finally {
      setAutoDetecting(false)
    }
  }

  const resetZonesToDefault = () => {
    const maxHR = parseInt(maxHRDraft || preferences.max_hr || '')
    if (isNaN(maxHR) || maxHR < 100 || maxHR > 230) return
    setZoneDrafts(computeDefaultZoneDrafts(maxHR))
    setZoneError(null)
  }

  const saveZoneBoundaries = async () => {
    if (zoneDrafts.length !== 5) {
      setZoneError(t('training.zoneInvalid'))
      return
    }
    const zones = zoneDrafts.map((d, i) => ({
      zone: i + 1,
      min_bpm: parseInt(d.min),
      max_bpm: parseInt(d.max),
    }))
    for (const z of zones) {
      if (isNaN(z.min_bpm) || isNaN(z.max_bpm) || z.min_bpm < 0 || z.max_bpm <= z.min_bpm || z.max_bpm > 300) {
        setZoneError(t('training.zoneInvalid'))
        return
      }
    }
    for (let i = 1; i < zones.length; i++) {
      if (zones[i].min_bpm < zones[i - 1].max_bpm) {
        setZoneError(t('training.zoneInvalid'))
        return
      }
    }
    setZoneError(null)
    await savePreferences({ zone_boundaries: JSON.stringify(zones) }, true)
  }

  return (
    <>
      <div className="flex items-center justify-between">
        <div>
          <p className="font-medium">{t('training.maxHeartRate')}</p>
          <p className="text-sm text-gray-400">{t('training.maxHeartRateDescription')}</p>
        </div>
        <input
          type="number"
          min="100"
          max="230"
          value={maxHRDraft}
          onChange={(e) => setMaxHRDraft(e.target.value)}
          onBlur={() => {
            if (maxHRDraft === '') {
              queuePreference('max_hr', '')
            } else {
              const num = parseInt(maxHRDraft)
              if (num >= 100 && num <= 230) {
                queuePreference('max_hr', maxHRDraft)
                if (zoneDrafts.length === 0 && !preferences.zone_boundaries) {
                  setZoneDrafts(computeDefaultZoneDrafts(num))
                }
              } else {
                setMaxHRDraft(preferences.max_hr || '')
              }
            }
          }}
          placeholder={t('training.maxHeartRatePlaceholder')}
          disabled={saving}
          aria-label={t('training.maxHeartRate')}
          className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      <div className="mt-4 pt-4 border-t border-gray-700 space-y-4">
        {/* Threshold HR */}
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('training.thresholdHeartRate')}</p>
            <p className="text-sm text-gray-400">{t('training.thresholdHeartRateDescription')}</p>
          </div>
          <input
            type="number"
            min="100"
            max="220"
            value={thresholdHRDraft}
            onChange={(e) => setThresholdHRDraft(e.target.value)}
            onBlur={() => {
              if (thresholdHRDraft === '') {
                queuePreference('threshold_hr', '')
              } else {
                const num = parseInt(thresholdHRDraft)
                if (num >= 100 && num <= 220) {
                  queuePreference('threshold_hr', thresholdHRDraft)
                } else {
                  setThresholdHRDraft(preferences.threshold_hr || '')
                }
              }
            }}
            placeholder={t('training.thresholdHeartRatePlaceholder')}
            disabled={saving}
            aria-label={t('training.thresholdHeartRate')}
            className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Threshold Pace */}
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('training.thresholdPace')}</p>
            <p className="text-sm text-gray-400">{t('training.thresholdPaceDescription')}</p>
          </div>
          <input
            type="text"
            value={thresholdPaceDraft}
            onChange={(e) => setThresholdPaceDraft(e.target.value)}
            onBlur={() => {
              if (thresholdPaceDraft === '') {
                queuePreference('threshold_pace', '')
              } else {
                const secStr = mmssToSec(thresholdPaceDraft)
                if (secStr) {
                  queuePreference('threshold_pace', secStr)
                } else {
                  setThresholdPaceDraft(secToMMSS(preferences.threshold_pace || ''))
                }
              }
            }}
            placeholder={t('training.thresholdPacePlaceholder')}
            disabled={saving}
            aria-label={t('training.thresholdPace')}
            className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Resting HR */}
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('training.restingHeartRate')}</p>
            <p className="text-sm text-gray-400">{t('training.restingHeartRateDescription')}</p>
          </div>
          <input
            type="number"
            min="30"
            max="100"
            value={restingHRDraft}
            onChange={(e) => setRestingHRDraft(e.target.value)}
            onBlur={() => {
              if (restingHRDraft === '') {
                queuePreference('resting_hr', '')
              } else {
                const num = parseInt(restingHRDraft)
                if (num >= 30 && num <= 100) {
                  queuePreference('resting_hr', restingHRDraft)
                } else {
                  setRestingHRDraft(preferences.resting_hr || '')
                }
              }
            }}
            placeholder={t('training.restingHeartRatePlaceholder')}
            disabled={saving}
            aria-label={t('training.restingHeartRate')}
            className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Easy Pace Min */}
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('training.easyPaceMin')}</p>
            <p className="text-sm text-gray-400">{t('training.easyPaceMinDescription')}</p>
          </div>
          <input
            type="text"
            value={easyPaceMinDraft}
            onChange={(e) => setEasyPaceMinDraft(e.target.value)}
            onBlur={() => {
              if (easyPaceMinDraft === '') {
                queuePreference('easy_pace_min', '')
              } else {
                const secStr = mmssToSec(easyPaceMinDraft)
                if (secStr) {
                  queuePreference('easy_pace_min', secStr)
                } else {
                  setEasyPaceMinDraft(secToMMSS(preferences.easy_pace_min || ''))
                }
              }
            }}
            placeholder={t('training.easyPaceMinPlaceholder')}
            disabled={saving}
            aria-label={t('training.easyPaceMin')}
            className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Easy Pace Max */}
        <div className="flex items-center justify-between">
          <div>
            <p className="font-medium">{t('training.easyPaceMax')}</p>
            <p className="text-sm text-gray-400">{t('training.easyPaceMaxDescription')}</p>
          </div>
          <input
            type="text"
            value={easyPaceMaxDraft}
            onChange={(e) => setEasyPaceMaxDraft(e.target.value)}
            onBlur={() => {
              if (easyPaceMaxDraft === '') {
                queuePreference('easy_pace_max', '')
              } else {
                const secStr = mmssToSec(easyPaceMaxDraft)
                if (secStr) {
                  queuePreference('easy_pace_max', secStr)
                } else {
                  setEasyPaceMaxDraft(secToMMSS(preferences.easy_pace_max || ''))
                }
              }
            }}
            placeholder={t('training.easyPaceMaxPlaceholder')}
            disabled={saving}
            aria-label={t('training.easyPaceMax')}
            className="w-24 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Auto-detect from lactate test */}
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-gray-400">{t('training.autoDetectDescription')}</p>
            {autoDetectError && (
              <p className="text-sm text-red-400 mt-1">{autoDetectError}</p>
            )}
          </div>
          <button
            type="button"
            onClick={autoDetectFromLactate}
            disabled={autoDetecting || saving}
            className="px-3 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg transition-colors"
          >
            {autoDetecting ? t('training.autoDetecting') : t('training.autoDetect')}
          </button>
        </div>

        {/* Zone boundaries editor */}
        <div className="border-t border-gray-700 pt-4 mt-4">
          <div className="flex items-center justify-between mb-3">
            <p className="text-sm font-medium text-gray-300">{t('training.zonesHeading')}</p>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={resetZonesToDefault}
                disabled={saving || (!parseInt(maxHRDraft || preferences.max_hr || ''))}
                className="px-3 py-1.5 text-xs bg-gray-700 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg transition-colors"
              >
                {t('training.zoneReset')}
              </button>
              <button
                type="button"
                onClick={saveZoneBoundaries}
                disabled={saving || zoneDrafts.length === 0}
                className="px-3 py-1.5 text-xs bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed rounded-lg transition-colors"
              >
                {t('training.zoneSave')}
              </button>
            </div>
          </div>
          {zoneError && (
            <p className="text-xs text-red-400 mb-2">{zoneError}</p>
          )}
          {zoneDrafts.length === 0 ? (
            <p className="text-sm text-gray-500">{t('training.zonesRequireMaxHR')}</p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr>
                  <th className="text-left pb-1.5 text-xs text-gray-500 font-medium w-16"></th>
                  <th className="text-left pb-1.5 text-xs text-gray-500 font-medium"></th>
                  <th className="text-right pb-1.5 text-xs text-gray-500 font-medium pr-2">{t('training.zoneBPMMin')}</th>
                  <th className="text-right pb-1.5 text-xs text-gray-500 font-medium">{t('training.zoneBPMMax')}</th>
                </tr>
              </thead>
              <tbody>
                {ZONE_NAME_KEYS.map((nameKey, i) => (
                  <tr key={i + 1} className="border-b border-gray-700 last:border-0">
                    <td className="py-1.5 text-gray-400 pr-2">{t('training.zone', { n: i + 1 })}</td>
                    <td className="py-1.5 text-gray-300 pr-2">{(t as (k: string) => string)(`training.${nameKey}`)}</td>
                    <td className="py-1.5 text-right pr-2">
                      <input
                        type="number"
                        value={zoneDrafts[i]?.min ?? ''}
                        onChange={(e) => {
                          const next = [...zoneDrafts]
                          next[i] = { ...next[i], min: e.target.value }
                          setZoneDrafts(next)
                          setZoneError(null)
                        }}
                        min={0}
                        max={299}
                        aria-label={t('training.zoneMinAriaLabel', { n: i + 1 })}
                        className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
                      />
                    </td>
                    <td className="py-1.5 text-right">
                      <input
                        type="number"
                        value={zoneDrafts[i]?.max ?? ''}
                        onChange={(e) => {
                          const next = [...zoneDrafts]
                          next[i] = { ...next[i], max: e.target.value }
                          setZoneDrafts(next)
                          setZoneError(null)
                        }}
                        min={1}
                        max={300}
                        aria-label={t('training.zoneMaxAriaLabel', { n: i + 1 })}
                        className="w-16 bg-gray-700 border border-gray-600 rounded px-2 py-1 text-xs text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        {/* AI Preferences */}
        <div className="border-t border-gray-700 pt-4 mt-4">
          <p className="text-sm font-medium text-gray-300 mb-3">{t('training.aiPreferences')}</p>
          <div className="flex items-center justify-between">
            <div>
              <p className="font-medium">{t('training.autoAnalyze')}</p>
              <p className="text-sm text-gray-400">{t('training.autoAnalyzeDescription')}</p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={preferences.ai_auto_analyze === 'true'}
              onClick={() =>
                savePreference('ai_auto_analyze', preferences.ai_auto_analyze === 'true' ? 'false' : 'true')
              }
              disabled={saving}
              aria-label={preferences.ai_auto_analyze === 'true' ? t('training.disableAutoAnalyze') : t('training.enableAutoAnalyze')}
              className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                preferences.ai_auto_analyze === 'true' ? 'bg-blue-600' : 'bg-gray-600'
              }`}
            >
              <span
                className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                  preferences.ai_auto_analyze === 'true' ? 'translate-x-6' : 'translate-x-1'
                }`}
              />
            </button>
          </div>
        </div>

        {/* Stride — every Stride-only preference, kept here rather than on the
            Stride page so training preferences live in one place, and hidden
            entirely for users without the Stride feature. The server enforces
            the same gate: PUT /api/settings/preferences rejects stride_* keys
            for a user without the feature. */}
        {hasStride && (
          <div className="border-t border-gray-700 pt-4 mt-4">
            <p className="text-sm font-medium text-gray-300 mb-3">{t('training.strideHeading')}</p>
            <div className="space-y-4">
              {/* Enable Stride */}
              <div className="flex items-center justify-between gap-4">
                <div>
                  <p className="font-medium">{t('training.strideEnabled')}</p>
                  <p className="text-sm text-gray-400">{t('training.strideEnabledDescription')}</p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={preferences.stride_enabled === 'true'}
                  onClick={() =>
                    savePreference('stride_enabled', preferences.stride_enabled === 'true' ? 'false' : 'true')
                  }
                  disabled={saving}
                  aria-label={preferences.stride_enabled === 'true' ? t('training.disableStride') : t('training.enableStride')}
                  className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                    preferences.stride_enabled === 'true' ? 'bg-blue-600' : 'bg-gray-600'
                  }`}
                >
                  <span
                    className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                      preferences.stride_enabled === 'true' ? 'translate-x-6' : 'translate-x-1'
                    }`}
                  />
                </button>
              </div>

              {/* Training days per week */}
              <div className="flex items-center justify-between gap-4">
                <div>
                  <p className="font-medium">{t('training.strideAvailableDays')}</p>
                  <p className="text-sm text-gray-400">{t('training.strideAvailableDaysDescription')}</p>
                </div>
                <input
                  id="stride-available-days"
                  type="number"
                  min="1"
                  max="7"
                  value={strideAvailableDaysValue}
                  onChange={(e) => setStrideAvailableDaysDraft(e.target.value)}
                  onBlur={() => {
                    const draft = strideAvailableDaysValue.trim()
                    if (draft === '') {
                      queuePreference('stride_available_days', '')
                    } else if (/^\d+$/.test(draft) && Number(draft) >= 1 && Number(draft) <= 7) {
                      // The digits-only test matters: parseInt would accept
                      // "3abc" from a keyboard the number input does not filter.
                      queuePreference('stride_available_days', String(Number(draft)))
                    }
                    // Dropping the draft falls back to the stored value: the
                    // accepted write above (normalised, so "07" shows as "7"),
                    // or the saved value when the entry was out of range.
                    setStrideAvailableDaysDraft(null)
                  }}
                  placeholder={t('training.strideAvailableDaysPlaceholder')}
                  disabled={saving}
                  aria-label={t('training.strideAvailableDays')}
                  className="w-24 shrink-0 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              {/* Weekly distance cap */}
              <div className="flex items-center justify-between gap-4">
                <div>
                  <p className="font-medium">{t('training.strideWeeklyDistanceCap')}</p>
                  <p className="text-sm text-gray-400">{t('training.strideWeeklyDistanceCapDescription')}</p>
                </div>
                <input
                  id="stride-weekly-distance-cap"
                  type="number"
                  min="1"
                  max="500"
                  value={strideWeeklyDistanceCapValue}
                  onChange={(e) => setStrideWeeklyDistanceCapDraft(e.target.value)}
                  onBlur={() => {
                    const draft = strideWeeklyDistanceCapValue.trim()
                    if (draft === '') {
                      queuePreference('stride_weekly_distance_cap', '')
                    } else if (/^\d+$/.test(draft) && Number(draft) >= 1 && Number(draft) <= 500) {
                      queuePreference('stride_weekly_distance_cap', String(Number(draft)))
                    }
                    setStrideWeeklyDistanceCapDraft(null)
                  }}
                  placeholder={t('training.strideWeeklyDistanceCapPlaceholder')}
                  disabled={saving}
                  aria-label={t('training.strideWeeklyDistanceCap')}
                  className="w-24 shrink-0 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              {/* Stride Custom Prompt */}
              <div>
                <label htmlFor="stride-custom-prompt">
                  <p className="font-medium">{t('training.strideCustomPrompt')}</p>
                  <p className="text-sm text-gray-400">{t('training.strideCustomPromptDescription')}</p>
                </label>
                <textarea
                  id="stride-custom-prompt"
                  rows={4}
                  value={strideCustomPromptDraft}
                  onChange={(e) => {
                    setStrideCustomPromptDraft(e.target.value)
                    queuePreference('stride_custom_prompt', e.target.value)
                  }}
                  onBlur={() => flushPreferences()}
                  placeholder={t('training.strideCustomPromptPlaceholder')}
                  disabled={saving}
                  className="mt-2 w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
                />
              </div>

              {/* Treadmill calibration — persisted so the coach reuses the athlete's
                  own measured belt/HR offsets instead of re-deriving them each week. */}
              <div>
                <label htmlFor="stride-treadmill-calibration">
                  <p className="font-medium">{t('training.strideTreadmillCalibration')}</p>
                  <p className="text-sm text-gray-400">{t('training.strideTreadmillCalibrationDescription')}</p>
                </label>
                <textarea
                  id="stride-treadmill-calibration"
                  rows={4}
                  value={strideTreadmillCalibrationDraft}
                  onChange={(e) => {
                    setStrideTreadmillCalibrationDraft(e.target.value)
                    queuePreference('stride_treadmill_calibration', e.target.value)
                  }}
                  onBlur={() => flushPreferences()}
                  placeholder={t('training.strideTreadmillCalibrationPlaceholder')}
                  disabled={saving}
                  className="mt-2 w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
                />
              </div>
            </div>
          </div>
        )}
      </div>
    </>
  )
}

export default TrainingSection
