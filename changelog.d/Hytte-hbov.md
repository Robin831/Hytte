category: Added
- **Netatmo OAuth2 client and token storage** - Add `internal/netatmo` package with OAuth2 authorization URL builder, authorization code exchange, and transparent token refresh. Tokens are encrypted at rest in `netatmo_oauth_tokens` table. Add `netatmo` feature flag (default off). (Hytte-hbov)
