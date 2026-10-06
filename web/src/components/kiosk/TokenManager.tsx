import { useState, useEffect, useCallback, useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Plus, QrCode, Moon } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { ConfirmDialog, Dialog, DialogHeader, DialogBody, DialogFooter } from '../ui/dialog'
import TokenCreateDialog from './TokenCreateDialog'
import DimOverrideFields from './DimOverrideFields'
import {
  EMPTY_DIM_OVERRIDE,
  dimOverrideFromConfig,
  dimOverrideToRequest,
  validateDimOverride,
  type DimOverride,
} from './dimOverride'
import { formatDate } from '../../utils/formatDate'

interface KioskToken {
  id: number
  name: string
  config: unknown
  created_by: string
  created_at: string
  expires_at: string | null
  last_used_at: string | null
}

export default function TokenManager() {
  const { t } = useTranslation('settings')

  const [tokens, setTokens] = useState<KioskToken[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<KioskToken | null>(null)
  const [revokeError, setRevokeError] = useState('')
  const [qrToken, setQrToken] = useState<KioskToken | null>(null)
  const qrTitleId = useId()
  const qrDescId = useId()
  const [dimTarget, setDimTarget] = useState<KioskToken | null>(null)
  const [dimDraft, setDimDraft] = useState<DimOverride>(EMPTY_DIM_OVERRIDE)
  const [dimSaving, setDimSaving] = useState(false)
  const [dimError, setDimError] = useState('')
  const dimTitleId = useId()

  const fetchTokens = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetch('/api/kiosk/tokens', { credentials: 'include' })
      if (!res.ok) {
        setError(t('kioskTokens.errorLoad'))
        return
      }
      const data: KioskToken[] = await res.json()
      const sorted = (data ?? []).slice().sort(
        (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      )
      setTokens(sorted)
    } catch {
      setError(t('kioskTokens.errorLoad'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchTokens()
  }, [fetchTokens])

  async function handleRevoke() {
    if (!revokeTarget) return
    setRevokeError('')
    const res = await fetch(`/api/kiosk/tokens/${revokeTarget.id}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (res.ok) {
      setTokens((prev) => prev.filter((tok) => tok.id !== revokeTarget.id))
    } else {
      setRevokeError(t('kioskTokens.revokeError'))
      throw new Error('revoke failed')
    }
  }

  function openDimEditor(token: KioskToken) {
    setDimTarget(token)
    setDimDraft(dimOverrideFromConfig(token.config))
    setDimError('')
  }

  function closeDimEditor() {
    if (dimSaving) return
    setDimTarget(null)
    setDimError('')
  }

  async function handleDimSave(e: React.FormEvent) {
    e.preventDefault()
    if (!dimTarget) return
    setDimError('')
    const validation = validateDimOverride(dimDraft)
    if (validation) {
      setDimError(t(`kioskTokens.dim.error.${validation}`))
      return
    }
    setDimSaving(true)
    try {
      const res = await fetch(`/api/kiosk/tokens/${dimTarget.id}/dim`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(dimOverrideToRequest(dimDraft)),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setDimError(data.error ?? t('kioskTokens.dim.error.save'))
        return
      }
      const data: { config: unknown } = await res.json()
      const id = dimTarget.id
      setTokens((prev) => prev.map((tok) => (tok.id === id ? { ...tok, config: data.config } : tok)))
      setDimTarget(null)
    } catch {
      setDimError(t('kioskTokens.dim.error.save'))
    } finally {
      setDimSaving(false)
    }
  }

  function dimSummary(token: KioskToken): string {
    const dim = dimOverrideFromConfig(token.config)
    const mode = t(`kioskTokens.dim.mode.${dim.mode}`)
    if (dim.mode !== 'off' && dim.start && dim.end) {
      return t('kioskTokens.dim.summaryWindow', { mode, start: dim.start, end: dim.end })
    }
    return mode
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <p className="text-sm text-gray-400">{t('kioskTokens.description')}</p>
        <button
          type="button"
          onClick={() => setShowCreate(true)}
          aria-expanded={showCreate}
          aria-haspopup="dialog"
          className="flex items-center gap-2 px-3 py-2 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer shrink-0"
        >
          <Plus size={16} />
          {t('kioskTokens.createButton')}
        </button>
      </div>

      {loading && (
        <p className="text-sm text-gray-400">{t('kioskTokens.loading')}</p>
      )}

      {!loading && error && (
        <p className="text-sm text-red-400">{error}</p>
      )}

      {revokeError && (
        <p className="text-sm text-red-400">{revokeError}</p>
      )}

      {!loading && !error && tokens.length === 0 && (
        <p className="text-sm text-gray-500 italic">{t('kioskTokens.empty')}</p>
      )}

      {!loading && !error && tokens.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-400 border-b border-gray-700">
                <th className="pb-2 pr-4 font-medium">{t('kioskTokens.colName')}</th>
                <th className="pb-2 pr-4 font-medium">{t('kioskTokens.colExpiry')}</th>
                <th className="pb-2 pr-4 font-medium">{t('kioskTokens.colLastUsed')}</th>
                <th className="pb-2 pr-4 font-medium hidden sm:table-cell">{t('kioskTokens.dim.column')}</th>
                <th className="pb-2 font-medium sr-only">{t('kioskTokens.colActions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-700/50">
              {tokens.map((token) => (
                <tr key={token.id} className="group">
                  <td className="py-3 pr-4">
                    <span className="text-white font-medium">{token.name}</span>
                    <span className="block text-xs text-gray-500">{token.created_by}</span>
                    <span className="block text-xs text-gray-500 sm:hidden">{dimSummary(token)}</span>
                  </td>
                  <td className="py-3 pr-4 text-gray-300">
                    {token.expires_at
                      ? formatDate(token.expires_at, { dateStyle: 'medium' })
                      : <span className="text-gray-500">{t('kioskTokens.noExpiry')}</span>}
                  </td>
                  <td className="py-3 pr-4 text-gray-400">
                    {token.last_used_at ? formatDate(token.last_used_at, { dateStyle: 'medium' }) : '—'}
                  </td>
                  <td className="py-3 pr-4 text-gray-400 hidden sm:table-cell">{dimSummary(token)}</td>
                  <td className="py-3 text-right">
                    <div className="flex items-center justify-end gap-2">
                      <button
                        type="button"
                        onClick={() => openDimEditor(token)}
                        aria-label={t('kioskTokens.dim.editAriaLabel', { name: token.name })}
                        className="text-gray-500 hover:text-white transition-colors cursor-pointer"
                      >
                        <Moon size={16} />
                      </button>
                      <button
                        type="button"
                        onClick={() => setQrToken(token)}
                        aria-label={t('kioskTokens.showQrAriaLabel', { name: token.name })}
                        className="text-gray-500 hover:text-white transition-colors cursor-pointer"
                      >
                        <QrCode size={16} />
                      </button>
                      <button
                        type="button"
                        onClick={() => { setRevokeError(''); setRevokeTarget(token) }}
                        aria-label={t('kioskTokens.revokeAriaLabel', { name: token.name })}
                        className="text-gray-500 hover:text-red-400 transition-colors cursor-pointer"
                      >
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <TokenCreateDialog
        open={showCreate}
        onClose={() => setShowCreate(false)}
        onSuccess={() => {
          fetchTokens()
        }}
      />

      <ConfirmDialog
        open={revokeTarget !== null}
        onClose={() => { setRevokeTarget(null); setRevokeError('') }}
        onConfirm={handleRevoke}
        title={t('kioskTokens.revokeTitle')}
        message={t('kioskTokens.revokeMessage', { name: revokeTarget?.name ?? '' })}
        confirmLabel={t('kioskTokens.revokeConfirm')}
        variant="destructive"
      />

      <Dialog open={dimTarget !== null} onClose={closeDimEditor} maxWidth="max-w-md" aria-labelledby={dimTitleId}>
        <DialogHeader
          id={dimTitleId}
          title={t('kioskTokens.dim.editTitle', { name: dimTarget?.name ?? '' })}
          onClose={closeDimEditor}
        />
        <form onSubmit={handleDimSave}>
          <DialogBody>
            <DimOverrideFields value={dimDraft} onChange={setDimDraft} disabled={dimSaving} />
            {dimError && <p className="text-sm text-red-400 mt-3">{dimError}</p>}
          </DialogBody>
          <DialogFooter>
            <button
              type="button"
              onClick={closeDimEditor}
              disabled={dimSaving}
              className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors disabled:opacity-50 cursor-pointer"
            >
              {t('kioskTokens.cancel')}
            </button>
            <button
              type="submit"
              disabled={dimSaving}
              className="px-4 py-2 text-sm font-medium rounded bg-blue-600 hover:bg-blue-500 text-white transition-colors disabled:opacity-50 cursor-pointer"
            >
              {dimSaving ? t('kioskTokens.dim.saving') : t('kioskTokens.dim.save')}
            </button>
          </DialogFooter>
        </form>
      </Dialog>

      <Dialog open={qrToken !== null} onClose={() => setQrToken(null)} maxWidth="max-w-sm" aria-labelledby={qrTitleId} aria-describedby={qrDescId}>
        <DialogHeader id={qrTitleId} title={t('kioskTokens.showQrTitle', { name: qrToken?.name ?? '' })} onClose={() => setQrToken(null)} />
        <DialogBody>
          <p id={qrDescId} className="text-sm text-gray-300 mb-3">{t('kioskTokens.showQrDescription')}</p>
          <p className="text-xs text-amber-400 mb-3">{t('kioskTokens.showQrNoToken')}</p>
          <div className="flex flex-col items-center">
            <div className="bg-white p-3 rounded-lg">
              <QRCodeSVG
                value={`${window.location.origin}/kiosk`}
                size={200}
                level="M"
                role="img"
                title={t('kioskTokens.showQrTitle', { name: qrToken?.name ?? '' })}
              />
            </div>
            <p className="text-xs text-gray-500 mt-2">{`${window.location.origin}/kiosk`}</p>
          </div>
        </DialogBody>
        <DialogFooter>
          <button
            type="button"
            onClick={() => setQrToken(null)}
            className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors cursor-pointer"
          >
            {t('kioskTokens.done')}
          </button>
        </DialogFooter>
      </Dialog>
    </div>
  )
}
