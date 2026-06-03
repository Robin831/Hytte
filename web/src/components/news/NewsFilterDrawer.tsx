import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X, Plus, Trash2 } from 'lucide-react'
import type { HiddenArticle, NewsSettings, NewsSource } from '../../hooks/useNews'

interface NewsFilterDrawerProps {
  open: boolean
  onClose: () => void
  hidden: HiddenArticle[]
  onSaved: () => void
}

function reasonParts(reason: string): { kind: string; value: string } {
  const i = reason.indexOf(':')
  if (i < 0) return { kind: reason, value: '' }
  return { kind: reason.slice(0, i), value: reason.slice(i + 1) }
}

// TagInput renders a removable chip list with an add-on-Enter field.
function TagInput({
  values, onChange, placeholder,
}: { values: string[]; onChange: (v: string[]) => void; placeholder: string }) {
  const [draft, setDraft] = useState('')
  const add = () => {
    const v = draft.trim().toLowerCase()
    if (v && !values.includes(v)) onChange([...values, v])
    setDraft('')
  }
  return (
    <div className="rounded-lg border border-gray-700 bg-gray-900 p-2">
      <div className="flex flex-wrap gap-1.5">
        {values.map(v => (
          <span key={v} className="inline-flex items-center gap-1 rounded-full bg-gray-800 px-2 py-0.5 text-xs text-gray-200">
            {v}
            <button type="button" onClick={() => onChange(values.filter(x => x !== v))} className="text-gray-500 hover:text-red-400 cursor-pointer">
              <X size={12} />
            </button>
          </span>
        ))}
      </div>
      <input
        value={draft}
        onChange={e => setDraft(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add() } }}
        onBlur={add}
        placeholder={placeholder}
        className="mt-2 w-full bg-transparent text-sm text-gray-100 placeholder-gray-600 focus:outline-none"
      />
    </div>
  )
}

