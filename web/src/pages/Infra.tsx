import { useState, useEffect, useCallback } from 'react'

import {
  RefreshCw,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  HelpCircle,
  ToggleLeft,
  ToggleRight,
} from 'lucide-react'

interface ModuleInfo {
  name: string
  display_name: string
  description: string
  enabled: boolean
}

interface ModuleResult {
  name: string
  status: 'ok' | 'degraded' | 'down' | 'unknown'
  message?: string
  details?: unknown
  checked_at: string
}

interface StatusResponse {
  overall: 'ok' | 'degraded' | 'down' | 'unknown'
  modules: ModuleResult[]
}

const statusConfig = {
  ok: { icon: CheckCircle2, color: 'text-green-400', bg: 'bg-green-400/10', border: 'border-green-400/20', label: 'Healthy' },
  degraded: { icon: AlertTriangle, color: 'text-yellow-400', bg: 'bg-yellow-400/10', border: 'border-yellow-400/20', label: 'Degraded' },
  down: { icon: XCircle, color: 'text-red-400', bg: 'bg-red-400/10', border: 'border-red-400/20', label: 'Down' },
  unknown: { icon: HelpCircle, color: 'text-gray-400', bg: 'bg-gray-400/10', border: 'border-gray-400/20', label: 'Unknown' },
}

export default function Infra() {
  const [modules, setModules] = useState<ModuleInfo[]>([])
  const [status, setStatus] = useState<StatusResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [toggling, setToggling] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const fetchModules = useCallback(async () => {
    const res = await fetch('/api/infra/modules', { credentials: 'include' })
    if (!res.ok) {
      throw new Error(`Failed to load modules (${res.status})`)
    }
    const data = await res.json()
    setModules(data.modules || [])
  }, [])

  const fetchStatus = useCallback(async () => {
    const res = await fetch('/api/infra/status', { credentials: 'include' })
    if (!res.ok) {
      throw new Error(`Failed to load status (${res.status})`)
    }
    const data: StatusResponse = await res.json()
    setStatus(data)
  }, [])

  const loadAll = useCallback(async () => {
    setError(null)
    try {
      await Promise.all([fetchModules(), fetchStatus()])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load infrastructure data')
    }
  }, [fetchModules, fetchStatus])

  useEffect(() => {
    loadAll()
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [loadAll])

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      await loadAll()
    } catch {
      // loadAll sets error state internally
    } finally {
      setRefreshing(false)
    }
  }

  const handleToggle = async (moduleName: string, currentEnabled: boolean) => {
    setToggling(moduleName)
    try {
      const res = await fetch(`/api/infra/modules/${moduleName}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !currentEnabled }),
      })
      if (!res.ok) {
        setError(`Failed to toggle module (${res.status})`)
        return
      }
      await loadAll()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to toggle module')
    } finally {
      setToggling(null)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[50vh]">
        <RefreshCw size={24} className="animate-spin text-gray-400" />
      </div>
    )
  }

  const overallStatus = status?.overall || 'unknown'
  const overallCfg = statusConfig[overallStatus]
  const OverallIcon = overallCfg.icon

  // Build a map from module status results for quick lookup.
  const statusByName = new Map<string, ModuleResult>()
  if (status?.modules) {
    for (const m of status.modules) {
      statusByName.set(m.name, m)
    }
  }

  return (
    <div className="max-w-6xl mx-auto px-4 py-8">
      {/* Header */}
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-white">Infrastructure</h1>
          <p className="text-sm text-gray-400 mt-1">Monitor your services and infrastructure</p>
        </div>
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="flex items-center gap-2 px-4 py-2 rounded-lg bg-gray-800 text-gray-300 hover:text-white hover:bg-gray-700 transition-colors cursor-pointer disabled:opacity-50"
        >
          <RefreshCw size={16} className={refreshing ? 'animate-spin' : ''} />
          Refresh
        </button>
      </div>

      {/* Error banner */}
      {error && (
        <div className="flex items-center gap-3 px-4 py-3 rounded-lg border mb-4 bg-red-400/10 border-red-400/20">
          <XCircle size={18} className="text-red-400 shrink-0" />
          <span className="text-sm text-red-400">{error}</span>
          <button
            onClick={() => setError(null)}
            className="ml-auto text-red-400 hover:text-red-300 text-xs cursor-pointer"
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Overall status banner */}
      <div className={`flex items-center gap-3 px-4 py-3 rounded-lg border mb-8 ${overallCfg.bg} ${overallCfg.border}`}>
        <OverallIcon size={20} className={overallCfg.color} />
        <span className={`text-sm font-medium ${overallCfg.color}`}>
          Overall: {overallCfg.label}
        </span>
        {status?.modules && (
          <span className="text-xs text-gray-500 ml-auto">
            {status.modules.length} module{status.modules.length !== 1 ? 's' : ''} active
          </span>
        )}
      </div>

      {/* Module cards */}
      {modules.length === 0 ? (
        <div className="text-center py-16">
          <HelpCircle size={48} className="mx-auto text-gray-600 mb-4" />
          <h2 className="text-lg font-medium text-gray-400 mb-2">No modules configured</h2>
          <p className="text-sm text-gray-500">
            Infrastructure modules will appear here once configured.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {modules.map(mod => {
            const modStatus = statusByName.get(mod.name)
            const cfg = modStatus ? statusConfig[modStatus.status] : statusConfig.unknown
            const StatusIcon = cfg.icon
            const isToggling = toggling === mod.name

            return (
              <div
                key={mod.name}
                className={`rounded-lg border p-4 transition-colors ${
                  mod.enabled
                    ? `${cfg.bg} ${cfg.border}`
                    : 'bg-gray-800/50 border-gray-700/50 opacity-60'
                }`}
              >
                <div className="flex items-start justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <StatusIcon size={18} className={mod.enabled ? cfg.color : 'text-gray-500'} />
                    <h3 className="font-medium text-white">{mod.display_name}</h3>
                  </div>
                  <button
                    onClick={() => handleToggle(mod.name, mod.enabled)}
                    disabled={isToggling}
                    className="text-gray-400 hover:text-white transition-colors cursor-pointer disabled:opacity-50"
                    title={mod.enabled ? 'Disable module' : 'Enable module'}
                    aria-label={mod.enabled ? `Disable ${mod.display_name}` : `Enable ${mod.display_name}`}
                  >
                    {mod.enabled ? (
                      <ToggleRight size={20} className="text-green-400" />
                    ) : (
                      <ToggleLeft size={20} />
                    )}
                  </button>
                </div>

                <p className="text-xs text-gray-400 mb-3">{mod.description}</p>

                {mod.enabled && modStatus && (
                  <div className="text-xs text-gray-500">
                    {modStatus.message && (
                      <p className="mb-1">{modStatus.message}</p>
                    )}
                    <p>
                      Last checked:{' '}
                      {new Date(modStatus.checked_at).toLocaleString(undefined, {
                        dateStyle: 'short',
                        timeStyle: 'medium',
                      })}
                    </p>
                  </div>
                )}

                {!mod.enabled && (
                  <p className="text-xs text-gray-600">Module disabled</p>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
