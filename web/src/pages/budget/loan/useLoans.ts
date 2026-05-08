import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import type { Loan } from './types'

export interface UseLoansResult {
  loans: Loan[]
  loading: boolean
  error: string | null
  createLoan: (form: Omit<Loan, 'id'>) => Promise<boolean>
  updateLoan: (id: number, form: Omit<Loan, 'id'>) => Promise<boolean>
  deleteLoan: (id: number) => Promise<boolean>
  refetch: () => Promise<void>
}

export function useLoans(): UseLoansResult {
  const { t } = useTranslation('budget')
  const [loans, setLoans] = useState<Loan[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refetch = useCallback(async () => {
    setError(null)
    try {
      const r = await fetch('/api/budget/loans', { credentials: 'include' })
      if (!r.ok) throw new Error('fetch failed')
      const d = await r.json() as { loans: Loan[] }
      setLoans(d.loans)
    } catch {
      setError(t('loan.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch
    void refetch()
  }, [refetch])

  const createLoan = useCallback(async (form: Omit<Loan, 'id'>): Promise<boolean> => {
    try {
      const r = await fetch('/api/budget/loans', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      if (!r.ok) throw new Error('create failed')
      await refetch()
      return true
    } catch {
      setError(t('loan.errors.saveFailed'))
      return false
    }
  }, [refetch, t])

  const updateLoan = useCallback(async (id: number, form: Omit<Loan, 'id'>): Promise<boolean> => {
    try {
      const r = await fetch(`/api/budget/loans/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      if (!r.ok) throw new Error('update failed')
      await refetch()
      return true
    } catch {
      setError(t('loan.errors.saveFailed'))
      return false
    }
  }, [refetch, t])

  const deleteLoan = useCallback(async (id: number): Promise<boolean> => {
    try {
      const r = await fetch(`/api/budget/loans/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!r.ok) throw new Error('delete failed')
      await refetch()
      return true
    } catch {
      setError(t('loan.errors.deleteFailed'))
      return false
    }
  }, [refetch, t])

  return { loans, loading, error, createLoan, updateLoan, deleteLoan, refetch }
}
