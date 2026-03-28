# Hytte Wordfeud Solver

**Status**: Planning
**Date**: 2026-03-28

---

## Overview

A full Wordfeud solver page in Hytte: enter the board state and your tiles, get ranked move suggestions with scores. Combines ideas from your existing WordfeudHelper (C# pattern matcher) and the tile tracker concept, elevated to a proper board-aware solver.

## What We're Building

Three integrated tools on one page:

### 1. Board Editor
Visual 15×15 Wordfeud board where you manually enter the current game state. Click cells to place letters. The board includes the standard Wordfeud multiplier pattern (TW, DW, TL, DL cells).

### 2. Tile Rack + Available Tiles
- **Your rack**: Enter your 7 tiles (including blanks as `*`)
- **Tile tracker**: Shows remaining tiles not yet played. As you enter the board, the tracker subtracts placed letters from the full tile bag. Shows "X tiles left in bag" and probability hints.

### 3. Move Solver
Given the board + your rack, find all valid moves ranked by score. Show:
- Word, position, direction, total score
- Breakdown: base points + multiplier bonuses
- Highlight the move on the board preview

## Algorithm: From WordfeudHelper to Full Solver

Your existing WordfeudHelper does **pattern matching** — given letters, find valid words from the dictionary. That's step 1 of a solver. We need to add:

### Step 1: Dictionary (reuse from WordfeudHelper)
- Load nsf2025.txt (922K Norwegian words, already in your C# repo)
- Build a **trie** for O(log n) prefix lookups (critical for board scanning)
- Copy the dictionary file into Hytte's data directory
- Filter: 2-15 letters, normalize accented chars, uppercase

### Step 2: Anchor-Based Move Generation
The standard Scrabble/Wordfeud solver algorithm:

1. **Find anchors**: Empty cells adjacent to occupied cells (or the center star for first move)
2. **For each anchor + direction** (horizontal/vertical):
   - Walk left/up to find the leftmost possible start position
   - Generate all valid words from rack tiles that:
     a. Start at or before the anchor
     b. Pass through the anchor
     c. Don't conflict with existing letters
     d. Form valid cross-words perpendicular to placement
3. **Score each candidate** including multiplier tiles (2×L, 3×L, 2×W, 3×W)
4. **Rank by total score** descending

This is the same algorithm used by the Rust `wordfeud-solver` crate (~1ms per evaluation).

### Step 3: Scoring Engine
Port the letter values from WordfeudHelper's `Letter.cs` (Norwegian variant):

| Letter | Points | Count |
|--------|--------|-------|
| A, D, E, N, R, S, T | 1 | 5-7 each |
| I, L | 2 | 5 each |
| F, G, H, K, M, O, P | 3-5 | 2-4 each |
| B, U, V, Å | 4 | 3-4 each |
| Ø, Y | 5-6 | 2 each |
| Æ, J, W, X | 8 | 1 each |
| C, Z, Q | 10 | 1 each |
| Blank (*) | 0 | 2 |

Board multipliers (Wordfeud standard layout):
- **TW** (Triple Word): 8 cells
- **DW** (Double Word): 16 cells
- **TL** (Triple Letter): 12 cells
- **DL** (Double Letter): 24 cells

**7-tile bonus**: +40 points when all 7 rack tiles are used in one move.

### Step 4: Cross-Word Validation
When placing a word horizontally, each new tile may form a vertical word with existing adjacent tiles. ALL cross-words must be valid dictionary words. This is the trickiest part — the trie structure makes it fast.

## Implementation

### Backend: `internal/wordfeud/`

- **trie.go** — Trie data structure, loaded from dictionary file on startup
- **board.go** — 15×15 board representation, multiplier map, tile placement
- **solver.go** — Anchor-based move generator, cross-word validation
- **scoring.go** — Norwegian letter values, score calculator with multipliers
- **tiles.go** — Full Norwegian tile bag (104 tiles), remaining tile tracker
- **handlers.go** — HTTP handlers:
  - `POST /api/wordfeud/solve` — board state + rack → ranked moves
  - `POST /api/wordfeud/validate` — check if a word is in the dictionary
  - `GET /api/wordfeud/tiles` — Norwegian tile distribution
- **dictionary.go** — Load and serve the NSF dictionary

**Performance target**: <500ms for full solve with 7 tiles on a populated board. Go is fast enough for this — no need for Rust.

### Frontend: WordfeudPage.tsx

**Layout (desktop — this needs a big screen):**

```
┌──────────────────────────────┬────────────────────┐
│                              │ Your Tiles         │
│    15×15 Board Editor        │ [A][E][R][S][T][ ]│
│    (click to place letters)  │                    │
│                              │ Remaining Tiles    │
│    Color-coded multipliers:  │ A:3 B:2 C:1 D:4.. │
│    TW=red DW=pink           │ 52 tiles in bag    │
│    TL=blue DL=lightblue     │                    │
│                              ├────────────────────┤
│                              │ Top Moves          │
│                              │ 1. AREST H8→ 42pt │
│                              │ 2. RASTE 7G↓ 38pt │
│                              │ 3. RASE  J5→ 28pt │
│                              │ ...                │
└──────────────────────────────┴────────────────────┘
```

**Board interaction:**
- Click cell → type letter (keyboard focused)
- Arrow keys to navigate
- Delete/backspace to clear
- Color-coded: your tiles vs opponent's tiles (optional)
- Multiplier cells visible as subtle background colors

**Move list:**
- Click a move → highlight it on the board preview
- Show score breakdown on hover/tap
- "Play" button to apply the move to the board (updates state)

**Mobile**: This is inherently a desktop feature. On mobile, show a simplified word finder (like the existing WordfeudHelper) without the full board.

### Dictionary File

Copy `nsf2025.txt` from `C:\Users\robs\source\repos\WordfeudHelper\Data\` to `data/nsf2025.txt` in the Hytte repo (or serve it from the backend).

**Important**: The actual Wordfeud dictionary differs from NSF. Some words that NSF allows, Wordfeud rejects (and vice versa). This solver will be an approximation — flag this in the UI: "Based on NSF dictionary — may differ from Wordfeud's word list."

## Feature Flag

`wordfeud` — default false. This is a niche tool, not everyone needs it.

## i18n

Minimal — the game itself uses Norwegian words. UI chrome in all 3 locales:
- `wordfeud.title`, `wordfeud.solve`, `wordfeud.rack`, `wordfeud.remaining`
- `wordfeud.noMoves`, `wordfeud.topMoves`, `wordfeud.score`

## Phase Plan

### Phase 1: Word finder (port WordfeudHelper)
- Dictionary loading (trie)
- Pattern matching: find words from available letters
- Simple UI: enter letters, get word suggestions ranked by base points
- Basically your C# app in the browser

### Phase 2: Board + solver
- 15×15 board editor with multiplier layout
- Anchor-based move generator
- Full scoring with board multipliers
- Cross-word validation
- Top moves display with board highlighting

### Phase 3: Tile tracker
- Full tile bag tracking
- Remaining tiles display (updated as board is filled)
- Probability hints ("opponent likely has high-value tiles")

### Phase 4: Game state persistence
- Save/load board states
- Multiple concurrent games
- Move history (undo/redo)
- Optional: import game state from Wordfeud API (community reverse-engineered API)

## Recommendation

**Start with Phase 1** — it's the most useful and simplest. Your kids (and you) can use it immediately as a word finder during games. The full board solver (Phase 2) is the impressive feature but requires more work. Phase 4 (API integration) is the riskiest due to reverse-engineered APIs that could break.

The trie-based dictionary in Go will be fast enough — the Rust crate achieves 1ms, and Go will be under 100ms easily. No need for WebAssembly or any exotic runtime.

## Decisions

1. **Language**: Norwegian only. Single dictionary (nsf2025.txt).
2. **Mobile**: Nice-to-have, not critical. Desktop-first for the board editor. Mobile gets the word finder (Phase 1).
3. **Random board**: No — standard Wordfeud multiplier layout only.
4. **Multiplayer tracking**: Yes — track both players' scores and whose turn it is. Simple state: two player names, running scores, turn indicator. Easy to add alongside the board state.
