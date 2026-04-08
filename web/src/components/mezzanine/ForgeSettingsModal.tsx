import { useState, useEffect, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Save } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'
import { CollapsibleSection } from '../CollapsibleSection'

interface AnvilConfig {
  path: string
  max_smiths: number
  auto_dispatch: string
  auto_dispatch_min_priority: number
  auto_dispatch_tag: string
  auto_merge: boolean
  wicket_enabled?: boolean
  wicket_auto_dispatch?: boolean
  wicket_trusted_users?: string[]
}

interface ForgeSettings {
  max_total_smiths: number
  poll_interval: string
  smith_timeout: string
  stale_interval: string
  bellows_interval: string
  rate_limit_backoff: string
  max_ci_fix_attempts: number
  max_pipeline_iterations: number
  max_rebase_attempts: number
  max_review_attempts: number
  max_review_fix_attempts: number
  providers: string[]
  smith_providers: string[]
  claude_flags: string[]
  auto_learn_rules: boolean
  schematic_enabled: boolean
  crucible_enabled: boolean
  wicket_enabled: boolean
  copilot_daily_request_limit: number
  smelter_interval: string
}

interface NotificationConfig {
  enabled: boolean
  teams: { webhook_url: string }
  webhooks: Array<{ name: string; url: string }>
}

interface ForgeConfig {
  anvils: Record<string, AnvilConfig>
  settings: ForgeSettings
  notifications: NotificationConfig
}

interface ForgeSettingsModalProps {
  open: boolean
  onClose: () => void
  showToast: (message: string, type: 'success' | 'error') => void
}

