import { useEffect, useState } from 'react'
import { type Person, fetchFamily } from './tripsApi'

/** The family roster, loaded once per page; reload() fetches it again. */
export function useFamily() {
  const [people, setPeople] = useState<Person[]>([])
  const [version, setVersion] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    fetchFamily(controller.signal).then(d => setPeople(d.people ?? [])).catch(() => {})
    return () => controller.abort()
  }, [version])
  return { people, reload: () => setVersion(v => v + 1) }
}
