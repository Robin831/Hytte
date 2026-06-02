import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2 } from 'lucide-react'
import { Skeleton } from '../../../components/ui/skeleton'
import { ConfirmDialog } from '../../../components/ui/dialog'
import { Select } from '../../../components/ui/select'
import type { WorkDeductionPreset } from '../types'
import { useWorkHoursApi } from '../useWorkHoursApi'
import EmojiPickerDropdown from '../components/EmojiPickerDropdown'
import { normalizePresetIcon } from '../presetIcons'

export default function SettingsView() {
  const { t } = useTranslation(['workhours', 'common'])
  const api = useWorkHoursApi()

  // Work settings
  const [standardHours, setStandardHours] = useState('7.5')
  const [rounding, setRounding] = useState('30')
  const [lunchMinutes, setLunchMinutes] = useState('30')
  const [vacationAllowance, setVacationAllowance] = useState('25')
  const [settingsLoaded, setSettingsLoaded] = useState(false)
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [showResetFlexConfirm, setShowResetFlexConfirm] = useState(false)

  // Presets
  const [presets, setPresets] = useState<WorkDeductionPreset[]>([])
  const [presetsLoading, setPresetsLoading] = useState(false)
  const [presetSaving, setPresetSaving] = useState(false)
  const [newPresetName, setNewPresetName] = useState('')
  const [newPresetMinutes, setNewPresetMinutes] = useState('')
  const [newPresetIcon, setNewPresetIcon] = useState('')
  const [editingPreset, setEditingPreset] = useState<WorkDeductionPreset | null>(null)
  const [editName, setEditName] = useState('')
  const [editMinutes, setEditMinutes] = useState('')
  const [editIcon, setEditIcon] = useState('')

  const loadPresets = useCallback(() => {
    setPresetsLoading(true)
    api.getPresets()
      .then(setPresets)
      .catch(() => {})
      .finally(() => setPresetsLoading(false))
  }, [api])

  useEffect(() => {
    api.getPreferences()
      .then(prefs => {
        if (prefs) {
          if (prefs.work_hours_standard_day) {
            setStandardHours((parseInt(prefs.work_hours_standard_day) / 60).toString())
          }
          if (prefs.work_hours_rounding) setRounding(prefs.work_hours_rounding)
          if (prefs.work_hours_lunch_minutes) setLunchMinutes(prefs.work_hours_lunch_minutes)
          if (prefs.work_hours_vacation_allowance) setVacationAllowance(prefs.work_hours_vacation_allowance)
        }
        setSettingsLoaded(true)
      })
      .catch(() => setSettingsLoaded(true))
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadPresets()
  }, [api, loadPresets])

  const handleSaveSettings = async () => {
    const hours = parseFloat(standardHours)
    if (isNaN(hours) || hours <= 0) return
    const lunch = Math.max(0, parseInt(lunchMinutes, 10) || 0)
    const parsedVacation = Number.parseInt(vacationAllowance, 10)
    const vacation = Number.isNaN(parsedVacation) ? 25 : Math.max(1, Math.min(100, parsedVacation))
    setSettingsSaving(true)
    try {
      const results = await Promise.all([
        api.setPreferences({ work_hours_standard_day: String(Math.round(hours * 60)) }),
        api.setPreferences({ work_hours_rounding: rounding }),
        api.setPreferences({ work_hours_lunch_minutes: String(lunch) }),
        api.setPreferences({ work_hours_vacation_allowance: String(vacation) }),
      ])
      if (results.some(ok => !ok)) {
        console.error('workhours: one or more settings failed to save')
      }
    } finally {
      setSettingsSaving(false)
    }
  }

  const handleAddPreset = async () => {
    const name = newPresetName.trim()
    const minutes = parseInt(newPresetMinutes, 10)
    if (!name || !minutes || minutes <= 0) return
    setPresetSaving(true)
    try {
      const ok = await api.addPreset({ name, default_minutes: minutes, icon: newPresetIcon.trim() || 'clock' })
      if (ok) {
        setNewPresetName('')
        setNewPresetMinutes('')
        setNewPresetIcon('')
        loadPresets()
      }
    } finally {
      setPresetSaving(false)
    }
  }

  const handleEditPreset = (preset: WorkDeductionPreset) => {
    setEditingPreset(preset)
    setEditName(preset.name)
    setEditMinutes(String(preset.default_minutes))
    setEditIcon(preset.icon)
  }

  const handleSavePreset = async () => {
    if (!editingPreset) return
    const name = editName.trim()
    const minutes = parseInt(editMinutes, 10)
    if (!name || !minutes || minutes <= 0) return
    setPresetSaving(true)
    try {
      const updated = await api.savePreset(editingPreset.id, {
        name,
        default_minutes: minutes,
        icon: editIcon.trim() || 'clock',
        active: editingPreset.active,
      })
      if (updated) {
        setPresets(prev => prev.map(p => (p.id === updated.id ? updated : p)))
        setEditingPreset(null)
      }
    } finally {
      setPresetSaving(false)
    }
  }

  const handleDeletePreset = async (id: number) => {
    setPresetSaving(true)
    try {
      const ok = await api.deletePreset(id)
      if (ok) loadPresets()
    } finally {
      setPresetSaving(false)
    }
  }

  const handleFlexReset = async () => {
    setSettingsSaving(true)
    try {
      const ok = await api.resetFlex()
      if (!ok) console.error('workhours: flex reset failed')
    } finally {
      setSettingsSaving(false)
    }
  }

  return (
    <div className="space-y-8">
      {/* Work settings */}
      <section className="space-y-4">
        <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          {t('workhours:workSettings')}
        </h2>
        {settingsLoaded && (
          <div className="space-y-3">
            <label className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:standardDay')}</span>
              <input
                type="number"
                value={standardHours}
                onChange={e => setStandardHours(e.target.value)}
                min="1"
                max="16"
                step="0.5"
                className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none"
              />
            </label>
            <label htmlFor="rounding-select" className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:rounding')}</span>
              <Select
                id="rounding-select"
                value={rounding}
                onChange={setRounding}
                aria-label={t('workhours:rounding')}
                options={[
                  { value: '15', label: '15' },
                  { value: '30', label: '30' },
                  { value: '60', label: '60' },
                ]}
              />
            </label>
            <label className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:lunchDuration')}</span>
              <input
                type="number"
                value={lunchMinutes}
                onChange={e => setLunchMinutes(e.target.value)}
                min="0"
                max="120"
                className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none"
              />
            </label>
            <label className="flex items-center gap-3">
              <span className="text-sm text-gray-300 w-52">{t('workhours:vacationAllowance')}</span>
              <input
                type="number"
                value={vacationAllowance}
                onChange={e => setVacationAllowance(e.target.value)}
                min="1"
                max="100"
                className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none"
              />
            </label>
            <button
              type="button"
              onClick={handleSaveSettings}
              disabled={settingsSaving}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white text-sm rounded transition-colors cursor-pointer"
            >
              {t('common:actions.save')}
            </button>
          </div>
        )}
      </section>

      {/* Flex pool */}
      <section className="space-y-3">
        <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          {t('workhours:flexPool')}
        </h2>
        <button
          type="button"
          onClick={() => setShowResetFlexConfirm(true)}
          disabled={settingsSaving}
          className="px-4 py-2 bg-gray-700 hover:bg-gray-600 disabled:opacity-40 text-white text-sm rounded transition-colors cursor-pointer"
        >
          {t('workhours:resetFlexPool')}
        </button>
      </section>

      {/* Deduction presets */}
      <section className="space-y-3">
        <h2 className="text-xs font-semibold text-gray-400 uppercase tracking-wide">
          {t('workhours:presets')}
        </h2>

        {presetsLoading ? (
          <div role="status" aria-live="polite">
            <span className="sr-only">{t('common:skeleton.loading')}</span>
            <Skeleton className="h-5 w-24" />
          </div>
        ) : presets.length === 0 ? (
          <p className="text-sm text-gray-500">{t('workhours:noPresets')}</p>
        ) : (
          <div className="space-y-2">
            {presets.map(p => {
              const icon = normalizePresetIcon(p.icon)
              return editingPreset?.id === p.id ? (
                <div key={p.id} className="bg-gray-800 rounded-lg p-3 space-y-2">
                  <div className="flex gap-2 flex-wrap">
                    <input
                      type="text"
                      value={editName}
                      onChange={e => setEditName(e.target.value)}
                      placeholder={t('workhours:presetName')}
                      aria-label={t('workhours:presetName')}
                      className="flex-1 min-w-32 bg-gray-700 text-white rounded px-2 py-1.5 text-sm border border-gray-600 focus:border-blue-500 focus:outline-none"
                    />
                    <input
                      type="number"
                      value={editMinutes}
                      onChange={e => setEditMinutes(e.target.value)}
                      placeholder={t('workhours:minutesShort')}
                      min="1"
                      aria-label={t('workhours:defaultMinutes')}
                      className="w-20 bg-gray-700 text-white rounded px-2 py-1.5 text-sm border border-gray-600 focus:border-blue-500 focus:outline-none"
                    />
                    <EmojiPickerDropdown
                      value={editIcon}
                      onChange={setEditIcon}
                      customInputId="edit-icon-custom"
                    />
                  </div>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      onClick={handleSavePreset}
                      disabled={presetSaving}
                      className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white text-sm rounded cursor-pointer"
                    >
                      {t('common:actions.save')}
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditingPreset(null)}
                      className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-white text-sm rounded cursor-pointer"
                    >
                      {t('common:actions.cancel')}
                    </button>
                  </div>
                </div>
              ) : (
                <div key={p.id} className="flex items-center gap-3 bg-gray-800/60 rounded-lg px-3 py-2">
                  {icon && <span className="text-base w-5 text-center">{icon}</span>}
                  <span className="flex-1 text-sm text-white">{p.name}</span>
                  <span className="text-xs text-gray-400 font-mono">
                    {t('workhours:minutesValue', { count: p.default_minutes })}
                  </span>
                  <button
                    type="button"
                    onClick={() => handleEditPreset(p)}
                    disabled={presetSaving}
                    className="text-xs text-gray-400 hover:text-white px-2 py-1 rounded transition-colors cursor-pointer disabled:opacity-40"
                  >
                    {t('common:actions.edit')}
                  </button>
                  <button
                    type="button"
                    onClick={() => handleDeletePreset(p.id)}
                    disabled={presetSaving}
                    className="text-gray-500 hover:text-red-400 transition-colors disabled:opacity-40 cursor-pointer"
                    aria-label={t('common:actions.delete')}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              )
            })}
          </div>
        )}

        {/* Add new preset */}
        <div className="flex gap-2 flex-wrap">
          <input
            type="text"
            value={newPresetName}
            onChange={e => setNewPresetName(e.target.value)}
            placeholder={t('workhours:presetName')}
            aria-label={t('workhours:presetName')}
            className="flex-1 min-w-32 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
          />
          <input
            type="number"
            value={newPresetMinutes}
            onChange={e => setNewPresetMinutes(e.target.value)}
            placeholder={t('workhours:minutesShort')}
            min="1"
            aria-label={t('workhours:defaultMinutes')}
            className="w-20 bg-gray-800 text-white rounded px-2 py-1.5 text-sm border border-gray-700 focus:border-blue-500 focus:outline-none placeholder-gray-500"
          />
          <EmojiPickerDropdown
            value={newPresetIcon}
            onChange={setNewPresetIcon}
            customInputId="new-icon-custom"
            buttonClassName="bg-gray-800 border-gray-700"
          />
          <button
            type="button"
            onClick={handleAddPreset}
            disabled={!newPresetName.trim() || !newPresetMinutes || presetSaving}
            className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
          >
            <Plus size={14} />
            {t('workhours:addPreset')}
          </button>
        </div>
      </section>
      <ConfirmDialog
        open={showResetFlexConfirm}
        onClose={() => setShowResetFlexConfirm(false)}
        onConfirm={handleFlexReset}
        title={t('workhours:resetFlexPool')}
        message={t('workhours:resetFlexConfirm')}
        variant="default"
      />
    </div>
  )
}
