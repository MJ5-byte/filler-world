import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, Replay, Turn } from '../api'

const AZURE = 'oklch(0.72 0.14 231)'
const AMBER = 'oklch(0.78 0.15 68)'

const COLORS: Record<string, string> = {
  '@': AZURE, // player 1 territory
  'a': AZURE, // player 1, just placed (same hue, board glow shows recency)
  '$': AMBER, // player 2 territory
  's': AMBER, // player 2, just placed
}
const EMPTY = '#000000'
const GRID_LINE = '#0a0a0a'

// Board cells the piece landed on, in board coordinates — used to draw a
// glowing outline around the most-recently-placed piece.
function pieceCells(turn: Turn): [number, number][] {
  const rows = turn.piece.split('\n')
  const cells: [number, number][] = []
  for (let ry = 0; ry < rows.length; ry++) {
    for (let rx = 0; rx < rows[ry].length; rx++) {
      if (rows[ry][rx] !== '.' && rows[ry][rx] !== ' ') cells.push([turn.x + rx, turn.y + ry])
    }
  }
  return cells
}

function drawBoard(canvas: HTMLCanvasElement, anfield: string, highlight: [number, number][], glowColor: string) {
  const rows = anfield.split('\n')
  const h = rows.length
  const w = rows[0]?.length ?? 0
  if (!w || !h) return
  const cell = Math.max(2, Math.min(26, Math.floor(680 / Math.max(w, h))))
  canvas.width = w * cell
  canvas.height = h * cell
  const ctx = canvas.getContext('2d')!
  ctx.fillStyle = GRID_LINE
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  const pad = cell > 6 ? 1 : 0
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < rows[y].length; x++) {
      const ch = rows[y][x]
      ctx.fillStyle = COLORS[ch] ?? EMPTY
      ctx.fillRect(x * cell + pad, y * cell + pad, cell - pad, cell - pad)
    }
  }
  // glow the just-placed piece
  if (highlight.length > 0) {
    ctx.save()
    ctx.shadowColor = glowColor
    ctx.shadowBlur = Math.max(4, cell)
    ctx.strokeStyle = '#ffffff'
    ctx.lineWidth = Math.max(1, cell * 0.12)
    for (const [x, y] of highlight) {
      ctx.strokeRect(x * cell + pad, y * cell + pad, cell - pad, cell - pad)
    }
    ctx.restore()
  }
}

