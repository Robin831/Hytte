// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import RejectReasonDialog from './RejectReasonDialog'

const TRANSLATIONS: Record<string, string> = {
  'reject.title': 'Reject completion',
  'reject.titleBatch': 'Reject {{count}} completions',
  'reject.reasonOptional': 'Reason (optional)',
  'reject.placeholder': 'Tell them what to fix',
  'reject.cancel': 'Cancel',
  'reject.confirm': 'Reject',
  'reject.chips.notFinished': 'Not finished',
  'reject.chips.redoProperly': 'Do it again properly',
  'reject.chips.wrongDay': 'Wrong day',
}

function mockT(key: string, opts?: Record<string, unknown>): string {
  const template = TRANSLATIONS[key] ?? key
  if (!opts) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(opts[name] ?? ''))
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
}))

function reasonField() {
  return screen.getByLabelText('Reason (optional)') as HTMLTextAreaElement
}

describe('RejectReasonDialog', () => {
  it('renders nothing while closed', () => {
    render(<RejectReasonDialog open={false} onCancel={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('uses the single title by default and the batch title above one', () => {
    const { rerender } = render(<RejectReasonDialog open onCancel={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByRole('dialog', { name: 'Reject completion' })).toBeInTheDocument()

    rerender(<RejectReasonDialog open count={3} onCancel={vi.fn()} onConfirm={vi.fn()} />)
    expect(screen.getByRole('dialog', { name: 'Reject 3 completions' })).toBeInTheDocument()
  })

  it('fills the textarea from a chip and marks it pressed', () => {
    const onConfirm = vi.fn()
    render(<RejectReasonDialog open onCancel={vi.fn()} onConfirm={onConfirm} />)

    const chip = screen.getByRole('button', { name: 'Wrong day' })
    expect(chip).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(chip)

    expect(reasonField().value).toBe('Wrong day')
    expect(chip).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Not finished' })).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(screen.getByRole('button', { name: 'Reject' }))
    expect(onConfirm).toHaveBeenCalledWith('Wrong day')
  })

  it('confirms with the trimmed free-text reason', () => {
    const onConfirm = vi.fn()
    render(<RejectReasonDialog open onCancel={vi.fn()} onConfirm={onConfirm} />)

    fireEvent.change(reasonField(), { target: { value: '  Dust under the bed  ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Reject' }))

    expect(onConfirm).toHaveBeenCalledWith('Dust under the bed')
  })

  it('resets the reason when reopened', () => {
    const props = { onCancel: vi.fn(), onConfirm: vi.fn() }
    const { rerender } = render(<RejectReasonDialog open {...props} />)
    fireEvent.change(reasonField(), { target: { value: 'Not good enough' } })

    rerender(<RejectReasonDialog open={false} {...props} />)
    rerender(<RejectReasonDialog open {...props} />)

    expect(reasonField().value).toBe('')
  })

  it('disables the buttons and ignores cancel while submitting', () => {
    const onCancel = vi.fn()
    render(<RejectReasonDialog open submitting error="Action failed" onCancel={onCancel} onConfirm={vi.fn()} />)

    const cancel = screen.getByRole('button', { name: 'Cancel' })
    expect(cancel).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeDisabled()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onCancel).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('Action failed')
  })
})
