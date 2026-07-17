import { useEffect, useState } from 'react'
import { Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, AuthUser } from './api'
import { useAuth } from './hooks/useAuth'

export interface AppContext {
  user: AuthUser | null
  authReady: boolean
  refreshUser: () => void
}

const NAV_ITEMS: { to: string; label: string; end?: boolean }[] = [
  { to: '/leaderboard', label: 'LEADERBOARD' },
  { to: '/challenge', label: 'CHALLENGE' },
  { to: '/matches', label: 'MATCHES' },
  { to: '/tournaments', label: 'TOURNAMENTS' },
  { to: '/players', label: 'PLAYERS' },
  { to: '/upload', label: 'BOT UPLOAD' },
]

// The arena is members-only: every page behind this shell requires a
// session. Anonymous visitors are bounced to the landing page's login
// modal, with ?next= so they land back where they were headed.
export default function App() {
  const { user, authReady, refreshUser } = useAuth()
  const [liveCount, setLiveCount] = useState(0)
  const nav = useNavigate()
  const location = useLocation()

  useEffect(() => {
    let alive = true
    const load = () =>
      api.matches()
        .then(ms => alive && setLiveCount(ms.filter(m => m.status === 'running').length))
        .catch(() => {})
    load()
    const t = setInterval(load, 5000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  if (!authReady) return null
  if (!user) {
    const next = encodeURIComponent(location.pathname + location.search)
    return <Navigate to={`/?next=${next}`} replace />
  }

  const logout = async () => {
    await api.logout().catch(() => {})
    nav('/')
  }

  return (
    <div className="app">
      <div className="sidebar">
        <div className="sidebar-head">
          <NavLink to="/leaderboard" className="logo">
            FILLER<span className="logo-underscore">_</span>ARENA
          </NavLink>
          <div className="sidebar-tagline">BOT WARFARE SPECTATOR</div>
        </div>
        <nav className="sidebar-nav">
          {NAV_ITEMS.map(item => (
            <NavLink key={item.to} to={item.to} end={item.end}>
              {item.label}
            </NavLink>
          ))}
          {user.isAdmin && <NavLink to="/admin">ADMIN PANEL</NavLink>}
        </nav>
        <div className="sidebar-foot">
          <span className="live-dot" />
          <span className="sidebar-foot-label">{liveCount} MATCH{liveCount === 1 ? '' : 'ES'} LIVE</span>
        </div>
        <div className="sidebar-user">
          <NavLink to={`/players/${user.login}`} className="user-chip">
            <span className="user-dot" />{user.login}
          </NavLink>
          <button className="ghost small" onClick={logout}>Log out</button>
        </div>
      </div>
      <main>
        <Outlet context={{ user, authReady, refreshUser } satisfies AppContext} />
        <footer className="footer">
          <span className="footer-glyph">▚</span> FILLER ARENA — bots fight, replays don't lie.
        </footer>
      </main>
    </div>
  )
}
