// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import RejectedLabel from './RejectedLabel'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { reason?: string }) =>
      key === 'reject.reasonDisplay' ? `“${opts?.reason}”` : key === 'myChores.rejected' ? 'Rejected — try again' : key,
    i18n: { language: 'en' },
  }),
}))

describe('RejectedLabel', () => {
  it('shows only the status when there is no reason', () => {
    render(<RejectedLabel />)
    expect(screen.getByText('Rejected — try again')).toBeInTheDocument()
    expect(screen.queryByTestId('rejection-reason')).toBeNull()
  })

  it('hides a whitespace-only reason', () => {
    render(<RejectedLabel reason={'   \n '} />)
    expect(screen.getByText('Rejected — try again')).toBeInTheDocument()
    expect(screen.queryByTestId('rejection-reason')).toBeNull()
  })

  it('shows the trimmed reason in quotes', () => {
    render(<RejectedLabel reason="  Not finished " />)
    expect(screen.getByTestId('rejection-reason')).toHaveTextContent('“Not finished”')
  })
})
