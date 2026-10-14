import { memo } from 'react'
import { useTranslation } from 'react-i18next'
import type { ModelSelectProps } from './types'

// MODEL_OPTIONS is a curated subset of the models the backend allows
// (internal/chat/handlers.go SupportedModels). The backend may accept
// additional model IDs (e.g. older Opus variants) that we intentionally
// omit from the UI dropdown.
const MODEL_OPTIONS = ['claude-fable-5', 'claude-opus-5-5', 'claude-sonnet-4-6', 'claude-haiku-4-5'] as const
export const DEFAULT_MODEL = 'claude-sonnet-4-6'

// modelLabelKey maps a model ID to its i18n label key by family so any
// supported variant (e.g. an older Opus) still shows a friendly name.
function modelLabelKey(model: string): 'models.fable' | 'models.opus' | 'models.sonnet' | 'models.haiku' | '' {
  if (model.startsWith('claude-fable')) return 'models.fable'
  if (model.startsWith('claude-opus')) return 'models.opus'
  if (model.startsWith('claude-sonnet')) return 'models.sonnet'
  if (model.startsWith('claude-haiku')) return 'models.haiku'
  return ''
}

// modelVersionSuffix extracts the version from a model ID ("claude-opus-4-8"
// → "4.8", "claude-opus-5" → "5") so two variants of the same family (a
// conversation pinned to an older Opus alongside the current one) render
// distinguishable labels.
function modelVersionSuffix(model: string): string {
  const m = model.match(/^claude-[a-z]+-(\d+(?:-\d+)*)$/)
  return m ? m[1].replace(/-/g, '.') : ''
}

// Options for the header model dropdown. If the current selection isn't one
// of the fixed options (e.g. an existing conversation on an older variant),
// prepend it so the select still reflects the real value.
function modelOptionsFor(current: string): string[] {
  return MODEL_OPTIONS.includes(current as (typeof MODEL_OPTIONS)[number])
    ? [...MODEL_OPTIONS]
    : [current, ...MODEL_OPTIONS]
}

function ModelSelect({ value, disabled, onChange }: ModelSelectProps) {
  const { t } = useTranslation('chat')

  const modelLabel = (model: string): string => {
    const key = modelLabelKey(model)
    if (!key) return model
    const suffix = modelVersionSuffix(model)
    return suffix ? `${t(key)} ${suffix}` : t(key)
  }

  return (
    <select
      value={value}
      onChange={e => onChange(e.target.value)}
      disabled={disabled}
      aria-label={t('header.modelLabel')}
      title={t('header.modelLabel')}
      className="shrink-0 bg-gray-800 border border-gray-600 rounded-lg px-2 py-1 text-xs text-gray-200 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
    >
      {modelOptionsFor(value).map(m => (
        <option key={m} value={m}>
          {modelLabel(m)}
        </option>
      ))}
    </select>
  )
}

export default memo(ModelSelect)
