category: Fixed
- **Infra page no longer flickers stale data on rapid actions** - Page-level and per-module fetches on `/infra` now share an `AbortController` that cancels in-flight requests when the user refreshes, toggles a module, or navigates away. This prevents stale responses from overwriting fresh state and silences phantom `AbortError` toasts on unmount. (Hytte-9dz6)