export default function ForgeSettingsModal({ open, onClose, showToast }: ForgeSettingsModalProps) {
  const { t } = useTranslation('forgeSettings')
  const titleId = useId()

  const [config, setConfig] = useState<ForgeConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    setLoading(true)
    setError(null)

    const load = async () => {
      try {
        const res = await fetch('/api/forge/config', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (controller.signal.aborted) return
        if (!res.ok) {
          setError(t('loadError'))
          return
        }
        const data = await res.json()
        if (!controller.signal.aborted) {
          setConfig({ ...data, anvils: data.anvils ?? {} })
        }
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return
        setError(t('loadError'))
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      }
    }
    load()
    return () => controller.abort()
  }, [open, t])

  const saveConfig = async () => {
    if (!config) return
    setSaving(true)
    try {
      const res = await fetch('/api/forge/config', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      })
      if (res.ok) {
        showToast(t('saveSuccess'), 'success')
        onClose()
      } else {
        const data = await res.json().catch(() => null)
        showToast(data?.error || t('saveError'), 'error')
      }
    } catch {
      showToast(t('saveError'), 'error')
    } finally {
      setSaving(false)
    }
  }

  const updateSettings = <K extends keyof ForgeSettings>(key: K, value: ForgeSettings[K]) => {
    if (!config) return
    setConfig({ ...config, settings: { ...config.settings, [key]: value } })
  }

  const updateAnvil = (name: string, key: keyof AnvilConfig, value: AnvilConfig[keyof AnvilConfig]) => {
    if (!config) return
    setConfig({
      ...config,
      anvils: {
        ...config.anvils,
        [name]: { ...config.anvils[name], [key]: value },
      },
    })
  }

  const updateNotifications = <K extends keyof NotificationConfig>(key: K, value: NotificationConfig[K]) => {
    if (!config) return
    setConfig({ ...config, notifications: { ...config.notifications, [key]: value } })
  }

  return (
    <Dialog open={open} onClose={onClose} maxWidth="max-w-3xl" aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={t('title')} onClose={onClose} />
      <DialogBody>
        {loading && (
          <div className="flex items-center justify-center h-32 text-gray-400">
            {t('loading')}
          </div>
        )}

        {error && !config && (
          <p className="text-red-400">{error}</p>
        )}

        {config && (
          <div className="space-y-2">
            {/* Global Settings */}
            <CollapsibleSection id="modal-forge-settings" title={t('sections.settings')} defaultExpanded headingLevel="h3" titleClassName="text-base font-semibold">
              <div className="space-y-4">
                <h4 className="text-sm font-medium text-gray-400 uppercase tracking-wider">{t('settings.concurrency')}</h4>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <NumberField
                    label={t('settings.maxTotalSmiths')}
                    value={config.settings.max_total_smiths}
                    onChange={(v) => updateSettings('max_total_smiths', v)}
                  />
                  <NumberField
                    label={t('settings.copilotDailyRequestLimit')}
                    value={config.settings.copilot_daily_request_limit}
                    onChange={(v) => updateSettings('copilot_daily_request_limit', v)}
                  />
                </div>

                <h4 className="text-sm font-medium text-gray-400 uppercase tracking-wider mt-6">{t('settings.timing')}</h4>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <TextField
                    label={t('settings.pollInterval')}
                    value={config.settings.poll_interval}
                    onChange={(v) => updateSettings('poll_interval', v)}
                  />
                  <TextField
                    label={t('settings.smithTimeout')}
                    value={config.settings.smith_timeout}
                    onChange={(v) => updateSettings('smith_timeout', v)}
                  />
                  <TextField
                    label={t('settings.staleInterval')}
                    value={config.settings.stale_interval}
                    onChange={(v) => updateSettings('stale_interval', v)}
                  />
                  <TextField
                    label={t('settings.bellowsInterval')}
                    value={config.settings.bellows_interval}
                    onChange={(v) => updateSettings('bellows_interval', v)}
                  />
                  <TextField
                    label={t('settings.rateLimitBackoff')}
                    value={config.settings.rate_limit_backoff}
                    onChange={(v) => updateSettings('rate_limit_backoff', v)}
                  />
                  <TextField
                    label={t('settings.smelterInterval')}
                    value={config.settings.smelter_interval}
                    onChange={(v) => updateSettings('smelter_interval', v)}
                  />
                </div>

                <h4 className="text-sm font-medium text-gray-400 uppercase tracking-wider mt-6">{t('settings.retryLimits')}</h4>
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                  <NumberField
                    label={t('settings.maxCIFixAttempts')}
                    value={config.settings.max_ci_fix_attempts}
                    onChange={(v) => updateSettings('max_ci_fix_attempts', v)}
                  />
                  <NumberField
                    label={t('settings.maxPipelineIterations')}
                    value={config.settings.max_pipeline_iterations}
                    onChange={(v) => updateSettings('max_pipeline_iterations', v)}
                  />
                  <NumberField
                    label={t('settings.maxRebaseAttempts')}
                    value={config.settings.max_rebase_attempts}
                    onChange={(v) => updateSettings('max_rebase_attempts', v)}
                  />
                  <NumberField
                    label={t('settings.maxReviewAttempts')}
                    value={config.settings.max_review_attempts}
                    onChange={(v) => updateSettings('max_review_attempts', v)}
                  />
                  <NumberField
                    label={t('settings.maxReviewFixAttempts')}
                    value={config.settings.max_review_fix_attempts}
                    onChange={(v) => updateSettings('max_review_fix_attempts', v)}
                  />
                </div>

                <h4 className="text-sm font-medium text-gray-400 uppercase tracking-wider mt-6">{t('settings.features')}</h4>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <ToggleField
                    label={t('settings.wicketEnabled')}
                    value={config.settings.wicket_enabled}
                    onChange={(v) => updateSettings('wicket_enabled', v)}
                  />
                  <ToggleField
                    label={t('settings.crucibleEnabled')}
                    value={config.settings.crucible_enabled}
                    onChange={(v) => updateSettings('crucible_enabled', v)}
                  />
                  <ToggleField
                    label={t('settings.schematicEnabled')}
                    value={config.settings.schematic_enabled}
                    onChange={(v) => updateSettings('schematic_enabled', v)}
                  />
                  <ToggleField
                    label={t('settings.autoLearnRules')}
                    value={config.settings.auto_learn_rules}
                    onChange={(v) => updateSettings('auto_learn_rules', v)}
                  />
                </div>

                <h4 className="text-sm font-medium text-gray-400 uppercase tracking-wider mt-6">{t('settings.providers')}</h4>
                <ListField
                  label={t('settings.providersList')}
                  values={config.settings.providers || []}
                  onChange={(v) => updateSettings('providers', v)}
                />
                <ListField
                  label={t('settings.smithProvidersList')}
                  values={config.settings.smith_providers || []}
                  onChange={(v) => updateSettings('smith_providers', v)}
                />
              </div>
            </CollapsibleSection>

            {/* Anvils */}
            <CollapsibleSection id="modal-forge-anvils" title={t('sections.anvils')} defaultExpanded={false} headingLevel="h3" titleClassName="text-base font-semibold">
              <div className="space-y-6">
                {Object.keys(config.anvils).sort().map((name) => {
                  const anvil = config.anvils[name]
                  return (
                    <div key={name} className="bg-gray-900/50 rounded-lg p-4 space-y-4">
                      <h4 className="text-base font-medium text-white">{name}</h4>
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <TextField
                          label={t('anvils.path')}
                          value={anvil.path}
                          onChange={(v) => updateAnvil(name, 'path', v)}
                        />
                        <NumberField
                          label={t('anvils.maxSmiths')}
                          value={anvil.max_smiths}
                          onChange={(v) => updateAnvil(name, 'max_smiths', v)}
                        />
                        <TextField
                          label={t('anvils.autoDispatch')}
                          value={anvil.auto_dispatch}
                          onChange={(v) => updateAnvil(name, 'auto_dispatch', v)}
                        />
                        <NumberField
                          label={t('anvils.autoDispatchMinPriority')}
                          value={anvil.auto_dispatch_min_priority}
                          onChange={(v) => updateAnvil(name, 'auto_dispatch_min_priority', v)}
                        />
                        <TextField
                          label={t('anvils.autoDispatchTag')}
                          value={anvil.auto_dispatch_tag}
                          onChange={(v) => updateAnvil(name, 'auto_dispatch_tag', v)}
                        />
                      </div>
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                        <ToggleField
                          label={t('anvils.autoMerge')}
                          value={anvil.auto_merge}
                          onChange={(v) => updateAnvil(name, 'auto_merge', v)}
                        />
                        <ToggleField
                          label={t('anvils.wicketEnabled')}
                          value={anvil.wicket_enabled ?? false}
                          onChange={(v) => updateAnvil(name, 'wicket_enabled', v)}
                        />
                        <ToggleField
                          label={t('anvils.wicketAutoDispatch')}
                          value={anvil.wicket_auto_dispatch ?? false}
                          onChange={(v) => updateAnvil(name, 'wicket_auto_dispatch', v)}
                        />
                      </div>
                    </div>
                  )
                })}
              </div>
            </CollapsibleSection>

            {/* Notifications */}
            <CollapsibleSection id="modal-forge-notifications" title={t('sections.notifications')} defaultExpanded={false} headingLevel="h3" titleClassName="text-base font-semibold">
              <div className="space-y-4">
                <ToggleField
                  label={t('notifications.enabled')}
                  value={config.notifications.enabled}
                  onChange={(v) => updateNotifications('enabled', v)}
                />
                <TextField
                  label={t('notifications.teamsWebhookUrl')}
                  value={config.notifications.teams?.webhook_url || ''}
                  onChange={(v) =>
                    updateNotifications('teams', { ...config.notifications.teams, webhook_url: v })
                  }
                />
              </div>
            </CollapsibleSection>
          </div>
        )}
      </DialogBody>
      {config && (
        <DialogFooter>
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors"
          >
            {t('cancel')}
          </button>
          <button
            type="button"
            onClick={saveConfig}
            disabled={saving}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
          >
            <Save size={16} />
            {saving ? t('saving') : t('save')}
          </button>
        </DialogFooter>
      )}
    </Dialog>
  )
}

