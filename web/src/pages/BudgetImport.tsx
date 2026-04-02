import { useState, useRef, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { Upload, ChevronLeft, AlertCircle, CheckCircle, Loader2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

// Column mapping: -1 means "not mapped"
interface ColumnMapping {
  date: number
  description: number
  amount: number
}

interface ImportRow {
  line: number
  date: string
  description: string
  amount: number
  error?: string
}

interface Account {
  id: number
  name: string
  type: string
  currency: string
}

type Step = 'upload' | 'preview' | 'done'

const DEFAULT_MAPPING: ColumnMapping = { date: 0, description: 1, amount: 2 }

export default function BudgetImport() {
  const { t } = useTranslation('budget')
  const [step, setStep] = useState<Step>('upload')
  const [file, setFile] = useState<File | null>(null)
  const [mapping, setMapping] = useState<ColumnMapping>(DEFAULT_MAPPING)
  const [dateFormat, setDateFormat] = useState('')
  const [skipHeader, setSkipHeader] = useState(true)

  const [rows, setRows] = useState<ImportRow[]>([])
  const [parseErrors, setParseErrors] = useState<string[]>([])
  const [selectedRows, setSelectedRows] = useState<Set<number>>(new Set())

  const [accounts, setAccounts] = useState<Account[]>([])
  const [accountID, setAccountID] = useState<number>(0)

  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [importedCount, setImportedCount] = useState(0)

  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    fetch('/api/budget/accounts', { credentials: 'include' })
      .then(r => (r.ok ? r.json() : { accounts: [] }))
      .then((data: { accounts?: Account[] }) => {
        const list = data.accounts ?? []
        setAccounts(list)
        if (list.length > 0) setAccountID(list[0].id)
      })
      .catch(() => {/* accounts list is non-critical at this stage */})
  }, [])

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFile(e.target.files?.[0] ?? null)
    setError('')
  }

  const handlePreview = async () => {
    if (!file) {
      setError(t('import.errors.noFile'))
      return
    }
    setLoading(true)
    setError('')
    try {
      const form = new FormData()
      form.append('file', file)
      form.append('mapping', JSON.stringify(mapping))
      form.append('skip_header', skipHeader ? 'true' : 'false')
      if (dateFormat) form.append('date_format', dateFormat)

      const res = await fetch('/api/budget/import/csv', {
        method: 'POST',
        credentials: 'include',
        body: form,
      })
      const data = await res.json()
      if (!res.ok) {
        setError(data.error ?? t('import.errors.parseFailed'))
        return
      }
      const parsed: ImportRow[] = data.rows ?? []
      setRows(parsed)
      setParseErrors(data.errors ?? [])
      // Pre-select all rows without errors.
      setSelectedRows(new Set(parsed.filter(r => !r.error).map(r => r.line)))
      setStep('preview')
    } catch {
      setError(t('import.errors.parseFailed'))
    } finally {
      setLoading(false)
    }
  }

  const toggleRow = (line: number) => {
    setSelectedRows(prev => {
      const next = new Set(prev)
      if (next.has(line)) next.delete(line)
      else next.add(line)
      return next
    })
  }

  const handleCommit = async () => {
    if (accountID === 0) {
      setError(t('import.errors.noAccount'))
      return
    }
    const toImport = rows.filter(r => selectedRows.has(r.line))
    if (toImport.length === 0) {
      setError(t('import.errors.noRowsSelected'))
      return
    }
    setLoading(true)
    setError('')
    try {
      const res = await fetch('/api/budget/import/csv/commit', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ account_id: accountID, transactions: toImport }),
      })
      const data = await res.json()
      if (!res.ok) {
        setError(data.error ?? t('import.errors.commitFailed'))
        return
      }
      setImportedCount(data.imported ?? 0)
      setStep('done')
    } catch {
      setError(t('import.errors.commitFailed'))
    } finally {
      setLoading(false)
    }
  }

  const fmt = useMemo(
    () => new Intl.NumberFormat(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
    [],
  )

  const goodRows = rows.filter(r => !r.error)
  const badRows = rows.filter(r => r.error)
  const selectedCount = rows.filter(r => selectedRows.has(r.line) && !r.error).length

  return (
    <div className="p-6 max-w-5xl mx-auto">
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <Link to="/budget" className="text-gray-400 hover:text-white transition-colors">
          <ChevronLeft size={20} />
        </Link>
        <h1 className="text-2xl font-semibold text-white">{t('import.title')}</h1>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-3 mb-4 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm">
          <AlertCircle size={16} className="shrink-0" />
          {error}
        </div>
      )}

      {/* Step: Upload & Map */}
      {step === 'upload' && (
        <div className="space-y-6">
          {/* File upload */}
          <div className="p-4 rounded-lg bg-gray-800 border border-gray-700">
            <label className="block text-sm font-medium text-gray-300 mb-2">
              {t('import.fileLabel')}
            </label>
            <div
              className="flex flex-col items-center justify-center gap-2 p-8 border-2 border-dashed border-gray-600 rounded-lg cursor-pointer hover:border-blue-500 transition-colors"
              onClick={() => fileInputRef.current?.click()}
            >
              <Upload size={24} className="text-gray-400" />
              <span className="text-gray-400 text-sm">
                {file ? file.name : t('import.fileHint')}
              </span>
            </div>
            <input
              ref={fileInputRef}
              type="file"
              accept=".csv,text/csv"
              className="hidden"
              aria-label={t('import.fileLabel')}
              onChange={handleFileChange}
            />
          </div>

          {/* Column mapping */}
          <div className="p-4 rounded-lg bg-gray-800 border border-gray-700">
            <h2 className="text-sm font-medium text-gray-300 mb-3">{t('import.mappingTitle')}</h2>
            <p className="text-xs text-gray-500 mb-4">{t('import.mappingHint')}</p>

            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              {(['date', 'description', 'amount'] as const).map(field => (
                <div key={field}>
                  <label
                    htmlFor={`col-${field}`}
                    className="block text-xs text-gray-400 mb-1"
                  >
                    {t(`import.columns.${field}`)}
                  </label>
                  <input
                    id={`col-${field}`}
                    type="number"
                    min={-1}
                    value={mapping[field]}
                    onChange={e =>
                      setMapping(prev => ({
                        ...prev,
                        [field]: e.target.value === '' ? -1 : parseInt(e.target.value, 10),
                      }))
                    }
                    className="w-full bg-gray-700 text-white text-sm rounded px-3 py-1.5 border border-gray-600 focus:outline-none focus:border-blue-500"
                  />
                </div>
              ))}
            </div>
          </div>

          {/* Options */}
          <div className="p-4 rounded-lg bg-gray-800 border border-gray-700">
            <h2 className="text-sm font-medium text-gray-300 mb-3">{t('import.optionsTitle')}</h2>

            <div className="space-y-3">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={skipHeader}
                  onChange={e => setSkipHeader(e.target.checked)}
                  className="accent-blue-500"
                />
                <span className="text-sm text-gray-300">{t('import.skipHeader')}</span>
              </label>

              <div>
                <label htmlFor="date-format" className="block text-xs text-gray-400 mb-1">
                  {t('import.dateFormatLabel')}
                </label>
                <input
                  id="date-format"
                  type="text"
                  placeholder={t('import.dateFormatPlaceholder')}
                  value={dateFormat}
                  onChange={e => setDateFormat(e.target.value)}
                  className="bg-gray-700 text-white text-sm rounded px-3 py-1.5 border border-gray-600 focus:outline-none focus:border-blue-500 w-48"
                />
              </div>
            </div>
          </div>

          <button
            onClick={handlePreview}
            disabled={!file || loading}
            className="flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
          >
            {loading ? <Loader2 size={16} className="animate-spin" /> : <Upload size={16} />}
            {t('import.previewButton')}
          </button>
        </div>
      )}

      {/* Step: Preview */}
      {step === 'preview' && (
        <div className="space-y-4">
          {/* Summary bar */}
          <div className="flex flex-wrap items-center gap-4 p-3 rounded-lg bg-gray-800 border border-gray-700 text-sm">
            <span className="text-gray-300">
              {t('import.previewSummary', { total: rows.length, good: goodRows.length, bad: badRows.length })}
            </span>
            <span className="text-blue-400 font-medium">
              {t('import.selectedCount', { count: selectedCount })}
            </span>
            <button
              onClick={() => setStep('upload')}
              className="ml-auto flex items-center gap-1 text-gray-400 hover:text-white text-xs transition-colors"
            >
              <X size={14} />
              {t('import.back')}
            </button>
          </div>

          {/* Parse-level errors */}
          {parseErrors.length > 0 && (
            <div className="p-3 rounded-lg bg-yellow-500/10 border border-yellow-500/30 text-yellow-400 text-xs space-y-1">
              {parseErrors.map((e, i) => <div key={i}>{e}</div>)}
            </div>
          )}

          {/* Account selector */}
          <div className="flex items-center gap-3">
            <label htmlFor="account-select" className="text-sm text-gray-300 shrink-0">
              {t('import.accountLabel')}
            </label>
            {accounts.length === 0 ? (
              <span className="text-sm text-yellow-400">{t('import.noAccounts')}</span>
            ) : (
              <select
                id="account-select"
                value={accountID}
                onChange={e => setAccountID(parseInt(e.target.value, 10))}
                className="bg-gray-700 text-white text-sm rounded px-3 py-1.5 border border-gray-600 focus:outline-none focus:border-blue-500"
              >
                {accounts.map(a => (
                  <option key={a.id} value={a.id}>
                    {a.name} ({a.currency})
                  </option>
                ))}
              </select>
            )}
          </div>

          {/* Rows table */}
          <div className="overflow-x-auto rounded-lg border border-gray-700">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-gray-800 text-gray-400 text-left">
                  <th className="px-3 py-2 w-8">
                    <input
                      type="checkbox"
                      aria-label={t('import.selectAll')}
                      checked={selectedCount === goodRows.length && goodRows.length > 0}
                      onChange={e => {
                        if (e.target.checked) setSelectedRows(new Set(goodRows.map(r => r.line)))
                        else setSelectedRows(new Set())
                      }}
                      className="accent-blue-500"
                    />
                  </th>
                  <th className="px-3 py-2">{t('import.columns.date')}</th>
                  <th className="px-3 py-2">{t('import.columns.description')}</th>
                  <th className="px-3 py-2 text-right">{t('import.columns.amount')}</th>
                  <th className="px-3 py-2">{t('import.statusLabel')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map(row => (
                  <tr
                    key={row.line}
                    className={`border-t border-gray-700 ${row.error ? 'opacity-50' : 'hover:bg-gray-800/50'}`}
                  >
                    <td className="px-3 py-2">
                      <input
                        type="checkbox"
                        aria-label={t('import.selectRow', { line: row.line })}
                        checked={selectedRows.has(row.line)}
                        disabled={!!row.error}
                        onChange={() => toggleRow(row.line)}
                        className="accent-blue-500"
                      />
                    </td>
                    <td className="px-3 py-2 text-gray-300 whitespace-nowrap">{row.date}</td>
                    <td className="px-3 py-2 text-gray-300 max-w-xs truncate">{row.description}</td>
                    <td className={`px-3 py-2 text-right whitespace-nowrap font-mono ${row.amount < 0 ? 'text-red-400' : 'text-green-400'}`}>
                      {fmt.format(row.amount)}
                    </td>
                    <td className="px-3 py-2">
                      {row.error ? (
                        <span className="flex items-center gap-1 text-red-400 text-xs">
                          <AlertCircle size={12} />
                          {row.error}
                        </span>
                      ) : (
                        <CheckCircle size={14} className="text-green-500" />
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <button
            onClick={handleCommit}
            disabled={loading || selectedCount === 0 || accountID === 0}
            className="flex items-center gap-2 px-4 py-2 rounded-lg bg-green-600 hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
          >
            {loading ? <Loader2 size={16} className="animate-spin" /> : <CheckCircle size={16} />}
            {t('import.commitButton', { count: selectedCount })}
          </button>
        </div>
      )}

      {/* Step: Done */}
      {step === 'done' && (
        <div className="flex flex-col items-center gap-4 py-12">
          <CheckCircle size={48} className="text-green-500" />
          <p className="text-lg font-medium text-white">
            {t('import.doneMessage', { count: importedCount })}
          </p>
          <div className="flex gap-3">
            <button
              onClick={() => {
                setStep('upload')
                setFile(null)
                setRows([])
                setParseErrors([])
                setError('')
              }}
              className="px-4 py-2 rounded-lg bg-gray-700 hover:bg-gray-600 text-white text-sm transition-colors"
            >
              {t('import.importAnother')}
            </button>
            <Link
              to="/budget"
              className="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white text-sm transition-colors"
            >
              {t('import.backToBudget')}
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}
