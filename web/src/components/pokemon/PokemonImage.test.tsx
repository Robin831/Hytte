// @vitest-environment happy-dom
import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import PokemonImage from './PokemonImage'

function renderImage(src: string | null | undefined) {
  return render(
    <PokemonImage
      src={src}
      alt="card"
      className="object-contain"
      fallback={<span>001</span>}
    />,
  )
}

describe('PokemonImage', () => {
  it('renders the image when a URL is given', () => {
    renderImage('https://example.com/a.png')
    const img = screen.getByAltText('card')
    expect(img).toHaveAttribute('src', 'https://example.com/a.png')
    expect(img).toHaveAttribute('loading', 'lazy')
    expect(screen.queryByText('001')).not.toBeInTheDocument()
  })

  it('renders the fallback when there is no URL', () => {
    renderImage('')
    expect(screen.getByText('001')).toBeInTheDocument()
    expect(screen.queryByAltText('card')).not.toBeInTheDocument()
  })

  it('swaps in the fallback when the image fails to load', () => {
    renderImage('https://example.com/missing.png')
    fireEvent.error(screen.getByAltText('card'))
    expect(screen.getByText('001')).toBeInTheDocument()
    expect(screen.queryByAltText('card')).not.toBeInTheDocument()
  })

  it('clears the error state when src changes', () => {
    const { rerender } = renderImage('https://example.com/missing.png')
    fireEvent.error(screen.getByAltText('card'))
    expect(screen.getByText('001')).toBeInTheDocument()

    rerender(
      <PokemonImage
        src="https://example.com/working.png"
        alt="card"
        className="object-contain"
        fallback={<span>001</span>}
      />,
    )
    const img = screen.getByAltText('card')
    expect(img).toHaveAttribute('src', 'https://example.com/working.png')
    expect(screen.queryByText('001')).not.toBeInTheDocument()
  })
})
