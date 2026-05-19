import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Users } from 'lucide-react'
import { Dialog, DialogHeader, DialogBody, DialogFooter } from '../../components/ui/dialog'
import { Skeleton } from '../../components/ui/skeleton'
import { useAuth } from '../../auth'

interface NewConversationModalProps {
  open: boolean
  onClose: () => void
  onCreated: (conversationId: number) => void
}

interface MemberOption {
  id: number
  label: string
  emoji: string
}

interface FamilyChild {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface SiblingInfo {
  child_id: number
  nickname: string
  avatar_emoji: string
}

interface ParentInfo {
  user_id: number
  name: string
  picture: string
}

export default function NewConversationModal({ open, onClose, onCreated }: NewConversationModalProps) {
  const { t } = useTranslation('familyChat')
  const { familyStatus } = useAuth()
  const titleId = useId()
  const nameInputId = useId()

  const [name, setName] = useState('')
  const [members, setMembers] = useState<MemberOption[]>([])
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [membersLoading, setMembersLoading] = useState(false)
  const [membersError, setMembersError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')

  // Reset form state when the modal transitions from open to closed.
  // Using the "adjust state during render" pattern avoids setState-in-effect.
  const [prevOpen, setPrevOpen] = useState(open)
  if (prevOpen !== open) {
    setPrevOpen(open)
    if (!open) {
      setName('')
      setSelectedIds(new Set())
      setSubmitError('')
    }
  }

  // Fetch the available family members each time the modal opens. We source
  // from /family/children for parents and /family/my-family for child users,
  // matching the endpoints used by the Family page.
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    ;(async () => {
      setMembersLoading(true)
      setMembersError('')
      try {
        const collected: MemberOption[] = []
        if (familyStatus?.is_parent) {
          const res = await fetch('/api/family/children', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (!res.ok) throw new Error(`family/children responded ${res.status}`)
          const data = await res.json()
          const kids: FamilyChild[] = data.children ?? []
          for (const k of kids) {
            collected.push({
              id: k.child_id,
              label: k.nickname || `#${k.child_id}`,
              emoji: k.avatar_emoji || '⭐',
            })
          }
        }
        if (familyStatus?.is_child) {
          const res = await fetch('/api/family/my-family', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (!res.ok) throw new Error(`family/my-family responded ${res.status}`)
          const data = await res.json()
          const parent: ParentInfo | undefined = data.parent
          if (parent?.user_id) {
            collected.push({
              id: parent.user_id,
              label: parent.name || t('newModal.parent'),
              emoji: '👤',
            })
          }
          const siblings: SiblingInfo[] = data.siblings ?? []
          for (const s of siblings) {
            collected.push({
              id: s.child_id,
              label: s.nickname || `#${s.child_id}`,
              emoji: s.avatar_emoji || '⭐',
            })
          }
        }
        if (!controller.signal.aborted) {
          setMembers(collected)
        }
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        setMembersError(t('newModal.errors.loadMembers'))
      } finally {
        if (!controller.signal.aborted) setMembersLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [open, familyStatus, t])

  function toggleMember(id: number) {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed || selectedIds.size === 0 || submitting) return
    setSubmitting(true)
    setSubmitError('')
    try {
      const res = await fetch('/api/familychat/conversations', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: trimmed,
          member_user_ids: Array.from(selectedIds),
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('newModal.errors.create'))
      }
      const data = await res.json()
      const id: number | undefined = data?.conversation?.id
      if (typeof id !== 'number') {
        throw new Error(t('newModal.errors.create'))
      }
      onCreated(id)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : t('newModal.errors.create'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} aria-labelledby={titleId}>
      <DialogHeader id={titleId} title={t('newModal.title')} onClose={onClose} />
      <form onSubmit={handleSubmit} className="flex flex-col flex-1 min-h-0">
        <DialogBody className="space-y-4">
          <div>
            <label htmlFor={nameInputId} className="block text-sm text-gray-300 mb-1">
              {t('newModal.nameLabel')}
            </label>
            <input
              id={nameInputId}
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder={t('newModal.namePlaceholder')}
              maxLength={200}
              required
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm placeholder:text-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>

          <fieldset>
            <legend className="block text-sm text-gray-300 mb-1">
              {t('newModal.membersLabel')}
            </legend>
            {membersLoading && (
              <div className="space-y-2" role="status" aria-live="polite" aria-busy="true">
                <span className="sr-only">{t('loading')}</span>
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            )}
            {!membersLoading && membersError && (
              <p className="text-sm text-red-400">{membersError}</p>
            )}
            {!membersLoading && !membersError && members.length === 0 && (
              <div className="p-3 text-sm text-gray-400 bg-gray-800/60 border border-gray-700 rounded-lg flex items-start gap-2">
                <Users size={16} className="mt-0.5 flex-shrink-0 text-gray-500" aria-hidden="true" />
                <span>{t('newModal.noMembers')}</span>
              </div>
            )}
            {!membersLoading && !membersError && members.length > 0 && (
              <ul className="space-y-1" role="list">
                {members.map(member => {
                  const checked = selectedIds.has(member.id)
                  return (
                    <li key={member.id}>
                      <label
                        className={`flex items-center gap-3 px-3 py-2 rounded-lg cursor-pointer border transition-colors ${
                          checked
                            ? 'bg-blue-500/10 border-blue-500/40'
                            : 'bg-gray-800/60 border-gray-700 hover:bg-gray-800'
                        }`}
                      >
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleMember(member.id)}
                          className="sr-only"
                        />
                        <span
                          aria-hidden="true"
                          className="w-8 h-8 flex items-center justify-center rounded-full bg-gray-700/60 text-lg"
                        >
                          {member.emoji}
                        </span>
                        <span className="text-white text-sm truncate flex-1">{member.label}</span>
                        {checked && (
                          <Check size={16} className="text-blue-400 flex-shrink-0" aria-hidden="true" />
                        )}
                      </label>
                    </li>
                  )
                })}
              </ul>
            )}
          </fieldset>

          {submitError && (
            <p className="text-sm text-red-400" role="alert">{submitError}</p>
          )}
        </DialogBody>
        <DialogFooter>
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors disabled:opacity-50 cursor-pointer"
          >
            {t('newModal.cancel')}
          </button>
          <button
            type="submit"
            disabled={submitting || !name.trim() || selectedIds.size === 0}
            className="px-4 py-2 text-sm font-medium bg-blue-600 hover:bg-blue-500 text-white rounded transition-colors disabled:opacity-50 cursor-pointer"
          >
            {submitting ? t('newModal.creating') : t('newModal.create')}
          </button>
        </DialogFooter>
      </form>
    </Dialog>
  )
}
