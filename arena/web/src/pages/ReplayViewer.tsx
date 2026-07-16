import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, Replay } from '../api'

const COLORS: Record<string, string> = {
  '@': '#2f86b3', // player 1 territory
  'a': '#a8e6ff', // player 1, just placed
  '$': '#c47a2c', // player 2 territory
  's': '#ffd9a8', // player 2, just placed
}
const EMPTY = '#20242e'
const GRID_LINE = '#12141a'

function drawBoard(canvas: HTMLCanvasElement, anfield: string) {
  const rows = anfield.split('\n')
  const h = rows.length
  const w = rows[0]?.length ?? 0
  if (!w || !h) return
  const cell = Math.max(4, Math.min(26, Math.floor(680 / Math.max(w, h))))
  canvas.width = w * cell
  canvas.height = h * cell
  const ctx = canvas.getContext('2d')!
  ctx.fillStyle = GRID_LINE
  ctx.fillRect(0, 0, canvas.width, canvas.height)
  const pad = cell > 6 ? 1 : 0
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < rows[y].length; x++) {
      ctx.fillStyle = COLORS[rows[y][x]] ?? EMPTY
      ctx.fillRect(x * cell + pad, y * cell + pad, cell - pad, cell - pad)
    }
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

  // territory share for the current turn's board
  let cellsA = 0, cellsB = 0
  if (turn) {
    for (const ch of turn.anfield) {
      if (ch === '@' || ch === 'a') cellsA++
      else if (ch === '$' || ch === 's') cellsB++
    }
  }
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
    if (canvasRef.current && turn) drawBoard(canvasRef.current, turn.anfield)
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

  return (
    <>
      <h1>
        Match #{m.id}: <Link to={`/bots/${m.botAId}`}>{m.botAName}</Link>
        <span className="muted"> vs </span>
        <Link to={`/bots/${m.botBId}`}>{m.botBName}</Link>
        <span className="muted"> on {m.mapName} </span>
        <span className={`badge ${m.status}`}>{m.status}</span>
      </h1>

      {m.status === 'error' && <div className="error-box">{m.error}</div>}
      {(m.status === 'queued' || m.status === 'running') && (
        <p className="muted">Match in progress — turns stream in when it finishes.</p>
      )}

      {turns.length > 0 && turn && (
        <div className="replay-layout">
          <div className="replay-board">
            <canvas ref={canvasRef} />
            <div className="replay-controls">
              <button className="ghost" onClick={() => { setPlaying(false); setIdx(0) }}>⏮</button>
              <button className="ghost" onClick={() => { setPlaying(false); setIdx(i => Math.max(0, i - 1)) }}>◀</button>
              <button onClick={() => setPlaying(p => !p)}>{playing ? '⏸ Pause' : '▶ Play'}</button>
              <button className="ghost" onClick={() => { setPlaying(false); setIdx(i => Math.min(turns.length - 1, i + 1)) }}>▶</button>
              <input
                type="range" min={0} max={turns.length - 1} value={idx}
                onChange={e => { setPlaying(false); setIdx(Number(e.target.value)) }}
              />
              <select value={speed} onChange={e => setSpeed(Number(e.target.value))}>
                <option value={2}>2×</option>
                <option value={8}>8×</option>
                <option value={20}>20×</option>
                <option value={60}>60×</option>
              </select>
            </div>
          </div>

          <div className="replay-side">
            <div className="panel">
              <div className="score-row" style={{ fontSize: 18 }}>
                <span className="p1">{m.botAName}</span>
                <span>{m.scoreA ?? '?'} – {m.scoreB ?? '?'}</span>
                <span className="p2">{m.botBName}</span>
              </div>
              <div className="territory-bar" title="territory share this turn">
                <div className="territory-a" style={{ width: `${share * 100}%` }} />
              </div>
              {m.status === 'finished' && (
                <p className="muted" style={{ marginBottom: 0 }}>
                  {m.winnerId == null ? 'Draw.'
                    : `${m.winnerId === m.botAId ? m.botAName : m.botBName} wins.`}
                </p>
              )}
            </div>

            <div className="panel">
              <div>Turn <strong>{turn.n}</strong> / {turns.length}</div>
              <div style={{ margin: '6px 0' }}>
                <span className={turn.player === 1 ? 'p1' : 'p2'}
                  style={{ color: turn.player === 1 ? 'var(--p1)' : 'var(--p2)', fontWeight: 700 }}>
                  {turn.player === 1 ? m.botAName : m.botBName}
                </span>{' '}
                {turn.x === 0 && turn.y === 0 ? 'passes (0 0)' : <>plays at <strong>{turn.x} {turn.y}</strong></>}
              </div>
              <div className="muted" style={{ marginBottom: 6 }}>Piece given:</div>
              <div className="piece-preview">{turn.piece}</div>
            </div>

            <p className="muted keys-hint">
              <span className="keys-label">keys</span> ←/→ step · space play/pause · Home/End jump
            </p>
            <div className="legend legend-grouped panel">
              <span>
                <span className="swatch" style={{ background: COLORS['@'] }} />{m.botAName}
                <span className="swatch swatch-second" style={{ background: COLORS['a'] }} />
                <span className="muted">just placed</span>
              </span>
              <span>
                <span className="swatch" style={{ background: COLORS['$'] }} />{m.botBName}
                <span className="swatch swatch-second" style={{ background: COLORS['s'] }} />
                <span className="muted">just placed</span>
              </span>
            </div>
          </div>
        </div>
      )}

      {turns.length === 0 && m.status === 'finished' && (
        <p className="muted">No replay data was captured for this match.</p>
      )}
    </>
  )
}
