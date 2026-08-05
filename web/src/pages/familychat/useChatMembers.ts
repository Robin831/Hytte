import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { MemberChip } from './ChatHeader'

// useChatMembers resolves the user ids that appear in a conversation to
// friendly names and avatar emoji. Parents learn their children's nicknames
// from /api/family/children; children learn their parent and siblings from
// /api/family/my-family. Anything the current user cannot name falls back to
// "Member #id", so a failed lookup degrades the labels but never the chat.

export interface MemberInfo {
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

interface CurrentUser {
  id: number
  name?: string
  email?: string
}

interface FamilyStatus {
  is_parent?: boolean
  is_child?: boolean
}

interface UseChatMembersOptions {
  user: CurrentUser | null | undefined
  familyStatus: FamilyStatus | null | undefined
  // memberIds is the conversation's roster, used for the header chips. Empty
  // until the conversation loads.
  memberIds: number[] | undefined
}

export interface UseChatMembersApi {
  // memberLabel resolves a user id to the friendly name shown on bubbles,
  // member chips and call banners. Stable so memoized bubbles only re-render
  // when the lookup itself changes.
  memberLabel: (id: number) => string
  // memberInfo adds the avatar emoji, for the group-call tiles.
  memberInfo: (id: number) => MemberInfo
  // peerLabel is memberLabel widened to accept null, for the 1:1 call overlay
  // where the remote id is unknown until the answer arrives.
  peerLabel: (id: number | null) => string
  memberChips: MemberChip[]
  // selfEmoji is the current user's avatar, shown on their own call tile.
  selfEmoji: string
}

export function useChatMembers({ user, familyStatus, memberIds }: UseChatMembersOptions): UseChatMembersApi {
  const { t } = useTranslation('familyChat')
  const [memberLookup, setMemberLookup] = useState<Map<number, MemberInfo>>(new Map())

  // Build a label/emoji lookup for every user the current user can name, so
  // member chips and sender labels render with friendly names. The current
  // user is always included from auth context.
  useEffect(() => {
    if (!user) return
    const controller = new AbortController()
    ;(async () => {
      const lookup = new Map<number, MemberInfo>()
      lookup.set(user.id, { label: user.name || user.email || `#${user.id}`, emoji: '👤' })
      try {
        if (familyStatus?.is_parent) {
          const res = await fetch('/api/family/children', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (res.ok) {
            const data = await res.json()
            const kids: FamilyChild[] = data.children ?? []
            for (const k of kids) {
              lookup.set(k.child_id, {
                label: k.nickname || `#${k.child_id}`,
                emoji: k.avatar_emoji || '⭐',
              })
            }
          }
        }
        if (familyStatus?.is_child) {
          const res = await fetch('/api/family/my-family', {
            credentials: 'include',
            signal: controller.signal,
          })
          if (res.ok) {
            const data = await res.json()
            const parent: ParentInfo | undefined = data.parent
            if (parent?.user_id) {
              lookup.set(parent.user_id, {
                label: parent.name || t('newModal.parent'),
                emoji: '👤',
              })
            }
            const siblings: SiblingInfo[] = data.siblings ?? []
            for (const s of siblings) {
              lookup.set(s.child_id, {
                label: s.nickname || `#${s.child_id}`,
                emoji: s.avatar_emoji || '⭐',
              })
            }
          }
        }
      } catch (err) {
        if (err instanceof Error && err.name === 'AbortError') return
        // Non-fatal: chips fall back to "Member #id" if the lookup is empty.
      }
      if (!controller.signal.aborted) setMemberLookup(lookup)
    })()
    return () => { controller.abort() }
  }, [user, familyStatus, t])

  const memberLabel = useCallback((id: number) => (
    memberLookup.get(id)?.label ?? t('chat.memberFallback', { id })
  ), [memberLookup, t])

  const memberInfo = useCallback((id: number): MemberInfo => (
    { label: memberLabel(id), emoji: memberLookup.get(id)?.emoji ?? '👤' }
  ), [memberLabel, memberLookup])

  const peerLabel = useCallback((id: number | null) => {
    if (id === null) return t('chat.memberFallback', { id: 0 })
    return memberLabel(id)
  }, [memberLabel, t])

  const memberChips = useMemo<MemberChip[]>(() => {
    if (!memberIds) return []
    return memberIds.map(id => {
      const info = memberLookup.get(id)
      const isSelf = user?.id === id
      return {
        id,
        label: isSelf
          ? t('chat.you')
          : info?.label ?? t('chat.memberFallback', { id }),
        emoji: info?.emoji ?? '👤',
        isSelf,
      }
    })
  }, [memberIds, memberLookup, t, user?.id])

  const selfEmoji = (user?.id !== undefined ? memberLookup.get(user.id)?.emoji : undefined) ?? '👤'

  return { memberLabel, memberInfo, peerLabel, memberChips, selfEmoji }
}
