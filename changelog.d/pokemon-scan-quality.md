category: Fixed
- **Pokémon scanner capture quality** - Bump auto-captured JPEG quality from 0.85 to 0.95. The set symbol and collector number on the bottom edge of a TCG card are tiny (≈5 px tall in the source) and the 0.85 compression was destroying enough detail that Claude vision returned confidence 0 even on clearly-framed cards. A small file-size hit for a big readability win.
