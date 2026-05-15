category: Fixed
- **Pokémon scanner timing out** - Bump the per-scan frontend timeout from 30 s to 120 s. Real-world Claude vision calls on a card image consistently take 60–90 s; the 30 s cap was making every scan time out client-side even when the backend would have returned a valid match seconds later.
