import { useEffect, useCallback, useReducer, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Loan } from './types'

export interface UseLoansResult {
  loans: Loan[]
  loading: boolean
  error: string | null
  createLoan: (form: Omit<Loan, 'id'>) => Promise<boolean>
  updateLoan: (id: number, form: Omit<Loan, 'id'>) => Promise<boolean>
  deleteLoan: (id: number) => Promise<boolean>
  refetch: () => void
}

type State = { loans: Loan[]; loading: boolean; error: string | null }
type Action =
  | { type: 'fetching' }
  | { type: 'success'; loans: Loan[] }
  | { type: 'error'; message: string }

function loansReducer(state: State, action: Action): State {
  switch (action.type) {
    case 'fetching': return { ...state, loading: true, error: null }
    case 'success': return { loans: action.loans, loading: false, error: null }
    case 'error': return { ...state, loading: false, error: action.message }
  }
}

export function useLoans(): UseLoansResult {
  const { t } = useTranslation('budget')
  const [state, dispatch] = useReducer(loansReducer, { loans: [], loading: true, error: null })
  const [refreshKey, setRefreshKey] = useState(0)

  const refetch = useCallback(() => setRefreshKey(k => k + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'fetching' })
    fetch('/api/budget/loans', { credentials: 'include', signal: controller.signal })
      .then(r => {
        if (!r.ok) throw new Error('fetch failed')
        return r.json() as Promise<{ loans: Loan[] }>
      })
      .then(d => dispatch({ type: 'success', loans: d.loans }))
      .catch(err => {
        if (err instanceof Error && err.name === 'AbortError') return
        dispatch({ type: 'error', message: t('loan.errors.loadFailed') })
      })
    return () => controller.abort()
  }, [refreshKey, t])

  const createLoan = useCallback(async (form: Omit<Loan, 'id'>): Promise<boolean> => {
    try {
      const r = await fetch('/api/budget/loans', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      if (!r.ok) throw new Error('create failed')
      setRefreshKey(k => k + 1)
      return true
    } catch {
      dispatch({ type: 'error', message: t('loan.errors.saveFailed') })
      return false
    }
  }, [t])

  const updateLoan = useCallback(async (id: number, form: Omit<Loan, 'id'>): Promise<boolean> => {
    try {
      const r = await fetch(`/api/budget/loans/${id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })
      if (!r.ok) throw new Error('update failed')
      setRefreshKey(k => k + 1)
      return true
    } catch {
      dispatch({ type: 'error', message: t('loan.errors.saveFailed') })
      return false
    }
  }, [t])

  const deleteLoan = useCallback(async (id: number): Promise<boolean> => {
    try {
      const r = await fetch(`/api/budget/loans/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!r.ok) throw new Error('delete failed')
      setRefreshKey(k => k + 1)
      return true
    } catch {
      dispatch({ type: 'error', message: t('loan.errors.deleteFailed') })
      return false
    }
  }, [t])

  return { loans: state.loans, loading: state.loading, error: state.error, createLoan, updateLoan, deleteLoan, refetch }
}
