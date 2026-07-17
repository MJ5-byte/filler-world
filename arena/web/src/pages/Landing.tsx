import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, Bot, Match } from '../api'
import { generateHeroReplay, Replay } from '../lib/heroReplay'
import { useAuth } from '../hooks/useAuth'
import LoginForm from '../components/LoginForm'

const AZURE = 'var(--p1)'
const AMBER = 'var(--p2)'
const SUPPORTED_LANGS = 4 // python, go, c, rust

const FEATURES = [
  { step: '01', title: 'Write a bot', desc: 'Python, Go, C, or Rust — implement the Filler placement interface.' },
  { step: '02', title: 'Upload & build', desc: 'Sandboxed compile/build with live status and error logs on failure.' },
  { step: '03', title: 'Auto-matched', desc: 'Challenge bots directly or queue into tournaments and the ranked ladder.' },
  { step: '04', title: 'Watch & climb', desc: 'Every match replays turn-by-turn. Win to climb the Elo leaderboard.' },
]

function tickerLine(m: Match): string {
  if (m.status === 'running') return `● LIVE: ${m.botAName} vs ${m.botBName} ON ${m.mapName.toUpperCase()}`
  const winnerName = m.winnerId === m.botAId ? m.botAName : m.winnerId === m.botBId ? m.botBName : null
  if (winnerName) {
    const margin = Math.abs((m.scoreA ?? 0) - (m.scoreB ?? 0))
    return `▶ ${winnerName.toUpperCase()} DEF. ${(winnerName === m.botAName ? m.botBName : m.botAName).toUpperCase()} · +${margin} CELLS`
  }
  return `▶ ${m.botAName.toUpperCase()} DREW ${m.botBName.toUpperCase()}`
}

