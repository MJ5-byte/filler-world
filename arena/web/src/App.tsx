import { useCallback, useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api, AuthUser } from './api'

export interface AppContext {
  user: AuthUser | null
  authReady: boolean
  refreshUser: () => void
}

export default function App() {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [authReady, setAuthReady] = useState(false)
  const nav = useNavigate()

  const refreshUser = useCallback(() => {
    api.me()
      .then(d => setUser(d.user))
      .catch(() => setUser(null))
      .finally(() => setAuthReady(true))
  }, [])

  useEffect(() => { refreshUser() }, [refreshUser])

  const logout = async () => {
    await api.logout().catch(() => {})
    setUser(null)
    nav('/')
  }

  return (
    <div className="app">
      <header className="topbar">
        <NavLink to="/" className="logo">
          <span className="logo-glyph" aria-hidden><i /><i /><i /><i /></span>
          Filler Arena
        </NavLink>
        <nav>
          <NavLink to="/" end>Leaderboard</NavLink>
          <NavLink to="/challenge">Challenge</NavLink>
          <NavLink to="/matches">Matches</NavLink>
          <NavLink to="/players">Players</NavLink>
          <NavLink to="/upload">Upload a bot</NavLink>
          {user?.isAdmin && <NavLink to="/admin">Admin</NavLink>}
        </nav>
        <div className="topbar-user">
          {user ? (
            <>
              <NavLink to={`/players/${user.login}`} className="user-chip">
                <span className="user-dot" />{user.login}
              </NavLink>
              <button className="ghost small" onClick={logout}>Log out</button>
            </>
          ) : (
            <NavLink to="/login" className="login-link">Log in</NavLink>
          )}
        </div>
      </header>
      <main>
        <Outlet context={{ user, authReady, refreshUser } satisfies AppContext} />
      </main>
      <footer className="footer muted">
        <span className="footer-glyph" aria-hidden>▚</span> Filler Arena — bots fight, replays don't lie.
      </footer>
    </div>
  )
}
