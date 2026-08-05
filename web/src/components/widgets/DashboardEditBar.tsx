import { useTranslation } from 'react-i18next'
import { Check, LayoutGrid, RotateCcw } from 'lucide-react'

interface DashboardEditBarProps {
  editing: boolean
  saving: boolean
  onToggleEditing: () => void
  onReset: () => void
}

/**
 * Toolbar above the dashboard grid: enters and leaves layout edit mode and
 * clears a customised layout back to the default order.
 */
export default function DashboardEditBar({
  editing,
  saving,
  onToggleEditing,
  onReset,
}: DashboardEditBarProps) {
  const { t } = useTranslation('dashboard')

  return (
    <div className="mb-4 flex flex-wrap items-center justify-end gap-2">
      {editing && (
        <p className="mr-auto text-xs text-gray-500 basis-full sm:basis-auto">{t('layout.hint')}</p>
      )}
      {editing && (
        <button
          type="button"
          onClick={onReset}
          disabled={saving}
          className="inline-flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-2 text-xs text-gray-300 transition-colors hover:bg-gray-700 disabled:opacity-40"
        >
          <RotateCcw size={14} />
          {t('layout.reset')}
        </button>
      )}
      <button
        type="button"
        onClick={onToggleEditing}
        className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-xs transition-colors ${
          editing
            ? 'bg-blue-600 text-white hover:bg-blue-500'
            : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
        }`}
      >
        {editing ? <Check size={14} /> : <LayoutGrid size={14} />}
        {editing ? t('layout.done') : t('layout.edit')}
      </button>
    </div>
  )
}
