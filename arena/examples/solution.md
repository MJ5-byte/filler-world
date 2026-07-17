# Filler Bot - AI Territory Control Game

## Overview

Filler is an algorithmic game where two bots compete to control territory on a grid by placing randomly-shaped pieces. This bot implements an advanced strategy that can defeat all standard opponents, including the "unbeatable" terminator.

---

## Table of Contents

1. [Game Rules](#game-rules)
2. [Algorithm](#algorithm)
3. [Scoring Formula](#scoring-formula)
4. [Data Structures](#data-structures)
5. [I/O Format](#io-format)
6. [Building & Running](#building--running)
7. [Performance Results](#performance-results)
8. [Why This Strategy Works](#why-this-strategy-works)

---

## Game Rules

### Objective
Occupy the most cells on the grid (Anfield) by placing pieces strategically.

### Placement Rules
| Rule | Description |
|------|-------------|
| Overlap | Exactly **ONE** cell must overlap your existing territory |
| Enemy | **ZERO** overlap with enemy cells allowed |
| Bounds | Piece must stay within map boundaries |
| Fallback | If no valid placement exists, output `0 0` |

### Player Symbols
| Player | Current Turn | Previous Territory |
|--------|--------------|-------------------|
| Player 1 | `a` | `@` |
| Player 2 | `s` | `$` |

---

## Algorithm

### Core Strategy: Heat Map + Territory Control

The bot uses a **multi-factor scoring system** to evaluate every possible placement and choose the optimal move.

### Step 1: Build Distance Maps (BFS)

```
For each cell, compute:
├── heat_map[r][c]    = distance to nearest ENEMY cell
└── my_heat_map[r][c] = distance to nearest OWN cell
```

Using **Breadth-First Search (BFS)** from all enemy/own cells simultaneously ensures O(n) computation.

```rust
fn compute_distance_map(&self, starts: &[(usize, usize)]) -> Vec<Vec<i32>> {
    // BFS from all starting positions
    // Returns distance from each cell to nearest start
}
```

### Step 2: Identify Frontier

```
frontier_map[r][c] = true if cell is adjacent to enemy territory
```

These are **critical blocking positions**.

### Step 3: Evaluate All Valid Placements

```
For each position (row, col):
    1. Check if piece can be legally placed
    2. Calculate all scoring factors
    3. Compute weighted total score
    4. Track best placement
```

### Step 4: Output Best Move

```
Print: "column row\n"
```

---

## Scoring Formula

### The Winning Formula

```rust
score = contested * 100      // Claim disputed territory
      + frontier * 80        // Block enemy expansion
      + territory * 50       // Control strategic positions
      + enemy_adj * 30       // Apply pressure
      + compact * 10         // Stay connected
      - heat * 5             // Prefer closer to enemy
```

### Scoring Components Explained

#### 1. Contested Score (Weight: 100) 🎯
**Purpose:** Prioritizes cells that are closer to the enemy than to us.

```rust
if enemy_dist <= my_dist {
    contested += (my_dist - enemy_dist + 1) * 2;
}
```

**Why:** Claims territory before the enemy can reach it.

---

#### 2. Frontier Score (Weight: 80) 🚧
**Purpose:** Counts piece cells that land directly adjacent to enemy.

```rust
if frontier_map[r][c] == true {
    frontier += 1;
}
```

**Why:** Blocks enemy expansion paths directly.

---

#### 3. Territory Control Score (Weight: 50) ⚔️
**Purpose:** Bonus for intercepting positions between territories.

```rust
if enemy_dist < my_dist {
    territory += (my_dist - enemy_dist) * 3;
}
if enemy_dist <= 3 {
    territory += (4 - enemy_dist) * 2;
}
```

**Why:** Controls the "front line" of battle.

---

#### 4. Enemy Adjacency Score (Weight: 30) 👊
**Purpose:** Counts adjacencies to enemy cells (including diagonals).

```rust
// 8-directional check: ↑ ↓ ← → ↖ ↗ ↙ ↘
for each piece cell:
    for each of 8 directions:
        if neighbor is enemy: enemy_adj += 1
```

**Why:** Maximizes pressure on enemy territory.

---

#### 5. Compactness Score (Weight: 10) 🔗
**Purpose:** Counts adjacencies to own territory.

```rust
// 4-directional check: ↑ ↓ ← →
for each piece cell:
    for each of 4 directions:
        if neighbor is mine: compact += 1
```

**Why:** Prevents fragmented, weak shapes.

---

#### 6. Heat Score (Weight: -5) 🔥
**Purpose:** Sum of distances to enemy for all piece cells.

```rust
heat = sum of heat_map[r][c] for all piece cells
```

**Why:** Negative weight means we prefer **LOWER** heat (closer to enemy).

---

## Data Structures

```rust
struct Game {
    // Board state
    anfield: Vec<Vec<char>>,          // Game board grid
    rows: usize,                       // Board height
    cols: usize,                       // Board width

    // Distance maps (computed each turn)
    heat_map: Vec<Vec<i32>>,          // Distance to nearest enemy
    my_heat_map: Vec<Vec<i32>>,       // Distance to nearest own cell
    frontier_map: Vec<Vec<bool>>,     // Cells adjacent to enemy

    // Piece data
    piece_blocks: Vec<(i32, i32)>,    // Relative positions of piece cells
    piece_rows: usize,                 // Piece height
    piece_cols: usize,                 // Piece width

    // Position tracking
    my_cells: Vec<(usize, usize)>,    // All my cell positions
    enemy_cells: Vec<(usize, usize)>, // All enemy cell positions

    // Game state
    player_num: u8,                    // 1 or 2
    my_chars: (char, char),           // ('@', 'a') or ('$', 's')
    enemy_chars: (char, char),        // Opposite of my_chars
    turn_count: usize,                 // Current turn number
}
```

---

## I/O Format

### Input (from game engine via stdin)

**1. Player Assignment (once at start):**
```
$$$ exec p1 : [./my_bot]
```

**2. Anfield (each turn):**
```
Anfield 40 30:
    0123456789012345678901234567890123456789
000 ........................................
001 .........@..............................
002 ........................................
...
029 ....................$...................
```

**3. Piece (each turn):**
```
Piece 4 2:
.OO.
..O.
```

### Output (to stdout)

```
X Y
```

Where:
- `X` = column (0-indexed from left)
- `Y` = row (0-indexed from top)
- Position is **top-left corner** of piece placement

### Example
```
Input piece:     Output: "7 2"
.OO.             Places piece at column 7, row 2
..O.
```

---

## Building & Running

### Prerequisites
- Docker installed

### Build Docker Image
```bash
cd docker_image
docker build -t filler .
```

### Run Container
```bash
docker run -it filler
```

### Run Single Game
```bash
# Inside container
./game_engine -f maps/map01 -p1 ./my_bot -p2 robots/bender
```

### Run All Tests
```bash
./test_all.sh                    # Standard bots only
./test_all.sh --with-terminator  # Include terminator
```

### Game Engine Flags
| Flag | Description |
|------|-------------|
| `-f` | Path to map file |
| `-p1` | Path to player 1 bot |
| `-p2` | Path to player 2 bot |
| `-q` | Quiet mode (no visualization) |
| `-s` | Random seed number |
| `-t` | Timeout in seconds (default: 10) |

---

## File Structure

```
docker_image/
├── Dockerfile              # Build configuration
├── README.md               # This documentation
├── test_all.sh             # Automated test script
├── solution/
│   ├── Cargo.toml          # Rust project config
│   └── src/
│       └── main.rs         # Bot implementation (~350 lines)
├── maps/
│   ├── map00               # Small: 20×15 (300 cells)
│   ├── map01               # Medium: 40×30 (1,200 cells)
│   └── map02               # Large: 100×100 (10,000 cells)
├── robots/
│   ├── bender              # Easy opponent
│   ├── h2_d2               # Medium opponent
│   ├── wall_e              # Hard opponent
│   └── terminator          # "Unbeatable" opponent
└── game_engine             # Game executable
```

---

## Performance Results

### vs Standard Bots ✅ 100% Win Rate

| Map | vs Bender | vs H2_D2 | vs Wall_E |
|-----|-----------|----------|-----------|
| map00 (small) | 227-53 | 232-54 | 211-81 |
| map01 (medium) | 957-162 | 922-230 | 828-293 |
| map02 (large) | 9241-454 | 9380-330 | 9197-464 |

### vs Terminator 🏆 (Supposed to be "unbeatable")

| Map | Result | Our Score | Enemy Score |
|-----|--------|-----------|-------------|
| map00 | **WIN** | 165 | 110 |
| map01 | **WIN** | 816 | 280 |
| map02 | **WIN** | 5265 | 3871 |

*Note: Results vary slightly due to random piece generation*

---

## Why This Strategy Works

### 1. Aggressive Expansion 🚀
- Prioritizes **contested territory** over safe expansion
- Claims disputed cells before enemy can reach them

### 2. Direct Blocking 🛡️
- **Frontier scoring** ensures we cut off enemy paths
- Places pieces directly adjacent to enemy territory

### 3. Constant Pressure 💪
- **Enemy adjacency** keeps constant pressure
- 8-directional check maximizes contact

### 4. Efficient Computation ⚡
- BFS distance maps are **O(n)** per turn
- No expensive pathfinding or simulation

### 5. Simple & Robust 🎯
- Single scoring formula, no complex state machines
- Works on any map size with any piece shape

---

## Complexity Analysis

| Operation | Time Complexity |
|-----------|-----------------|
| Parse input | O(rows × cols) |
| Build heat maps (BFS) | O(rows × cols) |
| Build frontier map | O(enemy_cells × 4) |
| Find valid placements | O(rows × cols × piece_size) |
| Score single placement | O(piece_size × 8) |
| **Total per turn** | **O(rows × cols × piece_size)** |

For map02 (100×100) with average piece size 5:
- ~50,000 operations per turn
- Executes in **< 10ms**

---

## Key Insights

1. **Heat maps are the foundation** - Know distance to enemy at every cell
2. **Contested cells are gold** - Win the race for disputed territory
3. **Blocking beats expanding** - Cut off enemy before growing safely
4. **Simplicity wins** - One good formula beats complex logic
5. **Test extensively** - Random pieces mean variance in results

---

## Credits

- **Language:** Rust 1.75
- **Algorithm:** BFS + Multi-factor weighted scoring
- **Achievement:** Defeats the "unbeatable" terminator

---

## Quick Start

```bash
# Build
cd docker_image && docker build -t filler .

# Run
docker run -it filler

# Test
./test_all.sh --with-terminator
```

**Expected output:** 12/12 wins (or 11/12 with unlucky pieces)
