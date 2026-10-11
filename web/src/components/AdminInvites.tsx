import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, UserPlus } from 'lucide-react'

interface Invite { email: string; created_at: string }

/** Who may sign in: Hytte only lets in existing users and invited emails. */
export default function AdminInvites() {
  const { t } = useTranslation('settings')
  const [invites, setInvites] = useState<Invite[]>([])
  const [email, setEmail] = useState('')
  const [error, setError] = useState('')

  const call = async (url: string, init?: RequestInit) => {
    setError('')
    try {
      const res = await fetch(url, { credentials: 'include', ...init })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error((data as { error?: string }).error || `HTTP ${res.status}`)
      setInvites((data as { invites?: Invite[] }).invites ?? [])
      return true
    } catch (err) {
      setError(err instanceof Error ? err.message : t('admin.invites.error'))
      return false
    }
  }

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/admin/invites', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : { invites: [] }))
      .then((d: { invites?: Invite[] }) => setInvites(d.invites ?? []))
      .catch(() => {})
    return () => controller.abort()
  }, [])

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!email.trim()) return
    const ok = await call('/api/admin/invites', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email }),
    })
    if (ok) setEmail('')
  }

  return (
    <section className="mt-8 rounded-xl bg-gray-800 p-4" aria-labelledby="invites-h">
      <h2 id="invites-h" className="text-lg font-semibold">{t('admin.invites.heading')}</h2>
      <p className="mt-1 text-sm text-gray-400">{t('admin.invites.about')}</p>
      <form onSubmit={add} className="mt-3 flex flex-wrap gap-2">
        <input type="email" value={email} onChange={e => setEmail(e.target.value)} required
          placeholder={t('admin.invites.placeholder')} aria-label={t('admin.invites.placeholder')}
          className="min-w-0 flex-1 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-sm" />
        <button type="submit" className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-2 text-sm font-medium hover:bg-blue-500 cursor-pointer">
          <UserPlus size={15} aria-hidden="true" /> {t('admin.invites.add')}
        </button>
      </form>
      {error && <p role="alert" className="mt-2 text-sm text-red-400">{error}</p>}
      {invites.length === 0 ? (
        <p className="mt-3 text-sm text-gray-500">{t('admin.invites.none')}</p>
      ) : (
        <ul className="mt-3 divide-y divide-gray-700 text-sm">
          {invites.map(i => (
            <li key={i.email} className="flex items-center justify-between gap-3 py-2">
              <span className="min-w-0 break-all">{i.email} <span className="text-xs text-gray-500">· {t('admin.invites.pending')}</span></span>
              <button type="button" aria-label={t('admin.invites.remove', { email: i.email })} className="text-gray-500 hover:text-red-300 cursor-pointer"
                onClick={() => call(`/api/admin/invites?email=${encodeURIComponent(i.email)}`, { method: 'DELETE' })}>
                <Trash2 size={15} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