export default function ReplayViewer() {
  const { id } = useParams()
  const [replay, setReplay] = useState<Replay | null>(null)
  const [error, setError] = useState('')
  const [idx, setIdx] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(8) // turns per second
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    if (!id) return
    let alive = true
    const load = () =>
      api.replay(id).then(r => {
        if (!alive) return
        setReplay(r)
        // keep polling while the match is still being played
        if (r.match.status === 'queued' || r.match.status === 'running') {
          setTimeout(load, 3000)
        }
      }).catch(e => alive && setError(String(e)))
    load()
    return () => { alive = false }
  }, [id])

  const turns = replay?.turns ?? []
  const turn = turns[Math.min(idx, turns.length - 1)]

  // cumulative territory count per turn, computed once per load — powers
  // the territory bar and the "+N cells" move log.
  const cellCounts = useMemo(() => {
    return turns.map(t => {
      let a = 0, b = 0
      for (const ch of t.anfield) {
        if (ch === '@' || ch === 'a') a++
        else if (ch === '$' || ch === 's') b++
      }
      return { a, b }
    })
  }, [turns])

  const curIdx = Math.min(idx, turns.length - 1)
  const counts = cellCounts[curIdx]
  const cellsA = counts?.a ?? 0, cellsB = counts?.b ?? 0
  const share = cellsA + cellsB > 0 ? cellsA / (cellsA + cellsB) : 0.5

  // keyboard: ←/→ step, space play/pause, Home/End jump
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return
      if (e.key === 'ArrowLeft') { setPlaying(false); setIdx(i => Math.max(0, i - 1)) }
      else if (e.key === 'ArrowRight') { setPlaying(false); setIdx(i => Math.min(turns.length - 1, i + 1)) }
      else if (e.key === ' ') { e.preventDefault(); setPlaying(p => !p) }
      else if (e.key === 'Home') { setPlaying(false); setIdx(0) }
      else if (e.key === 'End') { setPlaying(false); setIdx(turns.length - 1) }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [turns.length])

  useEffect(() => {
    if (canvasRef.current && turn) {
      const glow = turn.player === 1 ? AZURE : AMBER
      drawBoard(canvasRef.current, turn.anfield, pieceCells(turn), glow)
    }
  }, [turn])

  useEffect(() => {
    if (!playing || turns.length === 0) return
    const t = setInterval(() => {
      setIdx(i => {
        if (i >= turns.length - 1) { setPlaying(false); return i }
        return i + 1
      })
    }, 1000 / speed)
    return () => clearInterval(t)
  }, [playing, speed, turns.length])

  if (error) return <div className="error-box">{error}</div>
  if (!replay) return <p className="muted">Loading…</p>
  const m = replay.match

  const nextTurn = turns[curIdx + 1]
  const nextMoverName = nextTurn ? (nextTurn.player === 1 ? m.botAName : m.botBName) : null

  const logStart = Math.max(0, curIdx - 7)
  const moveLog = []
  for (let i = curIdx; i >= logStart; i--) {
    const t = turns[i]
    const mine = t.player === 1 ? cellCounts[i]?.a : cellCounts[i]?.b
    const prevIdx = i - 2
    const prev = prevIdx >= 0
      ? (t.player === 1 ? cellCounts[prevIdx]?.a : cellCounts[prevIdx]?.b)
      : 1
    const gained = mine != null && prev != null ? Math.max(0, mine - prev) : null
    moveLog.push({ turn: t.n, player: t.player, gained })
  }

  return (
    <>
      <div className="replay-toolbar">
        <h1 style={{ margin: 0 }}>
          Match #{m.id}: <Link to={`/bots/${m.botAId}`}>{m.botAName}</Link>
          <span className="muted"> vs </span>
          <Link to={`/bots/${m.botBId}`}>{m.botBName}</Link>
          <span className="muted"> on {m.mapName} </span>
          <span className={`badge ${m.status}`}>{m.status}</span>
        </h1>
      </div>

      {m.status === 'error' && <div className="error-box">{m.error}</div>}
      {(m.status === 'queued' || m.status === 'running') && (
        <p className="muted">Match in progress — turns stream in when it finishes.</p>
      )}

      {turns.length > 0 && turn && (
        <>
          <div className="match-header">
            <div className="match-header-side p1">
              <div>
                <div className="match-header-name">{m.botAName}</div>
                <div className="match-header-meta">PLAYER 1</div>
              </div>
              <div className="match-header-score p1">{m.scoreA ?? cellsA}</div>
            </div>
            <div className="match-header-turn">
              <div className="match-header-turn-label">TURN</div>
              <div className="match-header-turn-val">{turn.n} / {turns.length}</div>
            </div>
            <div className="match-header-side p2">
              <div className="match-header-score p2">{m.scoreB ?? cellsB}</div>
              <div>
                <div className="match-header-name">{m.botBName}</div>
                <div className="match-header-meta">PLAYER 2</div>
              </div>
            </div>
          </div>

          <div className="replay-layout">
            <div className="replay-board">
              <canvas ref={canvasRef} />
            </div>

            <div className="replay-side">
              <div className="panel">
                <div className="panel-title">
                  Next piece{nextMoverName ? ` — ${nextMoverName}` : ''}
                </div>
                <div className="piece-preview">{nextTurn ? nextTurn.piece : turn.piece}</div>
              </div>

              <div className="panel">
                <div className="panel-title">Move log</div>
                <div className="move-log-list">
                  {moveLog.map((l, i) => (
                    <div className="move-log-row" key={i}>
                      <span className="move-log-turn">T{l.turn}</span>{' '}
                      <span style={{ color: l.player === 1 ? 'var(--p1)' : 'var(--p2)', fontWeight: 600 }}>
                        {l.player === 1 ? 'P1' : 'P2'}
                      </span>{' '}
                      {l.gained != null ? `+${l.gained} cells` : `plays ${turns[curIdx]?.x},${turns[curIdx]?.y}`}
                    </div>
                  ))}
                </div>
              </div>

              <div className="panel">
                <div className="panel-title">Territory</div>
                <div className="territory-bar" title="territory share this turn">
                  <div className="territory-a" style={{ width: `${share * 100}%` }} />
                </div>
                {m.status === 'finished' && (
                  <p className="muted small-num" style={{ marginBottom: 0 }}>
                    {m.winnerId == null ? 'Draw.'
                      : `${m.winnerId === m.botAId ? m.botAName : m.botBName} wins.`}
                  </p>
                )}
                <div className="legend legend-grouped" style={{ marginTop: 10 }}>
                  <span>
                    <span className="swatch" style={{ background: AZURE }} />{m.botAName}
                  </span>
                  <span>
                    <span className="swatch" style={{ background: AMBER }} />{m.botBName}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <div className="replay-controls">
            <div className="transport-btns">
              <button className="ghost small" onClick={() => { setPlaying(false); setIdx(0) }}>⏮</button>
              <button className="ghost small" onClick={() => { setPlaying(false); setIdx(i => Math.max(0, i - 1)) }}>◀</button>
            </div>
            <button className="play-btn" onClick={() => setPlaying(p => !p)}>{playing ? '❙❙ PAUSE' : '▶ PLAY'}</button>
            <div className="transport-btns">
              <button className="ghost small" onClick={() => { setPlaying(false); setIdx(i => Math.min(turns.length - 1, i + 1)) }}>▶</button>
              <button className="ghost small" onClick={() => { setPlaying(false); setIdx(turns.length - 1) }}>⏭</button>
            </div>
            <input
              type="range" min={0} max={turns.length - 1} value={idx}
              onChange={e => { setPlaying(false); setIdx(Number(e.target.value)) }}
            />
            <div className="speed-row">
              {[2, 8, 20, 60].map(v => (
                <span key={v} className={`speed-chip ${speed === v ? 'on' : ''}`} onClick={() => setSpeed(v)}>{v}×</span>
              ))}
            </div>
          </div>
          <p className="keys-hint muted">
            <span className="keys-label">keys</span> ←/→ step · space play/pause · Home/End jump
          </p>
        </>
      )}

      {turns.length === 0 && m.status === 'finished' && (
        <p className="muted">No replay data was captured for this match.</p>
      )}
    </>
  )
}