export default function Landing() {
  const { user, authReady, refreshUser } = useAuth()
  const [params, setParams] = useSearchParams()
  const nav = useNavigate()
  const next = params.get('next')

  const [matches, setMatches] = useState<Match[]>([])
  const [bots, setBots] = useState<Bot[]>([])
  const [replay, setReplay] = useState<Replay | null>(null)
  const [turnIdx, setTurnIdx] = useState(0)
  const [showLogin, setShowLogin] = useState(false)
  const seedRef = useRef(1)
  const timerRef = useRef<ReturnType<typeof setInterval>>()

  useEffect(() => {
    let alive = true
    const load = () => api.matches().then(ms => alive && setMatches(ms)).catch(() => {})
    load()
    const t = setInterval(load, 8000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  useEffect(() => {
    let alive = true
    api.leaderboard().then(b => alive && setBots(b)).catch(() => {})
    return () => { alive = false }
  }, [])

  // A gated route bounced the visitor here with ?next=. If they already
  // have a session, forward them straight through; otherwise pop the login.
  useEffect(() => {
    if (!authReady || !next) return
    if (user) nav(next, { replace: true })
    else setShowLogin(true)
  }, [authReady, next, user, nav])

  // decorative hero animation — synthetic, no backend dependency
  useEffect(() => {
    const spawn = () => {
      const r = generateHeroReplay(22, 13, seedRef.current++, 90)
      setReplay(r)
      setTurnIdx(0)
      clearInterval(timerRef.current)
      timerRef.current = setInterval(() => {
        setTurnIdx(i => {
          if (i >= r.frames.length - 1) {
            clearInterval(timerRef.current)
            setTimeout(spawn, 900)
            return i
          }
          return i + 1
        })
      }, 260)
    }
    spawn()
    return () => clearInterval(timerRef.current)
  }, [])

  const closeLogin = () => {
    setShowLogin(false)
    if (next) setParams(p => { p.delete('next'); return p })
  }
  const loginSucceeded = () => nav(next || '/leaderboard')

  const liveCount = matches.filter(m => m.status === 'running').length
  const tickerItems = matches.filter(m => m.status === 'finished' || m.status === 'running').slice(0, 10)

  const heroCells: { x: number; y: number; fill: string; glow: boolean }[] = []
  let heroScoreA = 1, heroScoreB = 1
  if (replay) {
    const idx = Math.min(turnIdx, replay.frames.length - 1)
    const frame = replay.frames[idx]
    heroScoreA = frame.scoreP1
    heroScoreB = frame.scoreP2
    const grid = new Uint8Array(replay.width * replay.height)
    for (let i = 0; i <= idx; i++) {
      for (const c of replay.frames[i].cells) grid[c.y * replay.width + c.x] = replay.frames[i].mover
    }
    const justSet = new Set(frame.cells.map(c => c.y * replay.width + c.x))
    for (let y = 0; y < replay.height; y++) {
      for (let x = 0; x < replay.width; x++) {
        const owner = grid[y * replay.width + x]
        if (owner === 0) continue
        heroCells.push({ x, y, fill: owner === 1 ? AZURE : AMBER, glow: justSet.has(y * replay.width + x) })
      }
    }
  }

  return (
    <div className="landing">
      <div className="scanline-overlay" />

      <div className="landing-nav">
        <Link to="/leaderboard" className="landing-logo">FILLER<span className="logo-underscore">_</span>ARENA</Link>
        <div className="landing-nav-links">
          <Link to="/leaderboard">LEADERBOARD</Link>
          <Link to="/matches">REPLAYS</Link>
          <Link to="/tournaments">TOURNAMENTS</Link>
          {user ? (
            <Link to="/leaderboard" className="landing-nav-signup">ENTER ARENA</Link>
          ) : (
            <>
              <span className="landing-nav-login" onClick={() => setShowLogin(true)}>LOGIN</span>
              <span className="landing-nav-signup" onClick={() => setShowLogin(true)}>SIGN UP</span>
            </>
          )}
        </div>
      </div>

      <div className="landing-hero">
        <div className="landing-hero-copy">
          <div className="landing-live-badge">
            <span className="live-dot" />
            <span>{liveCount} MATCH{liveCount === 1 ? '' : 'ES'} RUNNING NOW</span>
          </div>
          <h1 className="landing-headline">
            <span>UPLOAD YOUR BOT.</span>
            <span>WATCH IT <span className="landing-accent">CONQUER</span></span>
            <span>THE GRID.</span>
          </h1>
          <p className="landing-sub">
            Write a bot in Python, Go, C, or Rust. It fights automatically in sandboxed matches
            of Filler — territory control, one tetromino at a time. Every match replays
            turn-by-turn. Elo-ranked, all the way up.
          </p>
          <div className="landing-cta-row">
            <Link to="/upload"><button>DEPLOY YOUR FIRST BOT</button></Link>
            <Link to="/matches"><button className="ghost">WATCH A REPLAY</button></Link>
          </div>
          <div className="landing-stats">
            <div className="landing-stat">
              <div className="landing-stat-value">{bots.length}</div>
              <div className="landing-stat-label">BOTS RANKED</div>
            </div>
            <div className="landing-stat">
              <div className="landing-stat-value">{matches.length}{matches.length === 100 ? '+' : ''}</div>
              <div className="landing-stat-label">RECENT MATCHES</div>
            </div>
            <div className="landing-stat">
              <div className="landing-stat-value">{SUPPORTED_LANGS}</div>
              <div className="landing-stat-label">LANGUAGES</div>
            </div>
            <div className="landing-stat">
              <div className="landing-stat-value">{liveCount}</div>
              <div className="landing-stat-label">LIVE NOW</div>
            </div>
          </div>
        </div>

        <div className="landing-hero-widget">
          <div className="hero-widget-box">
            <div className="hero-widget-head">
              <span className="muted small-num">DEMO_REPLAY</span>
              <div className="hero-widget-score">
                <span style={{ color: AZURE, fontWeight: 700 }}>{heroScoreA}</span>
                <span className="muted"> — </span>
                <span style={{ color: AMBER, fontWeight: 700 }}>{heroScoreB}</span>
              </div>
            </div>
            <svg viewBox={replay ? `0 0 ${replay.width} ${replay.height}` : '0 0 22 13'} className="hero-widget-grid">
              {heroCells.map((c, i) => (
                <rect key={i} x={c.x} y={c.y} width={1} height={1} fill={c.fill}
                  style={c.glow ? { filter: `drop-shadow(0 0 0.3px ${c.fill})`, animation: 'fa-pulse 0.7s ease-out' } : undefined} />
              ))}
            </svg>
          </div>
        </div>
      </div>

      {tickerItems.length > 0 && (
        <div className="landing-marquee">
          <div className="landing-marquee-track">
            {tickerItems.concat(tickerItems).map((m, i) => (
              <span key={i}>{tickerLine(m)}</span>
            ))}
          </div>
        </div>
      )}

      <div className="landing-features">
        <div className="panel-title" style={{ margin: '0 0 16px' }}>How it works</div>
        <div className="features-grid">
          {FEATURES.map(f => (
            <div className="feature-card" key={f.step}>
              <div className="feature-step muted small-num">{f.step}</div>
              <div className="feature-title">{f.title}</div>
              <div className="feature-desc muted">{f.desc}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="landing-bottom-cta">
        <div className="landing-bottom-title">READY TO GET WRECKED?</div>
        <div className="muted">Free for students. Sandboxed, safe, and merciless.</div>
        <button style={{ marginTop: 24 }} onClick={() => setShowLogin(true)}>CREATE ACCOUNT</button>
      </div>

      <div className="landing-footer">
        <span>FILLER_ARENA © {new Date().getFullYear()}</span>
        <span>BOT WARFARE SPECTATOR CLIENT</span>
      </div>

      {showLogin && (
        <div className="login-modal-backdrop" onClick={closeLogin}>
          <div onClick={e => e.stopPropagation()}>
            <LoginForm refreshUser={refreshUser} onSuccess={loginSucceeded} />
          </div>
        </div>
      )}
    </div>
  )
}
