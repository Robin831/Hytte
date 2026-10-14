// @vitest-environment happy-dom
import { useEffect, useState } from 'react'
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, act, fireEvent, cleanup } from '@testing-library/react'
import MessageRow from './MessageRow'
import type { Message } from './types'

const TRANSLATIONS: Record<string, string> = {
  'input.copyMessage': 'Copy message',
  'input.regenerate': 'Regenerate response',
  'input.editMessage': 'Edit message',
  streamingIndicator: 'Streaming…',
}

function stableT(key: string): string {
  return TRANSLATIONS[key] ?? key
}

// MessageRow calls useTranslation exactly once per render, so counting calls
// to the mocked hook counts MessageRow renders (nothing else in these trees
// uses it).
const { useTranslationMock } = vi.hoisted(() => ({
  useTranslationMock: vi.fn(() => ({ t: stableT, i18n: { language: 'en' } })),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => useTranslationMock(),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('react-syntax-highlighter', () => ({
  Prism: ({ children, language }: { children: string; language: string }) => (
    <pre data-testid="highlighted-code" data-language={language}>{children}</pre>
  ),
}))
vi.mock('react-syntax-highlighter/dist/esm/styles/prism', () => ({
  vscDarkPlus: {},
}))

function makeMessage(id: number, overrides: Partial<Message> = {}): Message {
  return {
    id,
    conversation_id: 1,
    role: id % 2 === 1 ? 'user' : 'assistant',
    content: `message ${id}`,
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

// Stable callbacks shared by every row, as Chat passes them.
const onEdit = vi.fn()
const onRegenerate = vi.fn()

let setThread: (update: (prev: Message[]) => Message[]) => void = () => {}

function Thread({ initial }: { initial: Message[] }) {
  const [messages, setMessages] = useState(initial)
  useEffect(() => {
    setThread = setMessages
  }, [])
  return (
    <>
      {messages.map(m => (
        <MessageRow
          key={m.id}
          message={m}
          showEdit={m.role === 'user'}
          onEdit={onEdit}
          onRegenerate={onRegenerate}
        />
      ))}
    </>
  )
}

beforeEach(() => {
  useTranslationMock.mockClear()
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('MessageRow memoization', () => {
  const N = 6

  it('re-renders only the row whose message changed when a token is appended', () => {
    render(<Thread initial={Array.from({ length: N }, (_, i) => makeMessage(i + 1))} />)
    expect(useTranslationMock).toHaveBeenCalledTimes(N)
    useTranslationMock.mockClear()

    // What useChatStream does on a token delta: a new object for the
    // streaming message only, every other message keeps its reference.
    act(() => {
      setThread(prev =>
        prev.map((m, i) => (i === prev.length - 1 ? { ...m, content: m.content + ' more' } : m)),
      )
    })

    expect(useTranslationMock).toHaveBeenCalledTimes(1)
    expect(screen.getByText(`message ${N} more`)).toBeInTheDocument()
  })

  it('re-renders every row when every message object is replaced', () => {
    render(<Thread initial={Array.from({ length: N }, (_, i) => makeMessage(i + 1))} />)
    useTranslationMock.mockClear()

    act(() => {
      setThread(prev => prev.map(m => ({ ...m })))
    })

    expect(useTranslationMock).toHaveBeenCalledTimes(N)
  })
})

describe('MessageRow rendering', () => {
  it('renders fenced code blocks through the syntax highlighter', () => {
    const content = 'Here:\n\n```ts\nconst x = 1\nconst y = 2\n```\n'
    render(<MessageRow message={makeMessage(2, { content })} />)
    const code = screen.getByTestId('highlighted-code')
    expect(code).toHaveAttribute('data-language', 'ts')
    expect(code.textContent).toBe('const x = 1\nconst y = 2')
  })

  it('renders inline code without the highlighter', () => {
    render(<MessageRow message={makeMessage(2, { content: 'use `npm ci` here' })} />)
    expect(screen.queryByTestId('highlighted-code')).not.toBeInTheDocument()
    expect(screen.getByText('npm ci').tagName).toBe('CODE')
  })

  it('shows the streaming indicator for a pending placeholder', () => {
    render(<MessageRow message={makeMessage(-2, { role: 'assistant', content: '' })} pending />)
    expect(screen.getByText('Streaming…')).toBeInTheDocument()
    expect(screen.queryByLabelText('Copy message')).not.toBeInTheDocument()
  })

  it('shows the copied state for 2 seconds after copying', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

    const { container } = render(<MessageRow message={makeMessage(2, { content: 'copy me' })} />)
    expect(container.querySelector('.lucide-check-check')).toBeNull()

    await act(async () => {
      fireEvent.click(screen.getByLabelText('Copy message'))
    })
    expect(writeText).toHaveBeenCalledWith('copy me')
    expect(container.querySelector('.lucide-check-check')).not.toBeNull()

    act(() => {
      vi.advanceTimersByTime(1999)
    })
    expect(container.querySelector('.lucide-check-check')).not.toBeNull()

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(container.querySelector('.lucide-check-check')).toBeNull()
  })

  it('passes the message to onEdit and onRegenerate', () => {
    const user = makeMessage(1)
    const assistant = makeMessage(2)
    render(
      <>
        <MessageRow message={user} showEdit onEdit={onEdit} />
        <MessageRow message={assistant} showRegenerate onRegenerate={onRegenerate} />
      </>,
    )
    fireEvent.click(screen.getByLabelText('Edit message'))
    fireEvent.click(screen.getByLabelText('Regenerate response'))
    expect(onEdit).toHaveBeenCalledWith(user)
    expect(onRegenerate).toHaveBeenCalledWith(assistant)
  })
})
