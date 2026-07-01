import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { AIPrompt } from './types'

function AIAutomationSection() {
  const { t } = useTranslation(['settings', 'common'])
  const [aiPrompts, setAiPrompts] = useState<AIPrompt[]>([])
  const [aiPromptDrafts, setAiPromptDrafts] = useState<Record<string, string>>({})
  const [aiPromptsSaving, setAiPromptsSaving] = useState(false)
  const [aiPromptsFeedback, setAiPromptsFeedback] = useState<{ ok: boolean; message: string } | null>(null)
  const [aiPromptDefaultExpanded, setAiPromptDefaultExpanded] = useState<Record<string, boolean>>({})

  const applyAiPromptsData = (data: { prompts?: AIPrompt[] }) => {
    const prompts: AIPrompt[] = data.prompts || []
    setAiPrompts(prompts)
    const drafts: Record<string, string> = {}
    for (const p of prompts) drafts[p.key] = p.body
    setAiPromptDrafts(drafts)
  }

  const reloadAiPrompts = async () => {
    const res = await fetch('/api/settings/ai-prompts', { credentials: 'include' })
    if (!res.ok) throw new Error(`Failed to reload AI prompts (${res.status})`)
    applyAiPromptsData(await res.json())
  }

  const handleSaveAiPrompts = async () => {
    if (aiPromptsSaving) return
    setAiPromptsSaving(true)
    setAiPromptsFeedback(null)
    try {
      const dirtyKeys = aiPrompts
        .filter((p) => aiPromptDrafts[p.key] !== p.body)
        .map((p) => p.key)
      for (const key of dirtyKeys) {
        const res = await fetch(`/api/settings/ai-prompts/${key}`, {
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ body: aiPromptDrafts[key] }),
        })
        if (!res.ok) throw new Error(`Failed to save prompt "${key}"`)
      }
      await reloadAiPrompts()
      setAiPromptsFeedback({ ok: true, message: t('aiPrompts.saveSuccess') })
    } catch (err) {
      console.error('Failed to save AI prompts:', err)
      setAiPromptsFeedback({ ok: false, message: t('aiPrompts.saveError') })
    } finally {
      setAiPromptsSaving(false)
    }
  }

  const handleResetAiPrompt = async (key: string) => {
    setAiPromptsFeedback(null)
    try {
      const res = await fetch(`/api/settings/ai-prompts/${key}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(`Failed to reset prompt "${key}"`)
      await reloadAiPrompts()
      setAiPromptsFeedback({ ok: true, message: t('aiPrompts.resetSuccess') })
    } catch (err) {
      console.error('Failed to reset AI prompt:', err)
      setAiPromptsFeedback({ ok: false, message: t('aiPrompts.resetError') })
    }
  }

  // Load AI prompts on mount.
  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/settings/ai-prompts', { credentials: 'include', signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`Failed to load AI prompts (${res.status})`)
        return res.json()
      })
      .then((data) => {
        applyAiPromptsData(data)
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('Failed to load AI prompts:', err)
      })
    return () => controller.abort()
  }, [])

  return (
    <>
      <p className="text-sm text-gray-400 mb-4">{t('aiPrompts.description')}</p>

      <div className="space-y-6">
        {aiPrompts.map((prompt) => (
          <div key={prompt.key}>
            <div className="flex items-center justify-between mb-1">
              <label htmlFor={`ai-prompt-${prompt.key}`} className="text-sm font-medium text-gray-300">
                {t(`aiPrompts.key_${prompt.key}`, { defaultValue: prompt.key })}
              </label>
              <button
                type="button"
                onClick={() => handleResetAiPrompt(prompt.key)}
                disabled={prompt.is_default && (aiPromptDrafts[prompt.key] ?? prompt.body) === prompt.body}
                className="text-xs text-gray-400 hover:text-gray-200 underline cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
                aria-label={t('aiPrompts.resetAriaLabel', { key: prompt.key })}
              >
                {t('aiPrompts.resetToDefault')}
              </button>
            </div>
            <textarea
              id={`ai-prompt-${prompt.key}`}
              value={aiPromptDrafts[prompt.key] ?? prompt.body}
              onChange={(e) => setAiPromptDrafts((prev) => ({ ...prev, [prompt.key]: e.target.value }))}
              rows={4}
              placeholder={t('aiPrompts.placeholder')}
              className="w-full bg-gray-900 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white font-mono resize-y focus:outline-none focus:border-blue-500"
              aria-label={t(`aiPrompts.key_${prompt.key}`, { defaultValue: prompt.key })}
            />
            <p className="text-xs text-gray-500 mt-1">{t('aiPrompts.additionalContextHelper')}</p>
            {prompt.default_prompt && (
              <div className="mt-2">
                <button
                  type="button"
                  onClick={() => setAiPromptDefaultExpanded((prev) => ({ ...prev, [prompt.key]: !prev[prompt.key] }))}
                  className="text-xs text-gray-400 hover:text-gray-300 underline cursor-pointer"
                >
                  {aiPromptDefaultExpanded[prompt.key] ? t('aiPrompts.hideDefaultPrompt') : t('aiPrompts.viewDefaultPrompt')}
                </button>
                {aiPromptDefaultExpanded[prompt.key] && (
                  <pre className="mt-2 p-3 bg-gray-900 rounded text-xs text-gray-400 whitespace-pre-wrap font-mono border border-gray-700">
                    {prompt.default_prompt}
                  </pre>
                )}
              </div>
            )}
          </div>
        ))}
      </div>

      <div className="flex items-center gap-3 mt-4">
        <button
          type="button"
          onClick={handleSaveAiPrompts}
          disabled={aiPromptsSaving}
          className="px-4 py-2 rounded-lg bg-blue-600 text-white text-sm hover:bg-blue-500 transition-colors cursor-pointer disabled:opacity-50"
        >
          {aiPromptsSaving ? t('integrations.saving') : t('integrations.save')}
        </button>
        {aiPromptsFeedback && (
          <p className={`text-sm ${aiPromptsFeedback.ok ? 'text-green-400' : 'text-red-400'}`}>
            {aiPromptsFeedback.message}
          </p>
        )}
      </div>
    </>
  )
}

export default AIAutomationSection
