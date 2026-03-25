import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Users, Copy, Plus, Trash2, Edit2, Check, X } from 'lucide-react'

interface FamilyLink {
  id: number
  parent_id: number
  child_id: number
  nickname: string
  avatar_emoji: string
  created_at: string
}

interface InviteCode {
  id: number
  code: string
  parent_id: number
  used: boolean
  expires_at: string
  created_at: string
}

interface FamilyStatus {
  is_parent: boolean
  is_child: boolean
}

export default function Family() {
  const { t } = useTranslation('common')
  const [status, setStatus] = useState<FamilyStatus | null>(null)
  const [children, setChildren] = useState<FamilyLink[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [invite, setInvite] = useState<InviteCode | null>(null)
  const [generating, setGenerating] = useState(false)
  const [copied, setCopied] = useState(false)
  const [inviteInput, setInviteInput] = useState('')
  const [accepting, setAccepting] = useState(false)
  const [acceptError, setAcceptError] = useState('')
  const [removeConfirmId, setRemoveConfirmId] = useState<number | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editNickname, setEditNickname] = useState('')
  const [editEmoji, setEditEmoji] = useState('')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    loadData()
  }, [])

  async function loadData() {
    try {
      setLoading(true)
      setError('')
      const [statusRes, childrenRes] = await Promise.all([
        fetch('/api/family/status', { credentials: 'include' }),
        fetch('/api/family/children', { credentials: 'include' }),
      ])
      if (!statusRes.ok || !childrenRes.ok) {
        throw new Error('failed')
      }
      const statusData = await statusRes.json()
      const childrenData = await childrenRes.json()
      setStatus(statusData)
      setChildren(childrenData.children ?? [])
    } catch {
      setError(t('family.errors.failedToLoad'))
    } finally {
      setLoading(false)
    }
  }

  async function generateInvite() {
    try {
      setGenerating(true)
      const res = await fetch('/api/family/invite', {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      const data = await res.json()
      setInvite(data.invite)
    } catch {
      setError(t('family.errors.failedToGenerate'))
    } finally {
      setGenerating(false)
    }
  }

  async function copyCode() {
    if (!invite) return
    try {
      await navigator.clipboard.writeText(invite.code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // ignore clipboard errors
    }
  }

  async function acceptInvite() {
    if (!inviteInput.trim()) return
    try {
      setAccepting(true)
      setAcceptError('')
      const res = await fetch('/api/family/invite/accept', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: inviteInput.trim().toUpperCase() }),
      })
      const data = await res.json()
      if (!res.ok) {
        const msg = data.error ?? ''
        if (msg.includes('invalid')) setAcceptError(t('family.errors.invalidCode'))
        else if (msg.includes('expired')) setAcceptError(t('family.errors.expiredCode'))
        else if (msg.includes('already linked')) setAcceptError(t('family.errors.alreadyLinked'))
        else if (msg.includes('already been used')) setAcceptError(t('family.errors.usedCode'))
        else setAcceptError(t('family.errors.failedToAccept'))
        return
      }
      setInviteInput('')
      await loadData()
    } catch {
      setAcceptError(t('family.errors.failedToAccept'))
    } finally {
      setAccepting(false)
    }
  }

  async function removeChild(childId: number) {
    try {
      const res = await fetch(`/api/family/children/${childId}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error('failed')
      setRemoveConfirmId(null)
      await loadData()
    } catch {
      setError(t('family.errors.failedToRemove'))
    }
  }

  function startEdit(child: FamilyLink) {
    setEditingId(child.child_id)
    setEditNickname(child.nickname)
    setEditEmoji(child.avatar_emoji)
  }

  async function saveEdit(childId: number) {
    try {
      setSaving(true)
      const res = await fetch(`/api/family/children/${childId}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nickname: editNickname, avatar_emoji: editEmoji }),
      })
      if (!res.ok) throw new Error('failed')
      setEditingId(null)
      await loadData()
    } catch {
      setError(t('family.errors.failedToUpdate'))
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="p-6 text-gray-400">{t('status.loading')}...</div>
    )
  }

  return (
    <div className="p-6 max-w-2xl mx-auto">
      <div className="flex items-center gap-3 mb-6">
        <Users size={24} className="text-blue-400" />
        <h1 className="text-2xl font-semibold text-white">{t('family.title')}</h1>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/40 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Parent view: manage children */}
      <section className="mb-8">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-medium text-white">{t('family.children')}</h2>
          <button
            onClick={generateInvite}
            disabled={generating}
            className="flex items-center gap-2 px-3 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
          >
            <Plus size={16} />
            {t('family.addChild')}
          </button>
        </div>

        {/* Invite code display */}
        {invite && (
          <div className="mb-4 p-4 bg-gray-800 border border-gray-700 rounded-lg">
            <p className="text-sm text-gray-400 mb-2">{t('family.inviteCode')}</p>
            <div className="flex items-center gap-3">
              <span className="text-2xl font-mono font-bold text-white tracking-widest">{invite.code}</span>
              <button
                onClick={copyCode}
                className="flex items-center gap-1.5 px-2.5 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
                title={t('family.copyInvite')}
              >
                <Copy size={14} />
                {copied ? t('family.inviteCopied') : t('family.copyInvite')}
              </button>
            </div>
            <p className="text-xs text-gray-500 mt-2">{t('family.inviteExpiry')}</p>
          </div>
        )}

        {children.length === 0 ? (
          <div className="p-6 text-center bg-gray-800/50 rounded-lg border border-gray-700">
            <p className="text-gray-400 font-medium">{t('family.noChildren')}</p>
            <p className="text-gray-500 text-sm mt-1">{t('family.noChildrenHint')}</p>
          </div>
        ) : (
          <div className="space-y-3">
            {children.map(child => (
              <div key={child.child_id} className="p-4 bg-gray-800 border border-gray-700 rounded-lg">
                {editingId === child.child_id ? (
                  <div className="flex items-center gap-3">
                    <input
                      value={editEmoji}
                      onChange={e => setEditEmoji(e.target.value)}
                      className="w-12 text-center bg-gray-700 border border-gray-600 rounded px-2 py-1 text-white text-xl"
                      maxLength={4}
                      aria-label={t('family.avatarEmoji')}
                    />
                    <input
                      value={editNickname}
                      onChange={e => setEditNickname(e.target.value)}
                      placeholder={t('family.nickname')}
                      className="flex-1 bg-gray-700 border border-gray-600 rounded px-3 py-1.5 text-white text-sm"
                      aria-label={t('family.nickname')}
                    />
                    <button
                      onClick={() => saveEdit(child.child_id)}
                      disabled={saving}
                      className="p-1.5 text-green-400 hover:text-green-300 transition-colors cursor-pointer"
                      title={t('family.saveChild')}
                    >
                      <Check size={16} />
                    </button>
                    <button
                      onClick={() => setEditingId(null)}
                      className="p-1.5 text-gray-400 hover:text-gray-300 transition-colors cursor-pointer"
                      title={t('actions.cancel')}
                    >
                      <X size={16} />
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center gap-3">
                    <span className="text-2xl" aria-hidden="true">{child.avatar_emoji || '⭐'}</span>
                    <div className="flex-1 min-w-0">
                      <p className="text-white font-medium truncate">
                        {child.nickname || `User #${child.child_id}`}
                      </p>
                    </div>
                    <button
                      onClick={() => startEdit(child)}
                      className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                      title={t('family.editChild')}
                    >
                      <Edit2 size={16} />
                    </button>
                    {removeConfirmId === child.child_id ? (
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-red-400">{t('family.removeConfirm')}</span>
                        <button
                          onClick={() => removeChild(child.child_id)}
                          className="px-2 py-1 bg-red-700 hover:bg-red-600 text-white text-xs rounded transition-colors cursor-pointer"
                        >
                          {t('actions.confirm')}
                        </button>
                        <button
                          onClick={() => setRemoveConfirmId(null)}
                          className="px-2 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
                        >
                          {t('actions.cancel')}
                        </button>
                      </div>
                    ) : (
                      <button
                        onClick={() => setRemoveConfirmId(child.child_id)}
                        className="p-1.5 text-gray-400 hover:text-red-400 transition-colors cursor-pointer"
                        title={t('family.removeChild')}
                      >
                        <Trash2 size={16} />
                      </button>
                    )}
                  </div>
                )}
                {removeConfirmId === child.child_id && !editingId && (
                  <p className="text-xs text-gray-500 mt-2 ml-9">{t('family.removeConfirmHint')}</p>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Child view: join a family */}
      {(!status?.is_child) && (
        <section>
          <h2 className="text-lg font-medium text-white mb-4">{t('family.joinFamily')}</h2>
          <div className="p-4 bg-gray-800 border border-gray-700 rounded-lg">
            <div className="flex items-center gap-3">
              <input
                value={inviteInput}
                onChange={e => setInviteInput(e.target.value.toUpperCase())}
                placeholder={t('family.enterInviteCode')}
                maxLength={6}
                className="flex-1 bg-gray-700 border border-gray-600 rounded px-3 py-2 text-white font-mono tracking-widest uppercase placeholder:normal-case placeholder:tracking-normal"
                aria-label={t('family.enterInviteCode')}
                onKeyDown={e => { if (e.key === 'Enter') acceptInvite() }}
              />
              <button
                onClick={acceptInvite}
                disabled={accepting || !inviteInput.trim()}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded-lg transition-colors cursor-pointer"
              >
                {t('family.acceptInvite')}
              </button>
            </div>
            {acceptError && (
              <p className="text-red-400 text-sm mt-2">{acceptError}</p>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
