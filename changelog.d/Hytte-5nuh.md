category: Security
- **Encrypt existing plaintext data on startup** - A one-time migration encrypts all pre-existing sensitive fields (notes, lactate test data, push subscription keys, VAPID keys, and analysis data) using AES-256-GCM encryption at rest. Already-encrypted values are detected and skipped. (Hytte-5nuh)
