category: Added
- **Google Calendar OAuth scopes** - Expanded Google OAuth to request calendar.readonly and calendar.events scopes with offline access for refresh tokens. Google OAuth tokens are now persisted (encrypted at rest) in a new google_tokens table, enabling future Calendar API integration. Existing users will need to re-consent on next login. (Hytte-f9ui)
