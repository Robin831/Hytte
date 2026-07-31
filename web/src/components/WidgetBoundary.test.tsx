// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import WidgetBoundary from './WidgetBoundary'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) => {
      if (key === 'widgetError.title') return 'Widget unavailable'
      if (key === 'widgetError.titleWithLabel') return `${opts?.label} unavailable`
      if (key === 'widgetError.message') return 'This widget failed to load.'
      if (key === 'widgetError.retry') return 'Retry'
      return key
    },
    i18n: { language: 'en' },
  }),
}))

function Boom() {
  throw new Error('widget exploded')
}

let flakyShouldThrow = true

function Flaky() {
  if (flakyShouldThrow) throw new Error('flaky exploded')
  return <div>recovered content</div>
}

describe('WidgetBoundary', () => {
  let consoleError: MockInstance

  beforeEach(() => {
    flakyShouldThrow = true
    // React logs caught render errors itself; silence the noise but keep the calls.
    consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    consoleError.mockRestore()
  })

  it('renders children when nothing throws', () => {
    render(
      <WidgetBoundary label="Weather">
        <div>widget body</div>
      </WidgetBoundary>,
    )

    expect(screen.getByText('widget body')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('catches a throwing child and shows the fallback tile', () => {
    render(
      <WidgetBoundary label="Weather">
        <Boom />
      </WidgetBoundary>,
    )

    const alert = screen.getByRole('alert')
    expect(alert.className).toContain('bg-gray-800')
    expect(alert.className).toContain('rounded-xl')
    expect(alert.className).toContain('p-6')
    expect(screen.getByText('Weather unavailable')).toBeTruthy()
    expect(screen.getByText('This widget failed to load.')).toBeTruthy()
    expect(screen.getByRole('button', { name: /retry/i })).toBeTruthy()
  })

  it('logs the error and component stack to the console', () => {
    render(
      <WidgetBoundary label="Weather">
        <Boom />
      </WidgetBoundary>,
    )

    const boundaryLog = consoleError.mock.calls.find(
      (call) => typeof call[0] === 'string' && call[0].startsWith('[WidgetBoundary]'),
    )
    expect(boundaryLog).toBeTruthy()
    expect(boundaryLog?.[1]).toBe('Weather')
    expect((boundaryLog?.[2] as Error).message).toBe('widget exploded')
    expect(typeof boundaryLog?.[3]).toBe('string')
  })

  it('falls back to a generic title when no label is given', () => {
    render(
      <WidgetBoundary>
        <Boom />
      </WidgetBoundary>,
    )

    expect(screen.getByText('Widget unavailable')).toBeTruthy()
  })

  it('remounts the widget on retry and renders it once the cause is gone', () => {
    render(
      <WidgetBoundary label="Fitness">
        <Flaky />
      </WidgetBoundary>,
    )

    expect(screen.getByRole('alert')).toBeTruthy()

    flakyShouldThrow = false
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))

    expect(screen.getByText('recovered content')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('keeps showing the fallback when retry does not fix the cause', () => {
    render(
      <WidgetBoundary label="Fitness">
        <Flaky />
      </WidgetBoundary>,
    )

    fireEvent.click(screen.getByRole('button', { name: /retry/i }))

    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getByText('Fitness unavailable')).toBeTruthy()
  })

  it('isolates the failure so sibling widgets still render', () => {
    render(
      <div>
        <WidgetBoundary label="Weather">
          <Boom />
        </WidgetBoundary>
        <WidgetBoundary label="Daylight">
          <div>sibling body</div>
        </WidgetBoundary>
      </div>,
    )

    expect(screen.getByText('Weather unavailable')).toBeTruthy()
    expect(screen.getByText('sibling body')).toBeTruthy()
    expect(screen.getAllByRole('alert')).toHaveLength(1)
  })

  it('applies extra classes to the fallback tile so grid spans are preserved', () => {
    render(
      <WidgetBoundary className="col-span-full">
        <Boom />
      </WidgetBoundary>,
    )

    expect(screen.getByRole('alert').className).toContain('col-span-full')
  })
})
