category: Security
- **Encrypt claude_cli_path preference at rest** - The claude_cli_path user preference is now encrypted using AES-256-GCM before storage and decrypted on read, matching the encryption pattern used for other sensitive fields. Legacy plaintext values are handled gracefully. (Hytte-7ozj)
