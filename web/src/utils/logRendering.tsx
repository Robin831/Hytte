import type { AnchorHTMLAttributes } from 'react'

export function getSafeHref(href?: string): string | undefined {
  if (!href) return undefined
  try {
    const url = new URL(href, 'http://localhost')
    const protocol = url.protocol.toLowerCase()
    return ['http:', 'https:', 'mailto:'].includes(protocol) ? href : undefined
  } catch {
    return undefined
  }
}

export const markdownLinkComponents = {
  a: ({ href, children }: AnchorHTMLAttributes<HTMLAnchorElement>) => {
    const safeHref = getSafeHref(typeof href === 'string' ? href : undefined)
    if (!safeHref) return <span>{children}</span>
    return (
      <a href={safeHref} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
}

export function hasCodeFence(text: string): boolean {
  return text.includes('```')
}
