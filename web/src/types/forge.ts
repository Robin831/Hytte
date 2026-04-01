export interface BeadDependency {
  id: string
  title: string
  status: string
  priority: number
  issue_type: string
  dependency_type: string
}

export interface BeadDetail {
  id: string
  title: string
  description: string
  notes?: string
  design?: string
  acceptance_criteria?: string
  status: string
  priority: number
  issue_type: string
  owner: string
  assignee?: string
  created_at: string
  created_by: string
  updated_at: string
  closed_at?: string
  close_reason?: string
  labels: string[]
  comments: Record<string, unknown>[]
  dependencies: BeadDependency[]
  dependents: BeadDependency[]
}
