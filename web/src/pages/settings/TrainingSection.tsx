import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  secToMMSS,
  mmssToSec,
  isValidTargetTime,
  computeDefaultZoneDrafts,
  ZONE_NAME_KEYS,
  type PreferenceSectionProps,
} from './types'

interface TrainingSectionProps extends PreferenceSectionProps {
  queuePreference: (key: string, value: string) => void
  flushPreferences: () => void
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

function TrainingSection({ preferences, saving, savePreference, savePreferences, queuePreference, flushPreferences }: TrainingSectionProps) {
  const { t } = useTranslation(['settings', 'common'])
  const [maxHRDraft, setMaxHRDraft] = useState<string>(preferences.max_hr || '')
  const [thresholdHRDraft, setThresholdHRDraft] = useState<string>(preferences.threshold_hr || '')
  const [thresholdPaceDraft, setThresholdPaceDraft] = useState<string>(secToMMSS(preferences.threshold_pace || ''))
  const [restingHRDraft, setRestingHRDraft] = useState<string>(preferences.resting_hr || '')
  const [easyPaceMinDraft, setEasyPaceMinDraft] = useState<string>(secToMMSS(preferences.easy_pace_min || ''))
  const [easyPaceMaxDraft, setEasyPaceMaxDraft] = useState<string>(secToMMSS(preferences.easy_pace_max || ''))
  const [strideCustomPromptDraft, setStrideCustomPromptDraft] = useState(preferences.stride_custom_prompt || '')
  const [goalRaceNameDraft, setGoalRaceNameDraft] = useState<string>(preferences.goal_race_name || '')
  const [goalRaceDateDraft, setGoalRaceDateDraft] = useState<string>(preferences.goal_race_date || '')
  const [goalRaceDistanceDraft, setGoalRaceDistanceDraft] = useState<string>(preferences.goal_race_distance || '')
  const [goalRaceTargetTimeDraft, setGoalRaceTargetTimeDraft] = useState<string>(preferences.goal_race_target_time || '')
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

  // Compute weeks until race day.
  const weeksUntilRace = useMemo(() => {
    if (!goalRaceDateDraft) return null
    const raceDate = new Date(goalRaceDateDraft + 'T00:00:00')
    if (isNaN(raceDate.getTime())) return null
    const now = new Date()
    now.setHours(0, 0, 0, 0)
    const diffMs = raceDate.getTime() - now.getTime()
    if (diffMs < 0) return -1
    if (diffMs === 0) return 0
    return Math.ceil(diffMs / (7 * 24 * 60 * 60 * 1000))
  }, [goalRaceDateDraft])

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

          {/* Stride Custom Prompt */}
          <div className="mt-4">
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
        </div>
        {/* Goal Race */}
        <div className="border-t border-gray-700 pt-4 mt-4">
          <p className="text-sm font-medium text-gray-300 mb-3">{t('goalRace.heading')}</p>
          <div className="space-y-4">
            {/* Race name */}
            <div className="flex items-center justify-between gap-4">
              <label htmlFor="goal-race-name" className="font-medium shrink-0">{t('goalRace.raceName')}</label>
              <input
                id="goal-race-name"
                type="text"
                value={goalRaceNameDraft}
                onChange={(e) => setGoalRaceNameDraft(e.target.value)}
                onBlur={() => queuePreference('goal_race_name', goalRaceNameDraft)}
                placeholder={t('goalRace.raceNamePlaceholder')}
                disabled={saving}
                className="w-56 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>

            {/* Race date */}
            <div className="flex items-center justify-between gap-4">
              <label htmlFor="goal-race-date" className="font-medium shrink-0">{t('goalRace.raceDate')}</label>
              <input
                id="goal-race-date"
                type="date"
                value={goalRaceDateDraft}
                onChange={(e) => {
                  setGoalRaceDateDraft(e.target.value)
                  savePreference('goal_race_date', e.target.value, true)
                }}
                disabled={saving}
                className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 [color-scheme:dark]"
              />
            </div>

            {/* Distance */}
            <div className="flex items-center justify-between gap-4">
              <label htmlFor="goal-race-distance" className="font-medium shrink-0">{t('goalRace.raceDistance')}</label>
              <select
                id="goal-race-distance"
                value={goalRaceDistanceDraft}
                onChange={(e) => {
                  setGoalRaceDistanceDraft(e.target.value)
                  savePreference('goal_race_distance', e.target.value, true)
                }}
                disabled={saving}
                className="bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="">{t('goalRace.distancePlaceholder')}</option>
                <option value="5K">{t('goalRace.distance5K')}</option>
                <option value="10K">{t('goalRace.distance10K')}</option>
                <option value="half_marathon">{t('goalRace.distanceHalf')}</option>
                <option value="marathon">{t('goalRace.distanceMarathon')}</option>
                <option value="custom">{t('goalRace.distanceCustom')}</option>
              </select>
            </div>

            {/* Target time */}
            <div className="flex items-center justify-between gap-4">
              <label htmlFor="goal-race-target-time" className="font-medium shrink-0">{t('goalRace.targetTime')}</label>
              <input
                id="goal-race-target-time"
                type="text"
                value={goalRaceTargetTimeDraft}
                onChange={(e) => setGoalRaceTargetTimeDraft(e.target.value)}
                onBlur={() => {
                  if (goalRaceTargetTimeDraft === '') {
                    queuePreference('goal_race_target_time', '')
                  } else if (isValidTargetTime(goalRaceTargetTimeDraft)) {
                    queuePreference('goal_race_target_time', goalRaceTargetTimeDraft)
                  } else {
                    setGoalRaceTargetTimeDraft(preferences.goal_race_target_time || '')
                  }
                }}
                placeholder={t('goalRace.targetTimePlaceholder')}
                disabled={saving}
                aria-label={t('goalRace.targetTime')}
                className="w-32 bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white font-mono text-right focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>

            {/* Countdown */}
            {goalRaceDateDraft && (
              <div className="pt-2 text-sm font-medium">
                {weeksUntilRace === null ? null : weeksUntilRace === -1 ? (
                  <span className="text-gray-400">{t('goalRace.raceInPast')}</span>
                ) : weeksUntilRace === 0 ? (
                  <span className="text-green-400">{t('goalRace.raceToday')}</span>
                ) : (
                  <span className="text-blue-400">{t('goalRace.countdown', { count: weeksUntilRace })}</span>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  )
}

export default TrainingSection
