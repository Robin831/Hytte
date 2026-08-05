import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Pencil, Trash2, Ruler, Settings2, X, Baby, ShoppingBag } from 'lucide-react'
import { useAuth } from '../auth'
import { api, messageFor } from './wardrobeApi'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../components/ui/dialog'
import { Tabs, TabList, TabTrigger, TabPanel } from '../components/ui/tabs'
import { Select, type SelectOption } from '../components/ui/select'
import ConfirmDialog from '../components/ConfirmDialog'

interface Measurement {
  id: number
  kid_id: number
  measured_at: string
  height_cm: number
  foot_length_mm: number
  weight_kg: number
  note: string
}

interface Kid {
  id: number
  name: string
  birthdate: string
  avatar_emoji: string
  latest_measurement?: Measurement
  clothing?: { current_size: number; buy_size: number }
  shoe?: { current_eu: number; buy_eu: number }
  height_rate_cm_per_month?: number
  foot_rate_mm_per_month?: number
}

interface Category {
  id: number
  name: string
  icon: string
  size_system: 'clothing' | 'shoe' | 'none'
  target_qty: number
  sort_order: number
}

interface Item {
  id: number
  kid_id: number
  category_id: number
  name: string
  size_label: string
  quantity: number
  condition: 'new' | 'good' | 'worn'
  status: 'active' | 'too_small' | 'stored'
  location: 'home' | 'kindergarten' | 'school' | 'cabin' | 'other'
  season: 'all' | 'summer' | 'winter'
  notes: string
}

interface NeedEntry {
  category_id: number
  category_name: string
  category_icon: string
  have: number
  target: number
  recommended_size: string
}

interface TooSmallEntry {
  item: Item
  recommended_size: string
}

interface ItemForm {
  id?: number
  kid_id: number
  category_id: string
  name: string
  size_label: string
  quantity: string
  condition: string
  status: string
  location: string
  season: string
  notes: string
}

const emptyItemForm = (kidId: number): ItemForm => ({
  kid_id: kidId,
  category_id: '',
  name: '',
  size_label: '',
  quantity: '1',
  condition: 'good',
  status: 'active',
  location: 'home',
  season: 'all',
  notes: '',
})

function today(): string {
  return new Date().toISOString().slice(0, 10)
}

// FormError renders a validation message inside the form that produced it.
function FormError({ message, className }: { message: string; className?: string }) {
  if (!message) return null
  return <p role="alert" className={`text-sm text-red-400${className ? ` ${className}` : ''}`}>{message}</p>
}

