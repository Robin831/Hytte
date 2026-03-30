category: Fixed
- **Fix encrypted sibling name in team chore Join button** - The `/api/allowance/my/siblings` endpoint was returning raw encrypted ciphertext for sibling nicknames instead of the decrypted value, causing the Join button to render "Join enc:..." instead of the actual name. (Hytte-ka4x)
