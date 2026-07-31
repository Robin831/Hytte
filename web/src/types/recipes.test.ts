import { describe, it, expect } from 'vitest'
import { formatQuantity, ingredientLine, scaleQuantity, type RecipeIngredient } from './recipes'

function ingredient(overrides: Partial<RecipeIngredient>): RecipeIngredient {
  return {
    id: 1,
    position: 1,
    text: '',
    quantity: 0,
    unit: '',
    name: '',
    ...overrides,
  }
}

describe('scaleQuantity', () => {
  it('scales from the recipe yield to the chosen portions', () => {
    expect(scaleQuantity(400, 4, 8)).toBe(800)
    expect(scaleQuantity(400, 4, 3)).toBe(300)
    expect(scaleQuantity(2, 4, 1)).toBe(0.5)
  })

  it('leaves the quantity alone when there is nothing to scale by', () => {
    expect(scaleQuantity(400, 0, 8)).toBe(400)
    expect(scaleQuantity(400, 4, 0)).toBe(400)
  })
})

describe('formatQuantity', () => {
  it('renders whole numbers, fractions and mixed numbers', () => {
    expect(formatQuantity(3)).toBe('3')
    expect(formatQuantity(0.5)).toBe('1/2')
    expect(formatQuantity(1.5)).toBe('1 1/2')
    expect(formatQuantity(2 / 3)).toBe('2/3')
    expect(formatQuantity(0.125)).toBe('1/8')
  })

  it('falls back to two decimals, and renders no amount at all for zero', () => {
    expect(formatQuantity(0.6)).toBe('0.6')
    expect(formatQuantity(0)).toBe('')
    expect(formatQuantity(Number.NaN)).toBe('')
  })
})

describe('ingredientLine', () => {
  const cod = ingredient({ text: '400 g cod, cubed', quantity: 400, unit: 'g', name: 'cod' })

  it('shows the written line verbatim at the recipe’s own yield', () => {
    expect(ingredientLine(cod, 4, 4)).toBe('400 g cod, cubed')
  })

  it('keeps prep and parenthetical detail when the amount is scaled', () => {
    expect(ingredientLine(cod, 4, 8)).toBe('800 g cod, cubed')

    const cream = ingredient({
      text: '2 dl cream (room temperature)',
      quantity: 2,
      unit: 'dl',
      name: 'cream',
    })
    expect(ingredientLine(cream, 4, 3)).toBe('1 1/2 dl cream (room temperature)')
  })

  it('rewrites the amount wherever the line puts it, in the notation it uses', () => {
    const written = ingredient({ text: 'Cream, 1/2 dl', quantity: 0.5, unit: 'dl', name: 'cream' })
    expect(ingredientLine(written, 2, 4)).toBe('Cream, 1 dl')

    const unicode = ingredient({ text: '½ ts salt', quantity: 0.5, unit: 'ts', name: 'salt' })
    expect(ingredientLine(unicode, 2, 4)).toBe('1 ts salt')

    const decimal = ingredient({ text: '0,5 l milk', quantity: 0.5, unit: 'l', name: 'milk' })
    expect(ingredientLine(decimal, 2, 4)).toBe('1 l milk')
  })

  it('scales both ends of a written range', () => {
    const chilli = ingredient({ text: '2-3 chillies', quantity: 2, unit: '', name: 'chillies' })
    expect(ingredientLine(chilli, 2, 4)).toBe('4-6 chillies')
  })

  it('does not touch a number that is not the amount', () => {
    const eggs = ingredient({ text: '2 eggs, size 3', quantity: 2, unit: '', name: 'eggs' })
    expect(ingredientLine(eggs, 2, 4)).toBe('4 eggs, size 3')
  })

  it('rebuilds the line when the written amount cannot be found', () => {
    const parsed = ingredient({ text: 'a good glug of oil', quantity: 2, unit: 'tbsp', name: 'oil' })
    expect(ingredientLine(parsed, 4, 8)).toBe('4 tbsp oil')
  })

  it('shows lines without an amount as written', () => {
    const salt = ingredient({ text: 'salt', quantity: 0, unit: '', name: 'salt' })
    expect(ingredientLine(salt, 4, 8)).toBe('salt')

    const nameOnly = ingredient({ text: '', quantity: 0, unit: '', name: 'pepper' })
    expect(ingredientLine(nameOnly, 4, 8)).toBe('pepper')
  })
})
