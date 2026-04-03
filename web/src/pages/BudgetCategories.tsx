import { useState, useEffect, useCallback, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Plus, Trash2, Pencil, X, Check, ChevronLeft } from 'lucide-react'

// ── Types ────────────────────────────────────────────────────────────────────

interface Category {
  id: number
  name: string
  group_name: string
  icon: string
  color: string
  is_income: boolean
}

interface CategoryForm {
  name: string
  group_name: string
  icon: string
  color: string
  is_income: boolean
}

// ── Constants ────────────────────────────────────────────────────────────────

const GROUP_OPTIONS = ['Bolig', 'Barn', 'Fast', 'Variabel', 'Inntekt']

const DEFAULT_COLORS = [
  '#6366f1', // indigo
  '#22c55e', // green
  '#f59e0b', // amber
  '#ef4444', // red
  '#3b82f6', // blue
  '#a855f7', // purple
  '#14b8a6', // teal
  '#f97316', // orange
  '#ec4899', // pink
  '#64748b', // slate
]

function blankForm(): CategoryForm {
  return {
    name: '',
    group_name: GROUP_OPTIONS[3], // Variabel
    icon: '',
    color: DEFAULT_COLORS[0],
    is_income: false,
  }
}

function categoryToForm(c: Category): CategoryForm {
  return {
    name: c.name,
    group_name: c.group_name,
    icon: c.icon,
    color: c.color || DEFAULT_COLORS[0],
    is_income: c.is_income,
  }
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function groupCategories(categories: Category[]): [string, Category[]][] {
  const map = new Map<string, Category[]>()
  for (const c of categories) {
    const group = c.group_name || ''
    if (!map.has(group)) map.set(group, [])
    map.get(group)!.push(c)
  }
  // Sort groups: known groups first in fixed order, then custom groups
  const knownOrder = [...GROUP_OPTIONS, '']
  const groups = [...map.entries()].sort(([a], [b]) => {
    const ai = knownOrder.indexOf(a)
    const bi = knownOrder.indexOf(b)
    const ai2 = ai === -1 ? knownOrder.length : ai
    const bi2 = bi === -1 ? knownOrder.length : bi
    if (ai2 !== bi2) return ai2 - bi2
    return a.localeCompare(b)
  })
  return groups
}

// ── Component ────────────────────────────────────────────────────────────────

export default function BudgetCategories() {
  const { t } = useTranslation('budget')

  const [categories, setCategories] = useState<Category[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Form state: null = hidden, 0 = new, >0 = editing category id
  const [editingId, setEditingId] = useState<number | null>(null)
  const [form, setForm] = useState<CategoryForm | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [customGroup, setCustomGroup] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/api/budget/categories', { credentials: 'include' })
      if (!res.ok) throw new Error('load failed')
      const data = await res.json()
      setCategories(data.categories ?? [])
    } catch {
      setError(t('categories.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    load()
  }, [load])

  function openCreate() {
    const f = blankForm()
    setEditingId(0)
    setForm(f)
    setFormError(null)
    setCustomGroup(!GROUP_OPTIONS.includes(f.group_name))
  }

  function openEdit(c: Category) {
    setEditingId(c.id)
    setForm(categoryToForm(c))
    setFormError(null)
    setCustomGroup(!GROUP_OPTIONS.includes(c.group_name))
  }

  function cancelEdit() {
    setEditingId(null)
    setForm(null)
    setFormError(null)
    setCustomGroup(false)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!form) return
    if (!form.name.trim()) {
      setFormError(t('categories.errors.nameRequired'))
      return
    }
    setSaving(true)
    setFormError(null)
    try {
      const body = {
        name: form.name.trim(),
        group_name: form.group_name,
        icon: form.icon,
        color: form.color,
        is_income: form.is_income,
      }
      const isNew = editingId === 0
      const res = await fetch(
        isNew ? '/api/budget/categories' : `/api/budget/categories/${editingId}`,
        {
          method: isNew ? 'POST' : 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        }
      )
      if (!res.ok) throw new Error('save failed')
      cancelEdit()
      await load()
    } catch {
      setFormError(t('categories.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: number) {
    setError(null)
    try {
      const res = await fetch(`/api/budget/categories/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('delete failed')
      setCategories(prev => prev.filter(c => c.id !== id))
    } catch {
      setError(t('categories.errors.deleteFailed'))
    }
  }

  // ── Render ──────────────────────────────────────────────────────────────────

  if (loading) {
    return <div className="p-6 text-gray-400">{t('loading')}</div>
  }

  const grouped = groupCategories(categories)

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link
          to="/budget"
          className="text-gray-400 hover:text-white transition-colors"
          aria-label={t('import.backToBudget')}
        >
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-xl font-semibold text-white">{t('categories.title')}</h1>
        <button
          type="button"
          onClick={openCreate}
          className="ml-auto flex items-center gap-1.5 bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
        >
          <Plus size={16} />
          {t('categories.add')}
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Create / Edit form */}
      {editingId !== null && form !== null && (
        <form
          onSubmit={handleSubmit}
          className="mb-6 p-4 bg-gray-800 rounded-xl border border-gray-700 space-y-3"
        >
          <h2 className="text-sm font-medium text-gray-200">
            {editingId === 0 ? t('categories.newCategory') : t('categories.editCategory')}
          </h2>

          {formError && (
            <p className="text-red-400 text-sm">{formError}</p>
          )}

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {/* Name */}
            <div className="sm:col-span-2">
              <label className="block text-xs text-gray-400 mb-1" htmlFor="cat-name">
                {t('categories.name')}
              </label>
              <input
                id="cat-name"
                type="text"
                value={form.name}
                onChange={e => setForm({ ...form, name: e.target.value })}
                placeholder={t('categories.namePlaceholder')}
                required
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            {/* Group */}
            <div>
              <label className="block text-xs text-gray-400 mb-1" htmlFor="cat-group">
                {t('categories.group')}
              </label>
              {customGroup ? (
                <div className="flex gap-2">
                  <input
                    id="cat-group"
                    type="text"
                    value={form.group_name}
                    onChange={e => setForm({ ...form, group_name: e.target.value })}
                    placeholder={t('categories.groupPlaceholder')}
                    className="flex-1 bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
                  />
                  <button
                    type="button"
                    onClick={() => { setCustomGroup(false); setForm({ ...form, group_name: GROUP_OPTIONS[3] }) }}
                    className="px-2 py-1 text-xs text-gray-400 hover:text-white bg-gray-700 rounded-lg border border-gray-600"
                  >
                    {t('categories.usePreset')}
                  </button>
                </div>
              ) : (
                <div className="flex gap-2">
                  <select
                    id="cat-group"
                    value={form.group_name}
                    onChange={e => setForm({ ...form, group_name: e.target.value })}
                    className="flex-1 bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
                  >
                    {GROUP_OPTIONS.map(g => (
                      <option key={g} value={g}>{g}</option>
                    ))}
                  </select>
                  <button
                    type="button"
                    onClick={() => { setCustomGroup(true); setForm({ ...form, group_name: '' }) }}
                    className="px-2 py-1 text-xs text-gray-400 hover:text-white bg-gray-700 rounded-lg border border-gray-600"
                  >
                    {t('categories.custom')}
                  </button>
                </div>
              )}
            </div>

            {/* Icon */}
            <div>
              <label className="block text-xs text-gray-400 mb-1" htmlFor="cat-icon">
                {t('categories.icon')}
              </label>
              <input
                id="cat-icon"
                type="text"
                value={form.icon}
                onChange={e => setForm({ ...form, icon: e.target.value })}
                placeholder={t('categories.iconPlaceholder')}
                className="w-full bg-gray-700 text-white text-sm rounded-lg px-3 py-2 border border-gray-600 focus:border-indigo-500 focus:outline-none"
              />
            </div>

            {/* Color */}
            <div>
              <label className="block text-xs text-gray-400 mb-1">
                {t('categories.color')}
              </label>
              <div className="flex items-center gap-2 flex-wrap">
                {DEFAULT_COLORS.map(c => (
                  <button
                    key={c}
                    type="button"
                    onClick={() => setForm({ ...form, color: c })}
                    className="w-6 h-6 rounded-full border-2 transition-all"
                    style={{
                      backgroundColor: c,
                      borderColor: form.color === c ? 'white' : 'transparent',
                    }}
                    aria-label={c}
                  />
                ))}
                <input
                  type="color"
                  value={form.color}
                  onChange={e => setForm({ ...form, color: e.target.value })}
                  className="w-6 h-6 rounded cursor-pointer bg-transparent border-0"
                  title={t('categories.customColor')}
                />
              </div>
            </div>

            {/* Is income */}
            <div className="sm:col-span-2 flex items-center gap-2">
              <input
                id="cat-income"
                type="checkbox"
                checked={form.is_income}
                onChange={e => setForm({ ...form, is_income: e.target.checked })}
                className="w-4 h-4 rounded"
              />
              <label htmlFor="cat-income" className="text-sm text-gray-300">
                {t('categories.isIncome')}
              </label>
            </div>
          </div>

          {/* Form actions */}
          <div className="flex gap-2 pt-1">
            <button
              type="submit"
              disabled={saving}
              className="flex items-center gap-1.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
            >
              <Check size={14} />
              {saving ? t('quickAdd.saving') : t('categories.save')}
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              className="flex items-center gap-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm px-3 py-1.5 rounded-lg transition-colors"
            >
              <X size={14} />
              {t('quickAdd.cancel')}
            </button>
          </div>
        </form>
      )}

      {/* Category list */}
      {categories.length === 0 ? (
        <div className="text-center py-12 text-gray-500 text-sm">
          {t('categories.empty')}
        </div>
      ) : (
        <div className="space-y-6">
          {grouped.map(([group, cats]) => (
            <div key={group}>
              <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-2">
                {group || t('categories.ungrouped')}
              </h2>
              <ul className="space-y-2">
                {cats.map(cat => (
                  <li
                    key={cat.id}
                    className="flex items-center gap-3 p-3 bg-gray-800 rounded-xl border border-gray-700"
                  >
                    {/* Color swatch + icon */}
                    <div
                      className="w-8 h-8 rounded-full flex items-center justify-center text-base flex-shrink-0"
                      style={{ backgroundColor: cat.color || '#6366f1' }}
                    >
                      {cat.icon || ''}
                    </div>

                    {/* Info */}
                    <div className="flex-1 min-w-0">
                      <span className="text-sm font-medium text-white">{cat.name}</span>
                      {cat.is_income && (
                        <span className="ml-2 text-xs text-green-400 bg-green-400/10 px-1.5 py-0.5 rounded">
                          {t('categories.incomeLabel')}
                        </span>
                      )}
                    </div>

                    {/* Actions */}
                    <div className="flex gap-1 flex-shrink-0">
                      <button
                        type="button"
                        onClick={() => openEdit(cat)}
                        className="p-1.5 text-gray-400 hover:text-white rounded transition-colors"
                        aria-label={t('categories.edit')}
                      >
                        <Pencil size={15} />
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDelete(cat.id)}
                        className="p-1.5 text-gray-400 hover:text-red-400 rounded transition-colors"
                        aria-label={t('categories.delete')}
                      >
                        <Trash2 size={15} />
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
