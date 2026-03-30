category: Added
- **KioskAuth middleware** - Adds `internal/kiosk/auth.go` with HTTP middleware that authenticates kiosk/display clients via a `?token=` query parameter. Hashes the token with SHA-256, validates against `kiosk_tokens`, checks expiry, updates `last_used_at`, and injects the parsed config JSON into the request context. Includes a `GetKioskConfig(ctx)` accessor. (Hytte-tc9y)
