import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { GitBranch, CheckCircle2, XCircle } from 'lucide-react'
import { useAuth } from '../../auth'
import Widget from '../Widget'
import { timeAgo } from '../../utils/timeAgo'

interface WorkflowRun {
  id: number
  name: string
  status: string
  conclusion: string
  branch: string
  created_at: string
  html_url: string
}

interface RepoResult {
  owner: string
  repo: string
  status: string
  error?: string
  runs: WorkflowRun[]
}

interface ModuleDetailResponse {
  name: string
  status: string
  details?: {
    repos?: RepoResult[]
  }
}

export default function GitHubStatusWidget() {
  const { user } = useAuth()
  const [repos, setRepos] = useState<RepoResult[]>([])
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!user) return
    const controller = new AbortController()

    fetch('/api/infra/modules/github_actions/detail', {
      credentials: 'include',
      signal: controller.signal,
    })
      .then(r => r.ok ? r.json() as Promise<ModuleDetailResponse> : null)
      .then(data => {
        if (data?.details?.repos) setRepos(data.details.repos)
        setLoaded(true)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        console.error('GitHubStatusWidget fetch error:', err)
        setLoaded(true)
      })

    return () => { controller.abort() }
  }, [user])

  if (!user || !loaded) return null
  if (loaded && repos.length === 0) return null

  return (
    <Widget title="GitHub Actions">
      <div className="space-y-3">
        {repos.map(repo => {
          const latestRun = repo.runs?.[0]
          const hasFailure = repo.runs?.some(r => r.conclusion === 'failure')

          return (
            <div key={`${repo.owner}/${repo.repo}`} className="space-y-1">
              <div className="flex items-center gap-2">
                <GitBranch size={14} className="text-gray-400" />
                <span className="text-sm text-gray-200 truncate">
                  {repo.owner}/{repo.repo}
                </span>
                {repo.error ? (
                  <XCircle size={14} className="text-red-400 shrink-0 ml-auto" />
                ) : hasFailure ? (
                  <XCircle size={14} className="text-red-400 shrink-0 ml-auto" />
                ) : (
                  <CheckCircle2 size={14} className="text-green-400 shrink-0 ml-auto" />
                )}
              </div>
              {latestRun && !repo.error && (
                <p className="text-xs text-gray-500 pl-5 truncate">
                  {latestRun.name} · {latestRun.conclusion} · {timeAgo(latestRun.created_at)}
                </p>
              )}
              {repo.error && (
                <p className="text-xs text-red-400/70 pl-5 truncate">{repo.error}</p>
              )}
            </div>
          )
        })}
      </div>

      <Link
        to="/infra"
        className="inline-block mt-3 text-xs text-blue-400 hover:text-blue-300"
      >
        Infra dashboard →
      </Link>
    </Widget>
  )
}
