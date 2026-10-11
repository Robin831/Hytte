import { type ReactNode } from 'react'
import { Link } from 'react-router'

/** The "see everything" link at the foot of a widget. */
export default function WidgetLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to} className="mt-4 block text-xs text-blue-400 hover:text-blue-300">
      {children}
    </Link>
  )
}
