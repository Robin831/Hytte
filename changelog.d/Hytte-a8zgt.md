category: Security
- **Chat transcripts are now encrypted at rest** - Chat message content and conversation titles are encrypted with AES-256-GCM before being written to SQLite, matching the convention already used for notes and workout analyses. Existing plaintext rows keep rendering and are re-encrypted the next time they are written. (Hytte-a8zgt)
