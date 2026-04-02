category: Fixed
- **Wordfeud solver no longer suggests non-dictionary words** - The solver's left-part generation traversed the trie in placement order (anchor→left) instead of word reading order (left→right), causing it to validate scrambled prefixes and emit invalid words like JLOEDØD. (Hytte-ym90)
