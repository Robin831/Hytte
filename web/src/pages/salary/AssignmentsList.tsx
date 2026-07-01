import { useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Upload } from 'lucide-react'
import { useAuth } from '../../auth'
import type { SalaryData } from './useSalaryData'

interface AssignmentsListProps {
  salary: SalaryData
}

/**
 * Per-month trekktabell assignments card, plus the admin-only trekktabell data
 * import form. Owns its form inputs; data + mutations come from useSalaryData.
 */
export default function AssignmentsList({ salary }: AssignmentsListProps) {
  const { t } = useTranslation('salary')
  const auth = useAuth()
  const isAdmin = auth.user?.is_admin ?? false
  const {
    assignments,
    assignmentsLoading,
    assignmentsError,
    setAssignmentsError,
    saveAssignment,
    deleteAssignment,
    importTrekktabellData,
  } = salary

  const [newAssignmentMonth, setNewAssignmentMonth] = useState('')
  const [newAssignmentTable, setNewAssignmentTable] = useState('')
  const [savingAssignment, setSavingAssignment] = useState(false)

  const [importYear, setImportYear] = useState(String(new Date().getFullYear()))
  const [importing, setImporting] = useState(false)
  const [importMessage, setImportMessage] = useState<string | null>(null)
  const [importError, setImportError] = useState<string | null>(null)
  const importFileInputRef = useRef<HTMLInputElement>(null)

  const handleSaveAssignment = async () => {
    const month = newAssignmentMonth.trim()
    const tableNumber = newAssignmentTable.trim()
    if (!/^\d{4}-\d{2}$/.test(month)) {
      setAssignmentsError(t('trekktabellAssignments.invalidMonth'))
      return
    }
    if (!/^\d{4}$/.test(tableNumber)) {
      setAssignmentsError(t('trekktabellAssignments.invalidTable'))
      return
    }
    setSavingAssignment(true)
    const ok = await saveAssignment(month, tableNumber)
    if (ok) {
      setNewAssignmentMonth('')
      setNewAssignmentTable('')
    }
    setSavingAssignment(false)
  }

  const handleImportTrekktabellData = async (e: React.FormEvent) => {
    e.preventDefault()
    const year = parseInt(importYear, 10)
    if (Number.isNaN(year) || year < 2000 || year > 2100) {
      setImportError(t('trekktabellImport.invalidYear'))
      return
    }
    const file = importFileInputRef.current?.files?.[0]
    if (!file) {
      setImportError(t('trekktabellImport.selectFile'))
      return
    }
    setImporting(true)
    setImportError(null)
    setImportMessage(null)
    try {
      const data = await importTrekktabellData(year, file)
      setImportMessage(t('trekktabellImport.success', { rows: data.rows, tables: data.tables, year: data.year }))
      if (importFileInputRef.current) importFileInputRef.current.value = ''
    } catch (err) {
      setImportError(err instanceof Error ? err.message : t('errors.failedToImport'))
    } finally {
      setImporting(false)
    }
  }

  return (
    <div className="bg-gray-800 rounded-xl p-5 space-y-3">
      <div>
        <h2 className="text-base font-medium text-white">{t('trekktabellAssignments.title')}</h2>
        <p className="text-xs text-gray-500 mt-1">{t('trekktabellAssignments.helper')}</p>
      </div>

      {assignmentsLoading && (
        <p className="text-sm text-gray-400">{t('trekktabellAssignments.loading')}</p>
      )}

      {!assignmentsLoading && assignments.length === 0 && (
        <p className="text-sm text-gray-500 italic">{t('trekktabellAssignments.noneYet')}</p>
      )}

      {!assignmentsLoading && assignments.length > 0 && (
        <ul className="divide-y divide-gray-700/50 text-sm">
          {assignments.map(a => (
            <li key={a.effective_from} className="flex items-center justify-between py-2">
              <div>
                <span className="text-white tabular-nums">{a.effective_from}</span>
                <span className="text-gray-500 mx-2">→</span>
                <span className="text-white tabular-nums">{a.table_number}</span>
              </div>
              <button
                type="button"
                onClick={() => deleteAssignment(a.effective_from)}
                aria-label={t('trekktabellAssignments.delete')}
                className="p-1.5 text-gray-500 hover:text-red-400 hover:bg-gray-700 rounded-lg transition-colors"
              >
                <Trash2 size={16} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-col sm:flex-row gap-2 pt-2 border-t border-gray-700/50">
        <input
          type="month"
          value={newAssignmentMonth}
          onChange={e => setNewAssignmentMonth(e.target.value)}
          className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
          aria-label={t('trekktabellAssignments.fromMonth')}
        />
        <input
          type="text"
          inputMode="numeric"
          pattern="[0-9]{4}"
          maxLength={4}
          placeholder={t('trekktabellAssignments.tableNumberPlaceholder')}
          value={newAssignmentTable}
          onChange={e => setNewAssignmentTable(e.target.value.replace(/\D/g, '').slice(0, 4))}
          className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500 w-24"
          aria-label={t('trekktabellAssignments.tableNumber')}
        />
        <button
          type="button"
          onClick={handleSaveAssignment}
          disabled={savingAssignment || !newAssignmentMonth || newAssignmentTable.length !== 4}
          className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors"
        >
          {savingAssignment ? '...' : t('trekktabellAssignments.add')}
        </button>
      </div>

      {assignmentsError && <p className="text-sm text-red-400">{assignmentsError}</p>}

      {isAdmin && (
        <form onSubmit={handleImportTrekktabellData} className="pt-3 border-t border-gray-700/50 space-y-2">
          <div className="flex items-center gap-2">
            <Upload size={14} className="text-gray-400" />
            <h3 className="text-sm font-medium text-gray-300">{t('trekktabellImport.title')}</h3>
          </div>
          <p className="text-xs text-gray-500">{t('trekktabellImport.helper')}</p>
          <div className="flex flex-col sm:flex-row gap-2">
            <input
              type="number"
              value={importYear}
              onChange={e => setImportYear(e.target.value)}
              min={2000}
              max={2100}
              className="bg-gray-700 text-white rounded-lg px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-blue-500 w-24"
              aria-label={t('trekktabellImport.year')}
            />
            <input
              ref={importFileInputRef}
              type="file"
              accept=".txt,.zip"
              className="text-xs text-gray-400 file:mr-2 file:py-1.5 file:px-3 file:rounded-lg file:border-0 file:bg-gray-700 file:text-gray-300 hover:file:bg-gray-600 file:cursor-pointer flex-1 min-w-0"
              aria-label={t('trekktabellImport.file')}
            />
            <button
              type="submit"
              disabled={importing}
              className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white text-sm rounded-lg transition-colors whitespace-nowrap"
            >
              {importing ? '...' : t('trekktabellImport.submit')}
            </button>
          </div>
          {importMessage && <p className="text-sm text-green-400">{importMessage}</p>}
          {importError && <p className="text-sm text-red-400">{importError}</p>}
        </form>
      )}
    </div>
  )
}
