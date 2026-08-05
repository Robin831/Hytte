import { useState, type ReactNode } from 'react'

interface PokemonImageProps {
  src: string | null | undefined
  alt: string
  className?: string
  loading?: 'lazy' | 'eager'
  // fallback renders in place of the <img> when there is no URL or the URL
  // fails to load. It is a node rather than a string so all user-facing copy
  // (and its i18n) stays with the caller.
  fallback: ReactNode
}

// PokemonImage renders a remote pokemontcg.io image and swaps in `fallback`
// when the image 404s or otherwise fails, so grids show readable text instead
// of the browser's broken-image icon. The error flag resets during render when
// `src` changes — a new URL deserves a fresh attempt, and doing it here rather
// than in an effect avoids a frame where the stale fallback is still painted.
export default function PokemonImage({ src, alt, className, loading = 'lazy', fallback }: PokemonImageProps) {
  const [errored, setErrored] = useState(false)
  const [prevUrl, setPrevUrl] = useState(src)
  if (src !== prevUrl) {
    setPrevUrl(src)
    setErrored(false)
  }
  if (!src || errored) return <>{fallback}</>
  return (
    <img
      src={src}
      alt={alt}
      className={className}
      loading={loading}
      onError={() => setErrored(true)}
    />
  )
}