export default function NewsFilterDrawer({ open, onClose, hidden, onSaved }: NewsFilterDrawerProps) {
  const { t } = useTranslation('news')
  const [settings, setSettings] = useState<NewsSettings | null>(null)
  const [saving, setSaving] = useState(false)
  const [newSrc, setNewSrc] = useState({ name: '', url: '' })

  useEffect(() => {
    if (!open) return
    let cancelled = false
    fetch('/api/news/settings', { credentials: 'include' })
      .then(res => (res.ok ? res.json() : null))
      .then((data: NewsSettings | null) => { if (!cancelled && data) setSettings(data) })
      .catch(() => { /* drawer just stays empty */ })
    return () => { cancelled = true }
  }, [open])

  const update = (fields: Partial<NewsSettings>) => setSettings(s => (s ? { ...s, ...fields } : s))

  const save = async () => {
    if (!settings) return
    setSaving(true)
    try {
      const res = await fetch('/api/news/settings', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      })
      if (res.ok) onSaved()
    } finally {
      setSaving(false)
    }
  }

  const addSource = () => {
    if (!settings) return
    const name = newSrc.name.trim()
    const url = newSrc.url.trim()
    if (!name || !url) return
    const key = name.toLowerCase().replace(/[^a-z0-9]+/g, '-')
    const src: NewsSource = { key, name, feed_url: url, color: '#475569', enabled: true }
    update({ sources: [...settings.sources, src] })
    setNewSrc({ name: '', url: '' })
  }

  return (
    <>
      {open && <div className="fixed inset-0 z-40 bg-black/60" onClick={onClose} />}
      <aside
        className={`fixed inset-y-0 right-0 z-50 w-full max-w-md transform overflow-y-auto bg-gray-950 border-l border-gray-800 transition-transform duration-200 ${
          open ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        <div className="sticky top-0 flex items-center justify-between border-b border-gray-800 bg-gray-950 px-4 h-14">
          <h2 className="text-base font-semibold text-white">{t('filters.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-white cursor-pointer"><X size={20} /></button>
        </div>

        {settings && (
          <div className="space-y-6 p-4">
            {/* keywords */}
            <section>
              <h3 className="text-sm font-medium text-gray-200">{t('filters.keywords')}</h3>
              <p className="mb-2 text-xs text-gray-500">{t('filters.keywordsHint')}</p>
              <TagInput
                values={settings.block_keywords}
                onChange={v => update({ block_keywords: v })}
                placeholder={t('filters.addPlaceholder')}
              />
            </section>

            {/* categories */}
            <section>
              <h3 className="mb-2 text-sm font-medium text-gray-200">{t('filters.categories')}</h3>
              <TagInput
                values={settings.block_categories}
                onChange={v => update({ block_categories: v })}
                placeholder={t('filters.categoriesPlaceholder')}
              />
            </section>

            {/* toggles */}
            <section className="space-y-3">
              <label className="flex items-start gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={settings.hide_paywalled}
                  onChange={e => update({ hide_paywalled: e.target.checked })}
                  className="mt-0.5 h-4 w-4 accent-blue-500"
                />
                <span className="text-sm">
                  <span className="text-gray-200">{t('filters.paywall')}</span>
                  <span className="block text-xs text-gray-500">{t('filters.paywallHint')}</span>
                </span>
              </label>

              <label className="flex items-start gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={settings.llm_scoring}
                  onChange={e => update({ llm_scoring: e.target.checked })}
                  className="mt-0.5 h-4 w-4 accent-blue-500"
                />
                <span className="text-sm">
                  <span className="text-gray-200">{t('filters.ranking')}</span>
                  <span className="block text-xs text-gray-500">{t('filters.rankingHint')}</span>
                </span>
              </label>
            </section>

            {/* threshold */}
            {settings.llm_scoring && (
              <section>
                <label className="text-sm font-medium text-gray-200">
                  {t('filters.threshold')}: <span className="text-blue-400">{settings.score_threshold}</span>
                </label>
                <p className="mb-1 text-xs text-gray-500">{t('filters.thresholdHint')}</p>
                <input
                  type="range"
                  min={0}
                  max={100}
                  step={5}
                  value={settings.score_threshold}
                  onChange={e => update({ score_threshold: Number(e.target.value) })}
                  className="w-full accent-blue-500"
                />
              </section>
            )}

            {/* sources */}
            <section>
              <h3 className="mb-2 text-sm font-medium text-gray-200">{t('filters.sources')}</h3>
              <div className="space-y-1.5">
                {settings.sources.map((src, i) => (
                  <div key={src.key} className="flex items-center gap-2 rounded-lg border border-gray-800 px-2 py-1.5">
                    <input
                      type="checkbox"
                      checked={src.enabled}
                      onChange={e => {
                        const next = [...settings.sources]
                        next[i] = { ...src, enabled: e.target.checked }
                        update({ sources: next })
                      }}
                      className="h-4 w-4 accent-blue-500"
                    />
                    <span className="inline-block h-3 w-3 rounded-full" style={{ backgroundColor: src.color }} />
                    <span className="flex-1 truncate text-sm text-gray-200">{src.name}</span>
                    <button
                      type="button"
                      onClick={() => update({ sources: settings.sources.filter((_, j) => j !== i) })}
                      className="text-gray-600 hover:text-red-400 cursor-pointer"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>
                ))}
              </div>
              <div className="mt-2 flex gap-2">
                <input
                  value={newSrc.name}
                  onChange={e => setNewSrc(s => ({ ...s, name: e.target.value }))}
                  placeholder={t('filters.sourceName')}
                  className="w-28 rounded-lg border border-gray-700 bg-gray-900 px-2 py-1.5 text-sm focus:outline-none focus:border-blue-500"
                />
                <input
                  value={newSrc.url}
                  onChange={e => setNewSrc(s => ({ ...s, url: e.target.value }))}
                  placeholder={t('filters.sourceUrl')}
                  className="flex-1 rounded-lg border border-gray-700 bg-gray-900 px-2 py-1.5 text-sm focus:outline-none focus:border-blue-500"
                />
                <button
                  type="button"
                  onClick={addSource}
                  className="rounded-lg bg-gray-800 px-2 text-gray-300 hover:bg-gray-700 cursor-pointer"
                  title={t('filters.addSource')}
                >
                  <Plus size={16} />
                </button>
              </div>
            </section>

            {/* why filtered */}
            <section>
              <h3 className="mb-2 text-sm font-medium text-gray-200">
                {t('hidden.title')}{' '}
                <span className="text-gray-500">({t('hidden.count', { count: hidden.length })})</span>
              </h3>
              {hidden.length === 0 ? (
                <p className="text-xs text-gray-500">{t('hidden.none')}</p>
              ) : (
                <ul className="space-y-1.5 max-h-64 overflow-y-auto pr-1">
                  {hidden.map(a => {
                    const { kind, value } = reasonParts(a.reason)
                    const label = kind === 'paywall'
                      ? t('hidden.reason.paywall')
                      : t(`hidden.reason.${kind}` as 'hidden.reason.keyword', { value })
                    return (
                      <li key={a.id} className="flex items-start gap-2 text-xs">
                        <span className="mt-0.5 shrink-0 rounded bg-gray-800 px-1.5 py-0.5 text-gray-400">{label}</span>
                        <span className="text-gray-400 line-clamp-1">{a.title}</span>
                      </li>
                    )
                  })}
                </ul>
              )}
            </section>

            <button
              type="button"
              onClick={save}
              disabled={saving}
              className="sticky bottom-0 w-full rounded-lg bg-blue-600 py-2.5 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50 cursor-pointer"
            >
              {saving ? t('filters.saving') : t('filters.save')}
            </button>
          </div>
        )}
      </aside>
    </>
  )
}
