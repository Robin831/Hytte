import { useEffect, useReducer } from 'react'
import { Star, ListChecks } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface ChoreWithStatus {
  id: number
  name: string
  completion_status: string | null
}

interface ChoresResponse {
  chores: ChoreWithStatus[]
}

interface BalanceResponse {
  current_balance: number
  level: number
  title: string
}

interface State {
  loading: boolean
  pendingChores: number
  balance: number | null
  level: number | null
  title: string | null
}

type Action =
  | { type: 'start' }
  | { type: 'chores'; pending: number }
  | { type: 'stars'; balance: number; level: number; title: string }
  | { type: 'error' }

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'start': return { ...state, loading: true }
    case 'chores': return { ...state, loading: false, pendingChores: action.pending }
    case 'stars': return { ...state, loading: false, balance: action.balance, level: action.level, title: action.title }
    case 'error': return { ...state, loading: false }
  }
}

export default function KidsSummaryWidget() {
  const { t } = useTranslation('today')
  const [{ loading, pendingChores, balance }, dispatch] = useReducer(reducer, {
    loading: true,
    pendingChores: 0,
    balance: null,
    level: null,
    title: null,
  })

  useEffect(() => {
    const controller = new AbortController()
    dispatch({ type: 'start' })

    fetch('/api/allowance/my/chores', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<ChoresResponse>) : Promise.reject()))
      .then((d) => {
        const pending = (d.chores ?? []).filter(
          (c) => c.completion_status === null || c.completion_status === 'pending',
        ).length
        dispatch({ type: 'chores', pending })
      })
      .catch(() => { if (!controller.signal.aborted) dispatch({ type: 'error' }) })

    fetch('/api/stars/balance', { credentials: 'include', signal: controller.signal })
      .then((r) => (r.ok ? (r.json() as Promise<BalanceResponse>) : Promise.reject()))
      .then((d) => dispatch({ type: 'stars', balance: d.current_balance, level: d.level, title: d.title }))
      .catch(() => {})

    return () => controller.abort()
  }, [])

  if (loading && balance === null) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Star size={16} className="shrink-0" />
        <span>{t('kids.loading')}</span>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-3 text-sm">
      {balance !== null && (
        <span className="flex items-center gap-1">
          <Star size={14} className="text-yellow-400 shrink-0" />
          <span className="text-gray-200">{balance}</span>
        </span>
      )}
      <span className="flex items-center gap-1">
        <ListChecks size={14} className="text-gray-400 shrink-0" />
        <span className="text-gray-300">
          {pendingChores > 0
            ? t('kids.pendingChores', { count: pendingChores })
            : t('kids.allDone')}
        </span>
      </span>
    </div>
  )
}
