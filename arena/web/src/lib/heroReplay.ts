// Deterministic territory-growth simulator for the landing page's hero
// animation. Not the real game engine — approximates Filler's placement
// rule (piece overlaps exactly one own cell, rest land on empty cells) to
// produce a convincing decorative replay with no backend dependency.

function mulberry32(seed: number) {
  let a = seed >>> 0
  return function () {
    a |= 0; a = (a + 0x6D2B79F5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const BASE_SHAPES = [
  [[0, 0], [1, 0], [2, 0], [3, 0]],
  [[0, 0], [1, 0], [0, 1], [1, 1]],
  [[0, 0], [1, 0], [2, 0], [1, 1]],
  [[1, 0], [2, 0], [0, 1], [1, 1]],
  [[0, 0], [1, 0], [1, 1], [2, 1]],
  [[0, 0], [0, 1], [0, 2], [1, 2]],
  [[1, 0], [1, 1], [1, 2], [0, 2]],
]

type Cell = [number, number]

function normalize(cells: Cell[]): Cell[] {
  const minx = Math.min(...cells.map(c => c[0]))
  const miny = Math.min(...cells.map(c => c[1]))
  return cells.map(([x, y]) => [x - minx, y - miny])
}
function rotate(cells: Cell[]): Cell[] {
  return normalize(cells.map(([x, y]) => [y, -x]))
}
function key(cells: Cell[]): string {
  return cells.map(c => c.join(',')).sort().join('|')
}

const VARIANTS: Cell[][] = []
for (const shape of BASE_SHAPES) {
  const seen = new Set<string>()
  let cur = normalize(shape as Cell[])
  for (let i = 0; i < 4; i++) {
    const k = key(cur)
    if (!seen.has(k)) { seen.add(k); VARIANTS.push(cur) }
    cur = rotate(cur)
  }
}

export interface ReplayFrame {
  mover: 1 | 2
  cells: { x: number; y: number }[]
  scoreP1: number
  scoreP2: number
}
export interface Replay {
  width: number
  height: number
  frames: ReplayFrame[]
}

export function generateHeroReplay(width: number, height: number, seed: number, maxTurns = 90): Replay {
  const rand = mulberry32(seed)
  const owner = new Uint8Array(width * height) // 0 empty, 1 p1, 2 p2
  const idx = (x: number, y: number) => y * width + x
  const inBounds = (x: number, y: number) => x >= 0 && x < width && y >= 0 && y < height
  const NEI = [[1, 0], [-1, 0], [0, 1], [0, -1]]

  const frontier: Record<1 | 2, number[]> = { 1: [], 2: [] }
  const inFrontier: Record<1 | 2, Set<number>> = { 1: new Set(), 2: new Set() }
  function hasEmptyNeighbor(x: number, y: number) {
    for (const [dx, dy] of NEI) {
      const nx = x + dx, ny = y + dy
      if (inBounds(nx, ny) && owner[idx(nx, ny)] === 0) return true
    }
    return false
  }
  function pushFrontier(p: 1 | 2, x: number, y: number) {
    const i = idx(x, y)
    if (!inFrontier[p].has(i) && hasEmptyNeighbor(x, y)) {
      inFrontier[p].add(i)
      frontier[p].push(i)
    }
  }
  function sampleFrontier(p: 1 | 2, tries: number) {
    const arr = frontier[p]
    const out: number[] = []
    let attempts = 0
    while (arr.length && out.length < tries && attempts < tries * 3) {
      attempts++
      const pos = Math.floor(rand() * arr.length)
      const i = arr[pos]
      const x = i % width, y = (i / width) | 0
      if (owner[i] !== p || !hasEmptyNeighbor(x, y)) {
        arr.splice(pos, 1)
        inFrontier[p].delete(i)
        continue
      }
      out.push(i)
    }
    return out
  }

  const startP1: Cell = [1, 1]
  const startP2: Cell = [width - 2, height - 2]
  owner[idx(...startP1)] = 1
  owner[idx(...startP2)] = 2
  pushFrontier(1, ...startP1)
  pushFrontier(2, ...startP2)

  const frames: ReplayFrame[] = [
    { mover: 1, cells: [{ x: startP1[0], y: startP1[1] }], scoreP1: 1, scoreP2: 0 },
    { mover: 2, cells: [{ x: startP2[0], y: startP2[1] }], scoreP1: 1, scoreP2: 1 },
  ]

  let scoreP1 = 1, scoreP2 = 1
  let stuckStreak = 0
  let mover: 1 | 2 = 1

  const shuffledVariants = () => {
    const v = VARIANTS.slice()
    for (let i = v.length - 1; i > 0; i--) {
      const j = Math.floor(rand() * (i + 1))
      ;[v[i], v[j]] = [v[j], v[i]]
    }
    return v
  }

  for (let t = 2; t < maxTurns + 2; t++) {
    const p: 1 | 2 = mover
    const other: 1 | 2 = p === 1 ? 2 : 1
    const candidates = sampleFrontier(p, 24)
    let placed: Cell[] | null = null

    for (const ci of candidates) {
      const cx = ci % width, cy = (ci / width) | 0
      for (const variant of shuffledVariants()) {
        for (let oi = 0; oi < variant.length && !placed; oi++) {
          const [ox, oy] = variant[oi]
          const dx = cx - ox, dy = cy - oy
          const cells = variant.map(([vx, vy]) => [vx + dx, vy + dy] as Cell)
          let valid = true
          for (let k2 = 0; k2 < cells.length; k2++) {
            const [px, py] = cells[k2]
            if (!inBounds(px, py)) { valid = false; break }
            const cell = owner[idx(px, py)]
            if (k2 === oi) { if (cell !== p) { valid = false; break } }
            else if (cell !== 0) { valid = false; break }
          }
          if (valid) { placed = cells; break }
        }
        if (placed) break
      }
      if (placed) break
    }

    if (!placed) {
      stuckStreak++
      if (stuckStreak >= 4) break
      mover = other
      continue
    }
    stuckStreak = 0

    const outCells: { x: number; y: number }[] = []
    for (const [px, py] of placed) {
      if (owner[idx(px, py)] !== p) {
        owner[idx(px, py)] = p
        if (p === 1) scoreP1++; else scoreP2++
      }
      outCells.push({ x: px, y: py })
      pushFrontier(p, px, py)
    }
    frames.push({ mover: p, cells: outCells, scoreP1, scoreP2 })
    mover = other
  }

  return { width, height, frames }
}