export default function WardrobePage() {
  const { t, i18n } = useTranslation(['wardrobe', 'common'])
  const { user } = useAuth()

  const [kids, setKids] = useState<Kid[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [selectedKidId, setSelectedKidId] = useState<number | null>(null)
  const [items, setItems] = useState<Item[]>([])
  const [measurements, setMeasurements] = useState<Measurement[]>([])
  const [needs, setNeeds] = useState<NeedEntry[]>([])
  const [tooSmall, setTooSmall] = useState<TooSmallEntry[]>([])
  const [tab, setTab] = useState('inventory')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const [kidDialog, setKidDialog] = useState<{ id?: number; name: string; birthdate: string; emoji: string } | null>(null)
  const [kidError, setKidError] = useState('')
  const [itemDialog, setItemDialog] = useState<ItemForm | null>(null)
  const [itemError, setItemError] = useState('')
  const [categoriesOpen, setCategoriesOpen] = useState(false)
  const [confirm, setConfirm] = useState<{ message: string; action: () => void } | null>(null)
  const [saving, setSaving] = useState(false)

  // Opening or closing a dialog clears whatever error the last attempt left behind.
  const openKidDialog = (form: { id?: number; name: string; birthdate: string; emoji: string } | null) => {
    setKidError('')
    setKidDialog(form)
  }

  const openItemDialog = (form: ItemForm | null) => {
    setItemError('')
    setItemDialog(form)
  }

  const selectedKid = kids.find(k => k.id === selectedKidId) ?? null

  const loadKids = useCallback(async (signal?: AbortSignal) => {
    const res = await api('/kids', { signal })
    const data = await res.json()
    return data.kids as Kid[]
  }, [])

  const loadCategories = useCallback(async (signal?: AbortSignal) => {
    const res = await api('/categories', { signal })
    const data = await res.json()
    return data.categories as Category[]
  }, [])

  // Initial load: kids + categories (first categories fetch seeds the defaults).
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    ;(async () => {
      try {
        const [fetchedKids, fetchedCats] = await Promise.all([
          loadKids(controller.signal),
          loadCategories(controller.signal),
        ])
        if (controller.signal.aborted) return
        setKids(fetchedKids)
        setCategories(fetchedCats)
        setSelectedKidId(prev => prev ?? fetchedKids[0]?.id ?? null)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.failedToLoad'))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [user, loadKids, loadCategories, t])

  // Per-kid data.
  const refreshKidData = useCallback(async (kidId: number, signal?: AbortSignal) => {
    const [itemsRes, measRes, needsRes] = await Promise.all([
      api(`/items?kid_id=${kidId}`, { signal }),
      api(`/kids/${kidId}/measurements`, { signal }),
      api(`/needs?kid_id=${kidId}`, { signal }),
    ])
    const [itemsData, measData, needsData] = await Promise.all([itemsRes.json(), measRes.json(), needsRes.json()])
    if (signal?.aborted) return
    setItems(itemsData.items)
    setMeasurements(measData.measurements)
    setNeeds(needsData.needs)
    setTooSmall(needsData.too_small)
  }, [])

  useEffect(() => {
    if (!user || selectedKidId === null) return
    const controller = new AbortController()
    ;(async () => {
      try {
        await refreshKidData(selectedKidId, controller.signal)
      } catch {
        if (!controller.signal.aborted) setError(t('errors.failedToLoad'))
      }
    })()
    return () => controller.abort()
  }, [user, selectedKidId, refreshKidData, t])

  const refreshAll = useCallback(async () => {
    try {
      const fetchedKids = await loadKids()
      setKids(fetchedKids)
      if (selectedKidId !== null && fetchedKids.some(k => k.id === selectedKidId)) {
        await refreshKidData(selectedKidId)
      } else {
        setSelectedKidId(fetchedKids[0]?.id ?? null)
      }
    } catch {
      setError(t('errors.failedToLoad'))
    }
  }, [loadKids, refreshKidData, selectedKidId, t])

  // --- Kid CRUD ---

  const saveKid = async () => {
    if (!kidDialog || saving) return
    setSaving(true)
    setKidError('')
    try {
      const body = JSON.stringify({
        name: kidDialog.name.trim(),
        birthdate: kidDialog.birthdate,
        avatar_emoji: kidDialog.emoji,
      })
      if (kidDialog.id) {
        await api(`/kids/${kidDialog.id}`, { method: 'PATCH', body })
      } else {
        const res = await api('/kids', { method: 'POST', body })
        const data = await res.json()
        setSelectedKidId(data.kid.id as number)
      }
      openKidDialog(null)
      await refreshAll()
    } catch (e) {
      // Keep the dialog open with the entered values so they can be corrected.
      setKidError(messageFor(e, t, 'errors.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  const deleteKid = (kid: Kid) => {
    setConfirm({
      message: t('deleteKidConfirm', { name: kid.name }),
      action: async () => {
        setConfirm(null)
        try {
          await api(`/kids/${kid.id}`, { method: 'DELETE' })
          setSelectedKidId(null)
          await refreshAll()
        } catch (e) {
          setError(messageFor(e, t, 'errors.failedToDelete'))
        }
      },
    })
  }

  // --- Item CRUD ---

  const saveItem = async () => {
    if (!itemDialog || saving) return
    setSaving(true)
    setItemError('')
    try {
      const body = JSON.stringify({
        kid_id: itemDialog.kid_id,
        category_id: Number(itemDialog.category_id),
        name: itemDialog.name.trim(),
        size_label: itemDialog.size_label.trim(),
        quantity: Math.max(1, Number(itemDialog.quantity) || 1),
        condition: itemDialog.condition,
        status: itemDialog.status,
        location: itemDialog.location,
        season: itemDialog.season,
        notes: itemDialog.notes.trim(),
      })
      if (itemDialog.id) {
        await api(`/items/${itemDialog.id}`, { method: 'PATCH', body })
      } else {
        await api('/items', { method: 'POST', body })
      }
      openItemDialog(null)
      if (selectedKidId !== null) await refreshKidData(selectedKidId)
    } catch (e) {
      // Keep the dialog open with the entered values so they can be corrected.
      setItemError(messageFor(e, t, 'errors.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  const updateItemStatus = async (item: Item, status: Item['status']) => {
    setError('')
    try {
      await api(`/items/${item.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ ...item, status }),
      })
      if (selectedKidId !== null) await refreshKidData(selectedKidId)
    } catch (e) {
      setError(messageFor(e, t, 'errors.failedToSave'))
    }
  }

  const deleteItem = (item: Item) => {
    setConfirm({
      message: t('deleteItemConfirm', { name: item.name }),
      action: async () => {
        setConfirm(null)
        try {
          await api(`/items/${item.id}`, { method: 'DELETE' })
          if (selectedKidId !== null) await refreshKidData(selectedKidId)
        } catch (e) {
          setError(messageFor(e, t, 'errors.failedToDelete'))
        }
      },
    })
  }

  // --- Derived ---

  const categoryById = useMemo(() => new Map(categories.map(c => [c.id, c])), [categories])

  const recommendedSizeFor = useCallback((categoryId: number): string => {
    const cat = categoryById.get(categoryId)
    const kid = selectedKid
    if (!cat || !kid) return ''
    if (cat.size_system === 'clothing' && kid.clothing) return String(kid.clothing.buy_size)
    if (cat.size_system === 'shoe' && kid.shoe) return `EU ${kid.shoe.buy_eu}`
    return ''
  }, [categoryById, selectedKid])

  const dateFmt = useMemo(() => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }), [i18n.language])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64" role="status">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500" />
      </div>
    )
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">{t('title')}</h1>
        <button
          onClick={() => openKidDialog({ name: '', birthdate: '', emoji: '🧒' })}
          className="flex items-center gap-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg px-3 py-2 text-sm cursor-pointer transition-colors"
        >
          <Plus size={16} />
          {t('addKid')}
        </button>
      </div>

      {error && (
        <div role="alert" className="mb-4 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-200 text-sm flex items-center justify-between">
          <span>{error}</span>
          <button onClick={() => setError('')} className="ml-2 text-red-400 hover:text-red-200 cursor-pointer" aria-label={t('common:actions.close')}>
            <X size={16} />
          </button>
        </div>
      )}

      {kids.length === 0 ? (
        <div className="text-center py-16 text-gray-500">
          <Baby size={40} className="mx-auto mb-3 opacity-60" />
          <p className="text-lg">{t('empty')}</p>
          <p className="text-sm mt-1">{t('emptyHint')}</p>
        </div>
      ) : (
        <>
          {/* Kid selector */}
          <div className="flex flex-wrap gap-2 mb-4" role="tablist" aria-label={t('kidSelector')}>
            {kids.map(kid => (
              <button
                key={kid.id}
                role="tab"
                aria-selected={kid.id === selectedKidId}
                onClick={() => setSelectedKidId(kid.id)}
                className={`flex items-center gap-2 rounded-full px-4 py-2 text-sm cursor-pointer transition-colors border ${
                  kid.id === selectedKidId
                    ? 'bg-blue-600 border-blue-500 text-white'
                    : 'bg-gray-800 border-gray-700 text-gray-300 hover:bg-gray-700'
                }`}
              >
                <span aria-hidden="true">{kid.avatar_emoji || '🧒'}</span>
                {kid.name}
              </button>
            ))}
          </div>

          {selectedKid && (
            <>
              <KidStatsCard
                kid={selectedKid}
                dateFmt={dateFmt}
                onEdit={() => openKidDialog({
                  id: selectedKid.id,
                  name: selectedKid.name,
                  birthdate: selectedKid.birthdate,
                  emoji: selectedKid.avatar_emoji,
                })}
                onDelete={() => deleteKid(selectedKid)}
              />

              <Tabs value={tab} onChange={setTab} className="mt-6">
                <TabList aria-label={t('title')} className="mb-4">
                  <TabTrigger value="inventory">{t('tabs.inventory')}</TabTrigger>
                  <TabTrigger value="measurements">{t('tabs.measurements')}</TabTrigger>
                  <TabTrigger value="needs">
                    {t('tabs.needs')}
                    {needs.length + tooSmall.length > 0 && (
                      <span className="ml-1.5 inline-flex items-center justify-center bg-amber-600 text-white text-xs rounded-full min-w-5 h-5 px-1">
                        {needs.length + tooSmall.length}
                      </span>
                    )}
                  </TabTrigger>
                </TabList>

                <TabPanel value="inventory">
                  <InventoryTab
                    items={items}
                    categories={categories}
                    onAdd={() => selectedKidId !== null && openItemDialog(emptyItemForm(selectedKidId))}
                    onEdit={item => openItemDialog({
                      id: item.id,
                      kid_id: item.kid_id,
                      category_id: String(item.category_id),
                      name: item.name,
                      size_label: item.size_label,
                      quantity: String(item.quantity),
                      condition: item.condition,
                      status: item.status,
                      location: item.location,
                      season: item.season,
                      notes: item.notes,
                    })}
                    onStatus={updateItemStatus}
                    onDelete={deleteItem}
                    onManageCategories={() => setCategoriesOpen(true)}
                  />
                </TabPanel>

                <TabPanel value="measurements">
                  <MeasurementsTab
                    kid={selectedKid}
                    measurements={measurements}
                    dateFmt={dateFmt}
                    onChanged={refreshAll}
                  />
                </TabPanel>

                <TabPanel value="needs">
                  <NeedsTab needs={needs} tooSmall={tooSmall} categoryById={categoryById} />
                </TabPanel>
              </Tabs>
            </>
          )}
        </>
      )}

      {/* Kid add/edit dialog */}
      <Dialog open={kidDialog !== null} onClose={() => openKidDialog(null)} aria-labelledby="kid-dialog-title">
        {kidDialog && (
          <>
            <DialogHeader id="kid-dialog-title" title={kidDialog.id ? t('editKid') : t('addKid')} onClose={() => openKidDialog(null)} />
            <DialogBody className="space-y-4">
              <FormError message={kidError} />
              <label className="block">
                <span className="block text-sm text-gray-300 mb-1">{t('kidForm.name')}</span>
                <input
                  type="text"
                  value={kidDialog.name}
                  onChange={e => setKidDialog({ ...kidDialog, name: e.target.value })}
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white focus:outline-none focus:border-blue-500"
                />
              </label>
              <div className="grid grid-cols-2 gap-4">
                <label className="block">
                  <span className="block text-sm text-gray-300 mb-1">{t('kidForm.birthdate')}</span>
                  <input
                    type="date"
                    value={kidDialog.birthdate}
                    onChange={e => setKidDialog({ ...kidDialog, birthdate: e.target.value })}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white focus:outline-none focus:border-blue-500"
                  />
                </label>
                <label className="block">
                  <span className="block text-sm text-gray-300 mb-1">{t('kidForm.emoji')}</span>
                  <input
                    type="text"
                    value={kidDialog.emoji}
                    onChange={e => setKidDialog({ ...kidDialog, emoji: e.target.value })}
                    maxLength={4}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white focus:outline-none focus:border-blue-500"
                  />
                </label>
              </div>
            </DialogBody>
            <DialogFooter>
              <button onClick={() => openKidDialog(null)} className="px-4 py-2 text-sm text-gray-300 hover:text-white cursor-pointer">
                {t('common:actions.cancel')}
              </button>
              <button
                onClick={saveKid}
                disabled={!kidDialog.name.trim() || saving}
                className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-4 py-2 text-sm cursor-pointer"
              >
                {t('common:actions.save')}
              </button>
            </DialogFooter>
          </>
        )}
      </Dialog>

      {/* Item add/edit dialog */}
      <Dialog open={itemDialog !== null} onClose={() => openItemDialog(null)} aria-labelledby="item-dialog-title" maxWidth="max-w-lg">
        {itemDialog && (
          <>
            <DialogHeader id="item-dialog-title" title={itemDialog.id ? t('editItem') : t('addItem')} onClose={() => openItemDialog(null)} />
            <DialogBody className="space-y-4">
              <FormError message={itemError} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <label className="block">
                  <span className="block text-sm text-gray-300 mb-1">{t('itemForm.name')}</span>
                  <input
                    type="text"
                    value={itemDialog.name}
                    onChange={e => setItemDialog({ ...itemDialog, name: e.target.value })}
                    placeholder={t('itemForm.namePlaceholder')}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                  />
                </label>
                <div>
                  <span className="block text-sm text-gray-300 mb-1" id="item-category-label">{t('itemForm.category')}</span>
                  <Select
                    aria-label={t('itemForm.category')}
                    value={itemDialog.category_id}
                    onChange={v => setItemDialog({ ...itemDialog, category_id: v })}
                    options={categories.map((c): SelectOption => ({ value: String(c.id), label: `${c.icon} ${c.name}` }))}
                    placeholder={t('itemForm.categoryPlaceholder')}
                  />
                </div>
                <label className="block">
                  <span className="block text-sm text-gray-300 mb-1">{t('itemForm.size')}</span>
                  <input
                    type="text"
                    value={itemDialog.size_label}
                    onChange={e => setItemDialog({ ...itemDialog, size_label: e.target.value })}
                    placeholder={itemDialog.category_id ? recommendedSizeFor(Number(itemDialog.category_id)) : ''}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                  />
                  {itemDialog.category_id && recommendedSizeFor(Number(itemDialog.category_id)) && (
                    <span className="block text-xs text-blue-400 mt-1">
                      {t('itemForm.sizeHint', { size: recommendedSizeFor(Number(itemDialog.category_id)) })}
                    </span>
                  )}
                </label>
                <label className="block">
                  <span className="block text-sm text-gray-300 mb-1">{t('itemForm.quantity')}</span>
                  <input
                    type="number"
                    min={1}
                    max={99}
                    value={itemDialog.quantity}
                    onChange={e => setItemDialog({ ...itemDialog, quantity: e.target.value })}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white focus:outline-none focus:border-blue-500"
                  />
                </label>
                <LabeledSelect label={t('itemForm.condition')} value={itemDialog.condition} onChange={v => setItemDialog({ ...itemDialog, condition: v })}
                  options={(['new', 'good', 'worn'] as const).map((k): SelectOption => ({ value: k, label: t(`condition.${k}`) }))} />
                <LabeledSelect label={t('itemForm.status')} value={itemDialog.status} onChange={v => setItemDialog({ ...itemDialog, status: v })}
                  options={(['active', 'too_small', 'stored'] as const).map((k): SelectOption => ({ value: k, label: t(`status.${k}`) }))} />
                <LabeledSelect label={t('itemForm.location')} value={itemDialog.location} onChange={v => setItemDialog({ ...itemDialog, location: v })}
                  options={(['home', 'kindergarten', 'school', 'cabin', 'other'] as const).map((k): SelectOption => ({ value: k, label: t(`location.${k}`) }))} />
                <LabeledSelect label={t('itemForm.season')} value={itemDialog.season} onChange={v => setItemDialog({ ...itemDialog, season: v })}
                  options={(['all', 'summer', 'winter'] as const).map((k): SelectOption => ({ value: k, label: t(`season.${k}`) }))} />
              </div>
              <label className="block">
                <span className="block text-sm text-gray-300 mb-1">{t('itemForm.notes')}</span>
                <input
                  type="text"
                  value={itemDialog.notes}
                  onChange={e => setItemDialog({ ...itemDialog, notes: e.target.value })}
                  placeholder={t('itemForm.notesPlaceholder')}
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                />
              </label>
            </DialogBody>
            <DialogFooter>
              <button onClick={() => openItemDialog(null)} className="px-4 py-2 text-sm text-gray-300 hover:text-white cursor-pointer">
                {t('common:actions.cancel')}
              </button>
              <button
                onClick={saveItem}
                disabled={!itemDialog.name.trim() || !itemDialog.category_id || saving}
                className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-4 py-2 text-sm cursor-pointer"
              >
                {t('common:actions.save')}
              </button>
            </DialogFooter>
          </>
        )}
      </Dialog>

      <CategoriesDialog
        open={categoriesOpen}
        onClose={() => setCategoriesOpen(false)}
        categories={categories}
        onChanged={async () => {
          try {
            setCategories(await loadCategories())
            if (selectedKidId !== null) await refreshKidData(selectedKidId)
          } catch {
            setError(t('errors.failedToLoad'))
          }
        }}
      />

      <ConfirmDialog
        open={confirm !== null}
        title={t('confirmTitle')}
        message={confirm?.message ?? ''}
        destructive
        confirmLabel={t('common:actions.delete')}
        cancelLabel={t('common:actions.cancel')}
        onConfirm={() => confirm?.action()}
        onCancel={() => setConfirm(null)}
      />
    </div>
  )
}

// LabeledSelect renders a Select with a visible label above it.
function LabeledSelect({ label, value, onChange, options }: {
  label: string
  value: string
  onChange: (v: string) => void
  options: SelectOption[]
}) {
  return (
    <div>
      <span className="block text-sm text-gray-300 mb-1">{label}</span>
      <Select aria-label={label} value={value} onChange={onChange} options={options} />
    </div>
  )
}

function KidStatsCard({ kid, dateFmt, onEdit, onDelete }: {
  kid: Kid
  dateFmt: Intl.DateTimeFormat
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation('wardrobe')
  const m = kid.latest_measurement

  return (
    <div className="bg-gray-800/60 border border-gray-700 rounded-xl p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-2xl" aria-hidden="true">{kid.avatar_emoji || '🧒'}</span>
          <div className="min-w-0">
            <h2 className="font-semibold truncate">{kid.name}</h2>
            {m && (
              <p className="text-xs text-gray-500">{t('stats.measuredOn', { date: dateFmt.format(new Date(m.measured_at)) })}</p>
            )}
          </div>
        </div>
        <div className="flex gap-1 shrink-0">
          <button onClick={onEdit} aria-label={t('editKid')} className="p-2 text-gray-400 hover:text-white cursor-pointer rounded-lg hover:bg-gray-700 transition-colors">
            <Pencil size={16} />
          </button>
          <button onClick={onDelete} aria-label={t('deleteKid')} className="p-2 text-gray-400 hover:text-red-400 cursor-pointer rounded-lg hover:bg-gray-700 transition-colors">
            <Trash2 size={16} />
          </button>
        </div>
      </div>

      {m ? (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mt-4">
          {m.height_cm > 0 && (
            <StatTile label={t('stats.height')} value={`${m.height_cm} cm`} sub={kid.height_rate_cm_per_month != null ? t('stats.growthHeight', { rate: kid.height_rate_cm_per_month }) : undefined} />
          )}
          {m.foot_length_mm > 0 && (
            <StatTile label={t('stats.foot')} value={`${m.foot_length_mm} mm`} sub={kid.foot_rate_mm_per_month != null ? t('stats.growthFoot', { rate: kid.foot_rate_mm_per_month }) : undefined} />
          )}
          {kid.clothing && (
            <StatTile label={t('stats.clothingSize')} value={String(kid.clothing.current_size)} sub={kid.clothing.buy_size !== kid.clothing.current_size ? t('stats.buy', { size: kid.clothing.buy_size }) : undefined} highlight />
          )}
          {kid.shoe && (
            <StatTile label={t('stats.shoeSize')} value={`EU ${kid.shoe.current_eu}`} sub={t('stats.buy', { size: `EU ${kid.shoe.buy_eu}` })} highlight />
          )}
        </div>
      ) : (
        <p className="text-sm text-gray-500 mt-3 flex items-center gap-2">
          <Ruler size={16} />
          {t('stats.noMeasurements')}
        </p>
      )}
    </div>
  )
}

function StatTile({ label, value, sub, highlight }: { label: string; value: string; sub?: string; highlight?: boolean }) {
  return (
    <div className={`rounded-lg p-3 ${highlight ? 'bg-blue-900/30 border border-blue-800' : 'bg-gray-900/60 border border-gray-700'}`}>
      <p className="text-xs text-gray-400">{label}</p>
      <p className="text-lg font-semibold text-white">{value}</p>
      {sub && <p className={`text-xs mt-0.5 ${highlight ? 'text-blue-300' : 'text-gray-500'}`}>{sub}</p>}
    </div>
  )
}

function InventoryTab({ items, categories, onAdd, onEdit, onStatus, onDelete, onManageCategories }: {
  items: Item[]
  categories: Category[]
  onAdd: () => void
  onEdit: (item: Item) => void
  onStatus: (item: Item, status: Item['status']) => void
  onDelete: (item: Item) => void
  onManageCategories: () => void
}) {
  const { t } = useTranslation(['wardrobe', 'common'])
  const [statusFilter, setStatusFilter] = useState<'all' | Item['status']>('all')

  const filtered = statusFilter === 'all' ? items : items.filter(i => i.status === statusFilter)
  const byCategory = new Map<number, Item[]>()
  for (const item of filtered) {
    const list = byCategory.get(item.category_id) ?? []
    list.push(item)
    byCategory.set(item.category_id, list)
  }

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2 mb-4">
        <div className="flex flex-wrap gap-1.5" role="group" aria-label={t('inventory.filterLabel')}>
          {(['all', 'active', 'too_small', 'stored'] as const).map(s => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              aria-pressed={statusFilter === s}
              className={`rounded-full px-3 py-1 text-xs cursor-pointer transition-colors border ${
                statusFilter === s
                  ? 'bg-gray-200 text-gray-900 border-gray-200'
                  : 'bg-gray-800 text-gray-400 border-gray-700 hover:text-white'
              }`}
            >
              {s === 'all' ? t('inventory.filterAll') : t(`status.${s}`)}
            </button>
          ))}
        </div>
        <div className="flex gap-2">
          <button onClick={onManageCategories} className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white cursor-pointer rounded-lg px-2 py-1.5 transition-colors">
            <Settings2 size={16} />
            <span className="hidden sm:inline">{t('categories.manage')}</span>
          </button>
          <button onClick={onAdd} className="flex items-center gap-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg px-3 py-1.5 text-sm cursor-pointer transition-colors">
            <Plus size={16} />
            {t('addItem')}
          </button>
        </div>
      </div>

      {filtered.length === 0 ? (
        <div className="text-center py-10 text-gray-500">
          <ShoppingBag size={32} className="mx-auto mb-2 opacity-60" />
          <p>{t('inventory.empty')}</p>
        </div>
      ) : (
        <div className="space-y-5">
          {categories.filter(c => byCategory.has(c.id)).map(cat => (
            <div key={cat.id}>
              <h3 className="text-sm font-medium text-gray-400 mb-1.5">
                <span aria-hidden="true">{cat.icon}</span> {cat.name}
              </h3>
              <div className="space-y-1">
                {(byCategory.get(cat.id) ?? []).map(item => (
                  <ItemRow key={item.id} item={item} onEdit={onEdit} onStatus={onStatus} onDelete={onDelete} />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function ItemRow({ item, onEdit, onStatus, onDelete }: {
  item: Item
  onEdit: (item: Item) => void
  onStatus: (item: Item, status: Item['status']) => void
  onDelete: (item: Item) => void
}) {
  const { t } = useTranslation('wardrobe')
  const statusColors: Record<Item['status'], string> = {
    active: 'bg-green-900/50 text-green-300 border-green-800',
    too_small: 'bg-amber-900/50 text-amber-300 border-amber-800',
    stored: 'bg-gray-700/60 text-gray-300 border-gray-600',
  }

  return (
    <div className="flex items-center gap-3 bg-gray-800/40 hover:bg-gray-800 border border-gray-700/60 rounded-lg px-3 py-2 transition-colors">
      <button onClick={() => onEdit(item)} className="flex-1 min-w-0 text-left cursor-pointer">
        <span className="flex items-center gap-2 flex-wrap">
          <span className="text-sm text-white truncate">{item.name}</span>
          {item.size_label && <span className="text-xs text-blue-300 bg-blue-900/40 border border-blue-800 rounded px-1.5 py-0.5">{item.size_label}</span>}
          {item.quantity > 1 && <span className="text-xs text-gray-400">×{item.quantity}</span>}
          <span className={`text-xs border rounded px-1.5 py-0.5 ${statusColors[item.status]}`}>{t(`status.${item.status}`)}</span>
        </span>
        <span className="block text-xs text-gray-500 mt-0.5">
          {t(`location.${item.location}`)}
          {item.season !== 'all' && ` · ${t(`season.${item.season}`)}`}
          {item.notes && ` · ${item.notes}`}
        </span>
      </button>
      {item.status === 'active' && (
        <button
          onClick={() => onStatus(item, 'too_small')}
          className="shrink-0 text-xs text-amber-400/80 hover:text-amber-300 cursor-pointer whitespace-nowrap"
        >
          {t('inventory.markTooSmall')}
        </button>
      )}
      <button onClick={() => onDelete(item)} aria-label={t('inventory.deleteItem', { name: item.name })} className="shrink-0 p-1.5 text-gray-500 hover:text-red-400 cursor-pointer transition-colors">
        <Trash2 size={15} />
      </button>
    </div>
  )
}

function MeasurementsTab({ kid, measurements, dateFmt, onChanged }: {
  kid: Kid
  measurements: Measurement[]
  dateFmt: Intl.DateTimeFormat
  onChanged: () => Promise<void>
}) {
  const { t } = useTranslation(['wardrobe', 'common'])
  const [form, setForm] = useState({ date: today(), height: '', foot: '', weight: '', note: '' })
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState('')

  const canSave = form.date !== '' && (form.height !== '' || form.foot !== '' || form.weight !== '')

  const save = async () => {
    if (!canSave || saving) return
    setSaving(true)
    setFormError('')
    try {
      await api(`/kids/${kid.id}/measurements`, {
        method: 'POST',
        body: JSON.stringify({
          measured_at: form.date,
          height_cm: Number(form.height) || 0,
          foot_length_mm: Number(form.foot) || 0,
          weight_kg: Number(form.weight) || 0,
          note: form.note.trim(),
        }),
      })
      setForm({ date: today(), height: '', foot: '', weight: '', note: '' })
      await onChanged()
    } catch (e) {
      // Keep the typed values so the offending field can be corrected.
      setFormError(messageFor(e, t, 'errors.failedToSave'))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: number) => {
    setFormError('')
    try {
      await api(`/measurements/${id}`, { method: 'DELETE' })
      await onChanged()
    } catch (e) {
      setFormError(messageFor(e, t, 'errors.failedToDelete'))
    }
  }

  const newestFirst = [...measurements].reverse()

  return (
    <div>
      <div className="bg-gray-800/60 border border-gray-700 rounded-xl p-4 mb-5">
        <h3 className="text-sm font-medium text-gray-300 mb-3">{t('measurements.add')}</h3>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          <label className="block">
            <span className="block text-xs text-gray-400 mb-1">{t('measurements.date')}</span>
            <input type="date" value={form.date} onChange={e => setForm({ ...form, date: e.target.value })}
              className="w-full bg-gray-900 border border-gray-700 rounded-lg px-2 py-2 text-sm text-white focus:outline-none focus:border-blue-500" />
          </label>
          <label className="block">
            <span className="block text-xs text-gray-400 mb-1">{t('measurements.height')}</span>
            <input type="number" inputMode="decimal" min={0} max={250} step="0.5" value={form.height} onChange={e => setForm({ ...form, height: e.target.value })}
              className="w-full bg-gray-900 border border-gray-700 rounded-lg px-2 py-2 text-sm text-white focus:outline-none focus:border-blue-500" />
          </label>
          <label className="block">
            <span className="block text-xs text-gray-400 mb-1">{t('measurements.foot')}</span>
            <input type="number" inputMode="decimal" min={0} max={400} step="1" value={form.foot} onChange={e => setForm({ ...form, foot: e.target.value })}
              className="w-full bg-gray-900 border border-gray-700 rounded-lg px-2 py-2 text-sm text-white focus:outline-none focus:border-blue-500" />
          </label>
          <label className="block">
            <span className="block text-xs text-gray-400 mb-1">{t('measurements.weight')}</span>
            <input type="number" inputMode="decimal" min={0} max={200} step="0.1" value={form.weight} onChange={e => setForm({ ...form, weight: e.target.value })}
              className="w-full bg-gray-900 border border-gray-700 rounded-lg px-2 py-2 text-sm text-white focus:outline-none focus:border-blue-500" />
          </label>
        </div>
        <p className="text-xs text-gray-500 mt-2">{t('measurements.footHint')}</p>
        <FormError message={formError} className="mt-2" />
        <div className="flex justify-end mt-3">
          <button onClick={save} disabled={!canSave || saving}
            className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-4 py-2 text-sm cursor-pointer transition-colors">
            {t('common:actions.save')}
          </button>
        </div>
      </div>

      {newestFirst.length === 0 ? (
        <p className="text-center text-gray-500 py-6">{t('measurements.empty')}</p>
      ) : (
        <div className="space-y-1">
          {newestFirst.map(m => (
            <div key={m.id} className="flex items-center gap-3 bg-gray-800/40 border border-gray-700/60 rounded-lg px-3 py-2">
              <span className="text-sm text-gray-300 w-28 shrink-0">{dateFmt.format(new Date(m.measured_at))}</span>
              <span className="flex-1 text-sm text-gray-400 flex flex-wrap gap-x-4">
                {m.height_cm > 0 && <span>{m.height_cm} cm</span>}
                {m.foot_length_mm > 0 && <span>{m.foot_length_mm} mm</span>}
                {m.weight_kg > 0 && <span>{m.weight_kg} kg</span>}
                {m.note && <span className="text-gray-500">{m.note}</span>}
              </span>
              <button onClick={() => remove(m.id)} aria-label={t('measurements.delete')} className="shrink-0 p-1.5 text-gray-500 hover:text-red-400 cursor-pointer transition-colors">
                <Trash2 size={15} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function NeedsTab({ needs, tooSmall, categoryById }: {
  needs: NeedEntry[]
  tooSmall: TooSmallEntry[]
  categoryById: Map<number, Category>
}) {
  const { t } = useTranslation('wardrobe')

  if (needs.length === 0 && tooSmall.length === 0) {
    return <p className="text-center text-gray-400 py-10">{t('needs.empty')}</p>
  }

  return (
    <div className="space-y-6">
      {needs.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-400 mb-2">{t('needs.missingTitle')}</h3>
          <div className="space-y-1">
            {needs.map(n => (
              <div key={n.category_id} className="flex items-center gap-3 bg-amber-950/30 border border-amber-900/50 rounded-lg px-3 py-2.5">
                <span aria-hidden="true">{n.category_icon}</span>
                <span className="flex-1 text-sm text-white">{n.category_name}</span>
                <span className="text-xs text-amber-300">{t('needs.have', { have: n.have, target: n.target })}</span>
                {n.recommended_size && (
                  <span className="text-xs text-blue-300 bg-blue-900/40 border border-blue-800 rounded px-1.5 py-0.5 whitespace-nowrap">
                    {t('needs.buySize', { size: n.recommended_size })}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {tooSmall.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-400 mb-2">{t('needs.tooSmallTitle')}</h3>
          <div className="space-y-1">
            {tooSmall.map(e => (
              <div key={e.item.id} className="flex items-center gap-3 bg-gray-800/40 border border-gray-700/60 rounded-lg px-3 py-2.5">
                <span aria-hidden="true">{categoryById.get(e.item.category_id)?.icon ?? '👕'}</span>
                <span className="flex-1 text-sm text-white truncate">
                  {e.item.name}
                  {e.item.size_label && <span className="text-gray-500 ml-2">({e.item.size_label})</span>}
                </span>
                {e.recommended_size && (
                  <span className="text-xs text-blue-300 bg-blue-900/40 border border-blue-800 rounded px-1.5 py-0.5 whitespace-nowrap">
                    {t('needs.replaceWith', { size: e.recommended_size })}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

function CategoriesDialog({ open, onClose, categories, onChanged }: {
  open: boolean
  onClose: () => void
  categories: Category[]
  onChanged: () => Promise<void>
}) {
  const { t } = useTranslation(['wardrobe', 'common'])
  const [newCat, setNewCat] = useState({ name: '', icon: '', size_system: 'clothing', target: '0' })
  const [busy, setBusy] = useState(false)
  const [rowError, setRowError] = useState('')

  const patchCategory = async (cat: Category, patch: Partial<Pick<Category, 'target_qty' | 'name' | 'icon' | 'size_system'>>) => {
    setRowError('')
    try {
      await api(`/categories/${cat.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          name: patch.name ?? cat.name,
          icon: patch.icon ?? cat.icon,
          size_system: patch.size_system ?? cat.size_system,
          target_qty: patch.target_qty ?? cat.target_qty,
        }),
      })
      await onChanged()
    } catch (e) {
      setRowError(messageFor(e, t, 'errors.failedToSave'))
    }
  }

  const removeCategory = async (cat: Category) => {
    setRowError('')
    try {
      // A 409 "category has items" maps to categories.inUse via the shared mapper.
      await api(`/categories/${cat.id}`, { method: 'DELETE' })
      await onChanged()
    } catch (e) {
      setRowError(messageFor(e, t, 'errors.failedToDelete'))
    }
  }

  const addCategory = async () => {
    if (!newCat.name.trim() || busy) return
    setBusy(true)
    setRowError('')
    try {
      await api('/categories', {
        method: 'POST',
        body: JSON.stringify({
          name: newCat.name.trim(),
          icon: newCat.icon,
          size_system: newCat.size_system,
          target_qty: Math.max(0, Number(newCat.target) || 0),
        }),
      })
      setNewCat({ name: '', icon: '', size_system: 'clothing', target: '0' })
      await onChanged()
    } catch (e) {
      setRowError(messageFor(e, t, 'errors.failedToSave'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} aria-labelledby="categories-dialog-title" maxWidth="max-w-lg">
      <DialogHeader id="categories-dialog-title" title={t('categories.manage')} onClose={onClose} />
      <DialogBody>
        <p className="text-xs text-gray-500 mb-3">{t('categories.targetHint')}</p>
        {rowError && <p role="alert" className="text-sm text-red-400 mb-3">{rowError}</p>}
        <div className="space-y-1.5">
          {categories.map(cat => (
            <div key={cat.id} className="flex items-center gap-2">
              <span className="w-7 text-center shrink-0" aria-hidden="true">{cat.icon}</span>
              <span className="flex-1 text-sm text-white truncate">{cat.name}</span>
              <span className="text-xs text-gray-500 hidden sm:inline">{t(`categories.sizeSystems.${cat.size_system}`)}</span>
              <label className="flex items-center gap-1.5">
                <span className="text-xs text-gray-400">{t('categories.target')}</span>
                <input
                  type="number"
                  min={0}
                  max={99}
                  defaultValue={cat.target_qty}
                  onBlur={e => {
                    const v = Math.max(0, Math.min(99, Number(e.target.value) || 0))
                    if (v !== cat.target_qty) patchCategory(cat, { target_qty: v })
                  }}
                  aria-label={t('categories.targetFor', { name: cat.name })}
                  className="w-14 bg-gray-900 border border-gray-700 rounded px-2 py-1 text-sm text-white focus:outline-none focus:border-blue-500"
                />
              </label>
              <button onClick={() => removeCategory(cat)} aria-label={t('categories.deleteCategory', { name: cat.name })} className="shrink-0 p-1.5 text-gray-500 hover:text-red-400 cursor-pointer transition-colors">
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>

        <div className="mt-4 pt-4 border-t border-gray-700">
          <h4 className="text-sm text-gray-300 mb-2">{t('categories.add')}</h4>
          <div className="flex flex-wrap gap-2">
            <input
              type="text"
              value={newCat.icon}
              onChange={e => setNewCat({ ...newCat, icon: e.target.value })}
              maxLength={4}
              placeholder="🧢"
              aria-label={t('categories.icon')}
              className="w-14 bg-gray-900 border border-gray-700 rounded-lg px-2 py-2 text-sm text-white text-center placeholder-gray-600 focus:outline-none focus:border-blue-500"
            />
            <input
              type="text"
              value={newCat.name}
              onChange={e => setNewCat({ ...newCat, name: e.target.value })}
              placeholder={t('categories.name')}
              aria-label={t('categories.name')}
              className="flex-1 min-w-32 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-600 focus:outline-none focus:border-blue-500"
            />
            <Select
              aria-label={t('categories.sizeSystem')}
              value={newCat.size_system}
              onChange={v => setNewCat({ ...newCat, size_system: v })}
              options={(['clothing', 'shoe', 'none'] as const).map((s): SelectOption => ({ value: s, label: t(`categories.sizeSystems.${s}`) }))}
              className="w-32"
            />
            <button
              onClick={addCategory}
              disabled={!newCat.name.trim() || busy}
              className="bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded-lg px-3 py-2 text-sm cursor-pointer transition-colors"
            >
              {t('categories.addButton')}
            </button>
          </div>
        </div>
      </DialogBody>
      <DialogFooter>
        <button onClick={onClose} className="px-4 py-2 text-sm text-gray-300 hover:text-white cursor-pointer">
          {t('common:actions.close')}
        </button>
      </DialogFooter>
    </Dialog>
  )
}