// --- Field components (same as ForgeSettingsPage) ---

function TextField({
  label,
  value,
  onChange,
}: {
  label: string
  value: string
  onChange: (v: string) => void
}) {
  return (
    <label className="block">
      <span className="text-sm text-gray-400">{label}</span>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="mt-1 block w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
      />
    </label>
  )
}

function NumberField({
  label,
  value,
  onChange,
}: {
  label: string
  value: number
  onChange: (v: number) => void
}) {
  return (
    <label className="block">
      <span className="text-sm text-gray-400">{label}</span>
      <input
        type="number"
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value, 10) || 0)}
        className="mt-1 block w-full bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
      />
    </label>
  )
}

function ToggleField({
  label,
  value,
  onChange,
}: {
  label: string
  value: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <label className="flex items-center gap-3 cursor-pointer">
      <button
        type="button"
        role="switch"
        aria-checked={value}
        aria-label={label}
        onClick={() => onChange(!value)}
        className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors cursor-pointer ${
          value ? 'bg-blue-600' : 'bg-gray-600'
        }`}
      >
        <span
          className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
            value ? 'translate-x-6' : 'translate-x-1'
          }`}
        />
      </button>
      <span className="text-sm text-gray-300">{label}</span>
    </label>
  )
}

function ListField({
  label,
  values,
  onChange,
}: {
  label: string
  values: string[]
  onChange: (v: string[]) => void
}) {
  const [draft, setDraft] = useState('')

  const addItem = () => {
    const trimmed = draft.trim()
    if (trimmed && !values.includes(trimmed)) {
      onChange([...values, trimmed])
      setDraft('')
    }
  }

  const removeItem = (index: number) => {
    onChange(values.filter((_, i) => i !== index))
  }

  return (
    <div>
      <span className="text-sm text-gray-400">{label}</span>
      <div className="mt-1 space-y-2">
        {values.map((item, i) => (
          <div key={item} className="flex items-center gap-2">
            <span className="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm">
              {item}
            </span>
            <button
              type="button"
              onClick={() => removeItem(i)}
              className="text-red-400 hover:text-red-300 text-sm px-2 py-1 cursor-pointer"
              aria-label={`Remove ${item}`}
            >
              &times;
            </button>
          </div>
        ))}
        <div className="flex gap-2">
          <input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addItem()
              }
            }}
            placeholder={label}
            className="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button
            type="button"
            onClick={addItem}
            className="px-3 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm text-white cursor-pointer"
            aria-label={`Add ${label}`}
          >
            +
          </button>
        </div>
      </div>
    </div>
  )
}
