import { render } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router'
import KioskPage from '../pages/KioskPage'

// Shared render/timing helpers for the KioskPage suites (KioskPage.test.tsx and
// KioskPage.mockData.test.tsx) so the plumbing only has to be kept in sync in
// one place when the kiosk route or the polling timing changes. The i18n mock
// helper lives in ./kioskI18n so it can be imported from a vi.mock factory.

export function renderKiosk(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/kiosk" element={<KioskPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

export async function flushMicrotasks() {
  for (let i = 0; i < 5; i++) {
    await Promise.resolve()
  }
}
