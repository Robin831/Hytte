import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { FlexState } from '../types'
import { useWorkHoursApi } from '../useWorkHoursApi'

/**
 * Flex-pool state for the day view: the running total and redeeming a rounding
 * interval of it onto a day. The pool is loaded on mount; callers reload it
 * after anything that changes a day's remainder.
 */
export function useFlex() {
  const { t } = useTranslation(['workhours', 'common'])
  const api = useWorkHoursApi()

  const [flex, setFlex] = useState<FlexState | null>(null)
  const [redeeming, setRedeeming] = useState(false)

  const reload = useCallback(() => {
    api.getFlex()
      .then(data => setFlex(data))
      .catch(() => {})
  }, [api])

  useEffect(() => {
    reload()
  }, [reload])

  /**
   * Redeems flex onto `date` and reloads the pool. Resolves true when the
   * server accepted it, so the caller can reload the day it was credited to;
   * failures are reported with an alert.
   */
  const redeem = useCallback(async (date: string): Promise<boolean> => {
    setRedeeming(true)
    try {
      const res = await api.redeemFlex(date)
      if (res.ok) {
        reload()
        return true
      }
      alert(res.error ?? t('workhours:redeemFlexError'))
      return false
    } catch {
      alert(t('workhours:redeemFlexError'))
      return false
    } finally {
      setRedeeming(false)
    }
  }, [api, reload, t])

  return { flex, redeeming, reload, redeem }
}

export type FlexPool = ReturnType<typeof useFlex>
